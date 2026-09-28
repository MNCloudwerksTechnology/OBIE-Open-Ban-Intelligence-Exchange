package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/peercred"
)

func TestAuthorizeMiddleware(t *testing.T) {
	p := peercred.Policy{SelfUID: 997, Group: "obie", GID: -1}
	h := authorize(p, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }), discardLogger())
	for name, tc := range map[string]struct {
		ctx  context.Context
		code int
		body string
	}{
		"allowed":     {context.WithValue(context.Background(), credKey{}, credResult{cred: peercred.Cred{UID: 0}}), http.StatusOK, "ok"},
		"denied":      {context.WithValue(context.Background(), credKey{}, credResult{cred: peercred.Cred{UID: 1001, GID: 100}}), http.StatusForbidden, `uid 1001 is neither root nor a member of group "obie"`},
		"no creds":    {context.Background(), http.StatusForbidden, "cannot verify"},
		"cred error":  {context.WithValue(context.Background(), credKey{}, credResult{err: errors.New("EBADF")}), http.StatusForbidden, "cannot verify"},
		"unsupported": {context.WithValue(context.Background(), credKey{}, credResult{err: peercred.ErrUnsupported}), http.StatusOK, "ok"},
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
