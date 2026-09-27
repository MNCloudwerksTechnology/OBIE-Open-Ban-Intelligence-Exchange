package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessPolicy(t *testing.T) {
	groups := func(uid uint32) ([]string, error) {
		switch uid {
		case 1002:
			return []string{"100", "990"}, nil
		case 1003:
			return nil, errors.New("user database unavailable")
		}
		return []string{"100"}, nil
	}
	p := accessPolicy{selfUID: 997, group: "obie", gid: 990, groupsOf: groups}
	noGroup := accessPolicy{selfUID: 997, group: "obie", gid: -1, groupsOf: groups}
	for name, tc := range map[string]struct {
		policy accessPolicy
		cred   peerCred
		want   bool
	}{
		"root":                   {p, peerCred{UID: 0, GID: 0}, true},
		"obied's own user":       {p, peerCred{UID: 997, GID: 997}, true},
		"primary group":          {p, peerCred{UID: 1001, GID: 990}, true},
		"supplementary group":    {p, peerCred{UID: 1002, GID: 100}, true},
		"other user":             {p, peerCred{UID: 1001, GID: 100}, false},
		"group lookup fails":     {p, peerCred{UID: 1003, GID: 100}, false},
		"no group, root":         {noGroup, peerCred{UID: 0, GID: 0}, true},
		"no group, own user":     {noGroup, peerCred{UID: 997, GID: 997}, true},
		"no group, member gid":   {noGroup, peerCred{UID: 1002, GID: 990}, false},
		"no group, other user":   {noGroup, peerCred{UID: 1001, GID: 100}, false},
		"uid above int32 range":  {p, peerCred{UID: 1 << 31, GID: 100}, false},
		"gid above int32 range":  {p, peerCred{UID: 1001, GID: 1<<32 - 1}, false},
		"self uid does not wrap": {accessPolicy{selfUID: -1, gid: -1}, peerCred{UID: 1<<32 - 1}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.policy.allows(tc.cred, discardLogger()); got != tc.want {
				t.Errorf("allows(%+v) = %v, want %v", tc.cred, got, tc.want)
			}
		})
	}
}

func TestAuthorizeMiddleware(t *testing.T) {
	p := accessPolicy{selfUID: 997, group: "obie", gid: -1}
	h := authorize(p, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }), discardLogger())
	for name, tc := range map[string]struct {
		ctx  context.Context
		code int
		body string
	}{
		"allowed":     {context.WithValue(context.Background(), credKey{}, credResult{cred: peerCred{UID: 0}}), http.StatusOK, "ok"},
		"denied":      {context.WithValue(context.Background(), credKey{}, credResult{cred: peerCred{UID: 1001, GID: 100}}), http.StatusForbidden, `uid 1001 is neither root nor a member of group "obie"`},
		"no creds":    {context.Background(), http.StatusForbidden, "cannot verify"},
		"cred error":  {context.WithValue(context.Background(), credKey{}, credResult{err: errors.New("EBADF")}), http.StatusForbidden, "cannot verify"},
		"unsupported": {context.WithValue(context.Background(), credKey{}, credResult{err: errPeerCredUnsupported}), http.StatusOK, "ok"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, StatusPath, nil).WithContext(tc.ctx))
			if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.body) {
				t.Errorf("= %d %q, want %d containing %q", rec.Code, rec.Body.String(), tc.code, tc.body)
			}
		})
	}
}
