package audit

import (
	"context"
	"os"
	"os/user"
	"strconv"
	"testing"
)

// TestOrigin: the origin of an action travels in its context, and a
// context without one carries the zero Origin.
func TestOrigin(t *testing.T) {
	if o := OriginOf(context.Background()); o != (Origin{}) {
		t.Errorf("OriginOf(no origin) = %+v, want zero", o)
	}
	want := Origin{Via: OriginConsole, UserID: "1000", UserName: "alice"}
	if o := OriginOf(WithOrigin(context.Background(), want)); o != want {
		t.Errorf("OriginOf = %+v, want %+v", o, want)
	}
}

// TestLocalUser names the local user by UID and, if the host knows it, by
// name.
func TestLocalUser(t *testing.T) {
	uid := uint32(os.Getuid()) // #nosec G115 -- a UID fits in 32 bits.
	o := LocalUser(OriginAdminAPI, uid)
	id := strconv.FormatUint(uint64(uid), 10)
	if o.Via != OriginAdminAPI || o.UserID != id {
		t.Errorf("LocalUser = %+v, want via %s, uid %s", o, OriginAdminAPI, id)
	}
	if u, err := user.LookupId(id); err == nil && o.UserName != u.Username {
		t.Errorf("LocalUser name = %q, want %q", o.UserName, u.Username)
	}
	// A UID without an account keeps only its number.
	if o := LocalUser(OriginConsole, 4294967000); o.UserID != "4294967000" || o.UserName != "" {
		t.Errorf("LocalUser(unknown uid) = %+v", o)
	}
}
