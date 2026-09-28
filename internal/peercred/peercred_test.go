package peercred

import (
	"errors"
	"io"
	"log/slog"
	"testing"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestPolicyAllows(t *testing.T) {
	groups := func(uid uint32) ([]string, error) {
		switch uid {
		case 1002:
			return []string{"100", "990"}, nil
		case 1003:
			return nil, errors.New("user database unavailable")
		case 1004:
			return []string{"990"}, nil // primary group only
		}
		return []string{"100"}, nil
	}
	p := Policy{SelfUID: 997, Group: "obie", GID: 990, GroupsOf: groups}
	noGroup := Policy{SelfUID: 997, Group: "obie", GID: -1, GroupsOf: groups}
	for name, tc := range map[string]struct {
		policy Policy
		cred   Cred
		want   bool
	}{
		"root":                          {p, Cred{UID: 0, GID: 0}, true},
		"obied's own user":              {p, Cred{UID: 997, GID: 997}, true},
		"primary group":                 {p, Cred{UID: 1001, GID: 990}, true},
		"supplementary group":           {p, Cred{UID: 1002, GID: 100}, true},
		"other user":                    {p, Cred{UID: 1001, GID: 100}, false},
		"group lookup fails":            {p, Cred{UID: 1003, GID: 100}, false},
		"unknown gid, member":           {p, Cred{UID: 1002, GID: UnknownGID}, true},
		"unknown gid, primary group":    {p, Cred{UID: 1004, GID: UnknownGID}, true},
		"unknown gid, other user":       {p, Cred{UID: 1001, GID: UnknownGID}, false},
		"unknown gid, root":             {p, Cred{UID: 0, GID: UnknownGID}, true},
		"no group, root":                {noGroup, Cred{UID: 0, GID: 0}, true},
		"no group, own user":            {noGroup, Cred{UID: 997, GID: 997}, true},
		"no group, member gid":          {noGroup, Cred{UID: 1002, GID: 990}, false},
		"no group, other user":          {noGroup, Cred{UID: 1001, GID: 100}, false},
		"uid above int32 range":         {p, Cred{UID: 1 << 31, GID: 100}, false},
		"gid above int32 range":         {p, Cred{UID: 1001, GID: 1<<32 - 1}, false},
		"self uid does not wrap":        {Policy{SelfUID: -1, GID: -1}, Cred{UID: 1<<32 - 1}, false},
		"user database used when unset": {Policy{SelfUID: 997, GID: 1 << 30}, Cred{UID: 1<<31 + 7, GID: 100}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.policy.Allows(tc.cred, discardLogger()); got != tc.want {
				t.Errorf("Allows(%+v) = %v, want %v", tc.cred, got, tc.want)
			}
		})
	}
}

func TestNewPolicy(t *testing.T) {
	p, err := NewPolicy("obie-test-no-such-group")
	if err == nil || p.GID != -1 || p.Group != "obie-test-no-such-group" {
		t.Errorf("NewPolicy(missing group) = %+v, %v; want GID -1 and an error", p, err)
	}
	if !p.Allows(Cred{UID: uint32(p.SelfUID)}, discardLogger()) { // #nosec G115 -- a test UID.
		t.Error("the policy does not admit obied's own user")
	}
}
