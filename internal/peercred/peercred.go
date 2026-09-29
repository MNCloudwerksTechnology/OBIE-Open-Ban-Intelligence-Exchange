// Package peercred tells which local user is at the other end of a
// connection to one of the node's local interfaces — the admin API's Unix
// socket and the console's loopback TCP port — and whether that user may
// use them: root, the user obied runs as, and members of
// admin.socket_group (ADR 0012, ADR 0019).
package peercred

import (
	"errors"
	"log/slog"
	"math"
	"os"
	"os/user"
	"strconv"
)

// ErrUnsupported is returned by Unix and LoopbackTCP on platforms that
// cannot report the peer of a connection; there only the file mode of the
// admin socket and the console's token protect them.
var ErrUnsupported = errors.New("peer credentials are not supported on this platform")

// UnknownGID is the GID of a Cred whose group is not known: the socket
// table behind LoopbackTCP records only the owner. It is (gid_t)-1, which
// no group has.
const UnknownGID = math.MaxUint32

// Cred holds the credentials of the process at the other end of a local
// connection, as the kernel reports them.
type Cred struct {
	// PID is the peer's process ID; 0 if not known.
	PID int32
	UID uint32
	// GID is the peer's primary group; UnknownGID if not known.
	GID uint32
}

// Policy decides which local users may use the node's local interfaces:
// root, the user obied runs as, and members of Group.
type Policy struct {
	// SelfUID is the user obied runs as.
	SelfUID int
	// Group is admin.socket_group; GID is its ID, or -1 if it does not
	// exist.
	Group string
	GID   int
	// GroupsOf returns the IDs of every group a user belongs to, its
	// primary group included; nil looks them up in the user database.
	GroupsOf func(uid uint32) ([]string, error)
}

// NewPolicy returns the policy for obied's own user and group. If the
// group does not exist, the policy admits only root and obied's own user,
// and err says why.
func NewPolicy(group string) (Policy, error) {
	p := Policy{SelfUID: os.Getuid(), Group: group, GID: -1}
	g, err := user.LookupGroup(group)
	if err == nil {
		p.GID, err = strconv.Atoi(g.Gid)
	}
	if err != nil {
		p.GID = -1
		return p, err
	}
	return p, nil
}

// Allows reports whether a peer with credentials c may use the interface.
// A failure to look up the peer's groups is logged to log and denies.
func (p Policy) Allows(c Cred, log *slog.Logger) bool {
	if c.UID == 0 || int64(c.UID) == int64(p.SelfUID) {
		return true
	}
	if p.GID < 0 {
		return false
	}
	if int64(c.GID) == int64(p.GID) {
		return true
	}
	groupsOf := p.GroupsOf
	if groupsOf == nil {
		groupsOf = userGroups
	}
	groups, err := groupsOf(c.UID)
	if err != nil {
		log.Warn("cannot look up the groups of a local peer", "uid", c.UID, "error", err)
		return false
	}
	gid := strconv.Itoa(p.GID)
	for _, g := range groups {
		if g == gid {
			return true
		}
	}
	return false
}

// userGroups returns the group IDs of uid from the user database, its
// primary group included.
func userGroups(uid uint32) ([]string, error) {
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return nil, err
	}
	return u.GroupIds()
}
