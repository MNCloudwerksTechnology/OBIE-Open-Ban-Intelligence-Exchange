package console

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os/user"
	"strconv"
	"strings"
	"sync"

	"github.com/MNCloudwerksTechnology/obie/internal/peercred"
)

// contentSecurityPolicy allows the console's own scripts, styles, images
// and requests and nothing else: no inline code, no other origin, no
// framing (ADR 0019).
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; " +
	"connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

// securityHeaders sets the security headers of every response, rejections
// and redirects included.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// refuse answers a request the console will not serve with a plain-text
// explanation; plain text needs no stylesheet, which a refused client
// could not load either.
func refuse(w http.ResponseWriter, code int, msg string) {
	http.Error(w, msg, code)
}

// hostGuard refuses requests that are not addressed to this machine's
// loopback interface. A web page that points its own host name at
// 127.0.0.1 (DNS rebinding) sends that name as Host and is refused before
// anything else runs.
func (c *Console) hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			c.log.Debug("refusing a console request for another host", "host", r.Host)
			refuse(w, http.StatusMisdirectedRequest, fmt.Sprintf(
				"refused: this console answers only requests addressed to 127.0.0.1, [::1] or localhost, not to %q. "+
					"Open it at an address like http://127.0.0.1:9465/, through an SSH port forward from another machine.", r.Host))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackHost reports whether the Host header host names the loopback
// interface: a loopback IP literal or localhost, with any port, so that a
// port forward to another local port works.
func loopbackHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		name = host[1 : len(host)-1]
	}
	if strings.EqualFold(name, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(name)
	return err == nil && addr.Zone() == "" && addr.Unmap().IsLoopback()
}

// connKey is the context key of a request's connection.
type connKey struct{}

// connUser looks up the local user of a connection and decides whether it
// may use the console once, on its first request: keep-alive requests
// reuse the answer, and the http.Server's accept loop never waits for the
// lookup.
type connUser struct {
	once sync.Once
	conn net.Conn
	cred peercred.Cred
	err  error
	// refusal explains why the connection is refused; empty if it passes.
	refusal string
}

// withConn attaches the connection to its requests' context; it is the
// console server's ConnContext.
func withConn(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, &connUser{conn: c})
}

// localUser refuses connections from local users the policy does not
// admit, and connections whose user cannot be told, before any page — the
// sign-in page included. Where the platform cannot tell, only the token
// protects the console.
func (c *Console) localUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := r.Context().Value(connKey{}).(*connUser)
		if !ok {
			refuse(w, http.StatusForbidden, unknownUser)
			return
		}
		u.once.Do(func() { c.admit(u) })
		if u.refusal != "" {
			refuse(w, http.StatusForbidden, u.refusal)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// unknownUser explains the refusal of a connection whose user is not known.
const unknownUser = "refused: the console cannot tell which local user opened this connection, " +
	"so it does not serve it. The obied log says why."

// admit looks up the local user of u's connection and decides whether it
// may use the console. Refusals are logged, at most about once a second:
// any local user can open connections.
func (c *Console) admit(u *connUser) {
	u.cred, u.err = c.lookup(u.conn)
	switch {
	case errors.Is(u.err, peercred.ErrUnsupported):
		// Only the token protects the console on this platform.
	case u.err != nil:
		c.warn("refusing a console connection whose local user cannot be told", "error", u.err)
		u.refusal = unknownUser
	case !c.policy.Allows(u.cred, c.log):
		name := userName(u.cred.UID)
		c.warn("refusing a console connection of a local user outside the admin group",
			"uid", u.cred.UID, "user", name, "group", c.policy.Group)
		u.refusal = refusedUser(c.policy, name, u.cred.UID)
	}
}

// warn logs a warning that clients can provoke, dropping it while such
// warnings come faster than about one a second.
func (c *Console) warn(msg string, args ...any) {
	if c.warnLimit.Allow() {
		c.log.Warn(msg, args...)
	}
}

// refusedUser explains why the local user name (uid) may not use the
// console and how to admit it.
func refusedUser(p peercred.Policy, name string, uid uint32) string {
	msg := fmt.Sprintf("refused: this connection comes from the local user %s (uid %d), who is neither root, nor the user obied runs as, "+
		"nor a member of the group %q. Only those may use the console, as for obiectl. ", name, uid, p.Group)
	if p.GID < 0 {
		return msg + fmt.Sprintf("The group %q does not exist, so only root and the user obied runs as are admitted; "+
			"create it, or set admin.socket_group to an existing group, and restart obied.", p.Group)
	}
	return msg + fmt.Sprintf("To admit the user: sudo usermod -aG %s %s", p.Group, name)
}

// userName returns the name of the user uid, or the uid if it has none.
func userName(uid uint32) string {
	id := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(id); err == nil {
		return u.Username
	}
	return id
}

// fetchGuard refuses requests that other web pages in the operator's
// browser send (cross-site request forgery, cross-origin reads), using
// Fetch Metadata and, from browsers without it, Origin.
func (c *Console) fetchGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reason := crossSite(r); reason != "" {
			c.log.Debug("refusing a console request from another site", "reason", reason, "method", r.Method)
			refuse(w, http.StatusForbidden, "refused: "+reason+
				". The console only answers its own pages; open it directly in the address bar.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// crossSite returns why r must be refused as sent by another site or by a
// page on another port of this host, or "" if it may pass. Pages on other
// ports of 127.0.0.1 are the "same site", so only same-origin requests,
// the user's own navigations and top-level GET navigations from elsewhere
// (which cannot read the response) pass; state-changing requests must be
// same-origin.
func crossSite(r *http.Request) string {
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch site := r.Header.Get("Sec-Fetch-Site"); site {
	case "same-origin":
		return ""
	case "none":
		if safe {
			return ""
		}
		return "a state-changing request not sent by a console page"
	case "same-site", "cross-site":
		if safe && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document" {
			return ""
		}
		return "the request came from another web site or another port of this host"
	case "":
		// A browser without Fetch Metadata, or not a browser at all: every
		// browser sends Origin with a POST.
		if safe {
			return ""
		}
		if origin := r.Header.Values("Origin"); len(origin) != 1 || origin[0] != "http://"+r.Host {
			return "a state-changing request must come from a console page (same Origin)"
		}
		return ""
	default:
		return "unknown Sec-Fetch-Site " + strconv.Quote(site)
	}
}
