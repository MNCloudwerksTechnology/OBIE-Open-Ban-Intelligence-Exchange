package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/MNCloudwerksTechnology/obie/internal/peercred"
)

// newAccessPolicy returns the policy for obied's own user and group: root,
// that user and members of group may use the admin API (ADR 0012).
func newAccessPolicy(group string, log *slog.Logger) peercred.Policy {
	p, err := peercred.NewPolicy(group)
	if err != nil {
		log.Warn("admin socket group not found; only root and obied's own user may use the admin API",
			"group", group, "error", err)
	}
	return p
}

// credKey is the context key of a connection's peer credentials.
type credKey struct{}

// credResult is what peercred.Unix reported for a connection.
type credResult struct {
	cred peercred.Cred
	err  error
}

// withPeerCred attaches the peer credentials of c to ctx; it is the admin
// server's ConnContext.
func withPeerCred(ctx context.Context, c net.Conn) context.Context {
	cred, err := peercred.Unix(c)
	return context.WithValue(ctx, credKey{}, credResult{cred: cred, err: err})
}

// authorize answers 403 to requests whose peer the policy does not allow,
// and to requests whose peer credentials are unknown, unless the platform
// cannot report them.
func authorize(p peercred.Policy, next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, ok := r.Context().Value(credKey{}).(credResult)
		switch {
		case ok && errors.Is(res.err, peercred.ErrUnsupported):
			next.ServeHTTP(w, r)
		case !ok || res.err != nil:
			log.Warn("refusing an admin API request with unknown peer credentials", "error", res.err)
			http.Error(w, "forbidden: cannot verify the peer credentials of this connection", http.StatusForbidden)
		case !p.Allows(res.cred, log):
			log.Warn("refusing an admin API request", "uid", res.cred.UID, "gid", res.cred.GID, "pid", res.cred.PID,
				"method", r.Method, "path", r.URL.Path)
			http.Error(w, fmt.Sprintf("forbidden: uid %d is neither root nor a member of group %q; run obiectl as root or as a member of that group",
				res.cred.UID, p.Group), http.StatusForbidden)
		default:
			next.ServeHTTP(w, r)
		}
	})
}
