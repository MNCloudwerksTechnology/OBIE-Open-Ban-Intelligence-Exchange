package audit

import (
	"context"
	"os/user"
	"strconv"
)

// Doors through which an operator acts (obie.origin, ADR 0026).
const (
	// OriginConsole: the local web console.
	OriginConsole = "console"
	// OriginAdminAPI: the admin socket, which obiectl and every other
	// local client use.
	OriginAdminAPI = "admin-api"
)

// Origin is who carried out an operator action — setting or removing an
// override, a report, a revocation — and through which door. The zero
// Origin is a change no operator made.
type Origin struct {
	// Via is OriginConsole or OriginAdminAPI.
	Via string
	// UserID is the local user's UID in decimal, UserName its name; both
	// empty if not known.
	UserID, UserName string
}

// LocalUser returns the origin of an action the local user uid carried out
// through via, with the user's name if the host knows it.
func LocalUser(via string, uid uint32) Origin {
	o := Origin{Via: via, UserID: strconv.FormatUint(uint64(uid), 10)}
	if u, err := user.LookupId(o.UserID); err == nil {
		o.UserName = u.Username
	}
	return o
}

// originKey is the context key of an action's origin.
type originKey struct{}

// WithOrigin returns ctx carrying the origin of the action it serves, for
// the audit record the action writes.
func WithOrigin(ctx context.Context, o Origin) context.Context {
	return context.WithValue(ctx, originKey{}, o)
}

// OriginOf returns the origin ctx carries; the zero Origin if none.
func OriginOf(ctx context.Context) Origin {
	o, _ := ctx.Value(originKey{}).(Origin)
	return o
}
