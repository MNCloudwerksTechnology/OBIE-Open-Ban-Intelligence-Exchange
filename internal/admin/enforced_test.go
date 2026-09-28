package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnforcedHandler(t *testing.T) {
	applied := []EnforcedEntry{{Prefix: "203.0.113.7/32", ExpiresAt: blockedUntil}}
	tests := []struct {
		name     string
		enforced func(context.Context) ([]EnforcedEntry, error)
		code     int
		body     string
	}{
		{name: "entries", enforced: func(context.Context) ([]EnforcedEntry, error) { return applied, nil }, code: http.StatusOK,
			body: `{"mode":"observe","entries":[{"prefix":"203.0.113.7/32","expires_at":"2026-09-27T11:00:00Z"}]}` + "\n"},
		{name: "none", enforced: func(context.Context) ([]EnforcedEntry, error) { return nil, nil }, code: http.StatusOK,
			body: `{"mode":"observe","entries":[]}` + "\n"},
		{name: "backend failure", enforced: func(context.Context) ([]EnforcedEntry, error) { return nil, errors.New("netlink: EPERM") },
			code: http.StatusInternalServerError, body: "listing the enforced entries failed; see the obied log\n"},
		{name: "no backend", code: http.StatusServiceUnavailable, body: "the enforcement backend is not available\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := testInfo()
			info.Enforced = tt.enforced
			rec := httptest.NewRecorder()
			Handler(info, discardLogger()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, EnforcedPath, nil))
			if rec.Code != tt.code || rec.Body.String() != tt.body {
				t.Errorf("GET %s = %d\n%s\nwant %d\n%s", EnforcedPath, rec.Code, rec.Body.String(), tt.code, tt.body)
			}
		})
	}
}

func TestClientEnforcedOverSocket(t *testing.T) {
	path := socketPath(t)
	startServer(t, path, "obie-no-such-group", discardLogger())
	// The test server has no enforcement backend.
	_, err := NewClient(path).Enforced(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable || !strings.Contains(apiErr.Message, "not available") {
		t.Errorf("Enforced error = %v", err)
	}
}
