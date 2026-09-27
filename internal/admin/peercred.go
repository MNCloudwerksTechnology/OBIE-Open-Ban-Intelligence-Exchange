package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/user"
	"strconv"
)

// errPeerCredUnsupported is returned by readPeerCred on platforms without
// SO_PEERCRED; there only the socket's file mode protects the admin API.
var errPeerCredUnsupported = errors.New("peer credentials are not supported on this platform")

// peerCred holds the credentials of the process at the other end of an
// admin socket connection, as the kernel reports them (SO_PEERCRED).
type peerCred struct {
	PID int32
	UID uint32
	GID uint32
}

// accessPolicy decides which local users may use the admin API: root, the
// user obied runs as, and members of the socket group (ADR 0012).
type accessPolicy struct {
	// selfUID is the user obied runs as.
	selfUID int
	// group is admin.socket_group; gid is its ID, or -1 if it does not
	// exist.
	group string
	gid   int
	// groupsOf returns the IDs of every group a user belongs to.
	groupsOf func(uid uint32) ([]string, error)
}

// newAccessPolicy returns the policy for obied's own user and group.
func newAccessPolicy(group string, log *slog.Logger) accessPolicy {
	p := accessPolicy{selfUID: os.Getuid(), group: group, gid: -1, groupsOf: userGroups}
	g, err := user.LookupGroup(group)
	if err == nil {
		p.gid, err = strconv.Atoi(g.Gid)
	}
	if err != nil {
		p.gid = -1
		log.Warn("admin socket group not found; only root and obied's own user may use the admin API",
			"group", group, "error", err)
	}
	return p
}

// userGroups returns the group IDs of uid from the user database.
func userGroups(uid uint32) ([]string, error) {
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return nil, err
	}
	return u.GroupIds()
}

// allows reports whether a peer with credentials c may use the admin API.
func (p accessPolicy) allows(c peerCred, log *slog.Logger) bool {
	if c.UID == 0 || int64(c.UID) == int64(p.selfUID) {
		return true
	}
	if p.gid < 0 {
		return false
	}
	if int64(c.GID) == int64(p.gid) {
		return true
	}
	groups, err := p.groupsOf(c.UID)
	if err != nil {
		log.Warn("cannot look up the groups of an admin API peer", "uid", c.UID, "error", err)
		return false
	}
	gid := strconv.Itoa(p.gid)
	for _, g := range groups {
		if g == gid {
			return true
		}
	}
	return false
}

// credKey is the context key of a connection's peer credentials.
type credKey struct{}

// credResult is what readPeerCred reported for a connection.
type credResult struct {
	cred peerCred
	err  error
}

// withPeerCred attaches the peer credentials of c to ctx; it is the admin
// server's ConnContext.
func withPeerCred(ctx context.Context, c net.Conn) context.Context {
	cred, err := readPeerCred(c)
	return context.WithValue(ctx, credKey{}, credResult{cred: cred, err: err})
}

// authorize answers 403 to requests whose peer the policy does not allow,
// and to requests whose peer credentials are unknown, unless the platform
// cannot report them.
func authorize(p accessPolicy, next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, ok := r.Context().Value(credKey{}).(credResult)
		switch {
		case ok && errors.Is(res.err, errPeerCredUnsupported):
			next.ServeHTTP(w, r)
		case !ok || res.err != nil:
			log.Warn("refusing an admin API request with unknown peer credentials", "error", res.err)
			http.Error(w, "forbidden: cannot verify the peer credentials of this connection", http.StatusForbidden)
		case !p.allows(res.cred, log):
			log.Warn("refusing an admin API request", "uid", res.cred.UID, "gid", res.cred.GID, "pid", res.cred.PID,
				"method", r.Method, "path", r.URL.Path)
			http.Error(w, fmt.Sprintf("forbidden: uid %d is neither root nor a member of group %q; run obiectl as root or as a member of that group",
				res.cred.UID, p.group), http.StatusForbidden)
		default:
			next.ServeHTTP(w, r)
		}
	})
}
