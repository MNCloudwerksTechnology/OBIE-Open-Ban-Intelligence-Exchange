package console

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/peercred"
)

func TestLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:9465":          true,
		"127.0.0.1":               true,
		"127.1.2.3:80":            true,
		"localhost:9465":          true,
		"LocalHost:10000":         true,
		"[::1]:9465":              true,
		"[::1]":                   true,
		"[::ffff:127.0.0.1]:9465": true,
		"":                        false,
		"evil.example:9465":       false,
		"evil.example":            false,
		"localhost.:9465":         false,
		"console.localhost:9465":  false,
		"127.0.0.1.nip.io:9465":   false,
		"192.0.2.1:9465":          false,
		"0.0.0.0:9465":            false,
		"[fe80::1%lo]:9465":       false,
		"[::1%lo]:9465":           false,
	} {
		if got := loopbackHost(host); got != want {
			t.Errorf("loopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestCrossSite(t *testing.T) {
	type hdr = map[string]string
	for name, tc := range map[string]struct {
		method  string
		headers hdr
		refused bool
	}{
		"same-origin GET":                   {http.MethodGet, hdr{"Sec-Fetch-Site": "same-origin"}, false},
		"same-origin POST":                  {http.MethodPost, hdr{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:9465"}, false},
		"typed address":                     {http.MethodGet, hdr{"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, false},
		"POST not from a page":              {http.MethodPost, hdr{"Sec-Fetch-Site": "none"}, true},
		"link from another site":            {http.MethodGet, hdr{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, false},
		"link from another port":            {http.MethodGet, hdr{"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, false},
		"iframe from another site":          {http.MethodGet, hdr{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"}, true},
		"image from another site":           {http.MethodGet, hdr{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "image"}, true},
		"fetch from another port":           {http.MethodGet, hdr{"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "cors", "Sec-Fetch-Dest": "empty"}, true},
		"form POST from another site":       {http.MethodPost, hdr{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document", "Origin": "https://evil.example"}, true},
		"form POST from another port":       {http.MethodPost, hdr{"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document", "Origin": "http://127.0.0.1:8080"}, true},
		"unknown fetch site":                {http.MethodGet, hdr{"Sec-Fetch-Site": "same-planet"}, true},
		"old browser GET":                   {http.MethodGet, hdr{}, false},
		"old browser POST, own origin":      {http.MethodPost, hdr{"Origin": "http://127.0.0.1:9465"}, false},
		"old browser POST, other origin":    {http.MethodPost, hdr{"Origin": "http://evil.example"}, true},
		"old browser POST, other port":      {http.MethodPost, hdr{"Origin": "http://127.0.0.1:8080"}, true},
		"old browser POST, null origin":     {http.MethodPost, hdr{"Origin": "null"}, true},
		"non-browser POST":                  {http.MethodPost, hdr{}, false},
		"DELETE from another port":          {http.MethodDelete, hdr{"Sec-Fetch-Site": "same-site"}, true},
		"HEAD navigation from another site": {http.MethodHead, hdr{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://127.0.0.1:9465/login", nil)
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := crossSite(r); (got != "") != tc.refused {
				t.Errorf("crossSite = %q, want refused %v", got, tc.refused)
			}
		})
	}
}

// guardedConsole returns a console whose local-user lookup returns cred
// and err, counting its calls in lookups.
func guardedConsole(t *testing.T, cred peercred.Cred, err error, lookups *int) *Console {
	t.Helper()
	c := newConsole(t, config.Console{Listen: "127.0.0.1:9465"}, &syncBuffer{})
	c.policy = peercred.Policy{SelfUID: 997, Group: "obie", GID: 990,
		GroupsOf: func(uint32) ([]string, error) { return []string{"100"}, nil }}
	c.lookup = func(net.Conn) (peercred.Cred, error) {
		*lookups++
		return cred, err
	}
	return c
}

func TestLocalUserGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	for name, tc := range map[string]struct {
		cred    peercred.Cred
		err     error
		noConn  bool
		code    int
		message string
	}{
		"root":          {cred: peercred.Cred{UID: 0, GID: peercred.UnknownGID}, code: http.StatusOK, message: "ok"},
		"obied's user":  {cred: peercred.Cred{UID: 997, GID: peercred.UnknownGID}, code: http.StatusOK, message: "ok"},
		"group member":  {cred: peercred.Cred{UID: 1001, GID: 990}, code: http.StatusOK, message: "ok"},
		"other user":    {cred: peercred.Cred{UID: 2000000042, GID: peercred.UnknownGID}, code: http.StatusForbidden, message: `local user 2000000042 (uid 2000000042), who is neither root, nor the user obied runs as, nor a member of the group "obie". Only those may use the console, as for obiectl. To admit the user: sudo usermod -aG obie 2000000042`},
		"lookup fails":  {err: errors.New("socket not found"), code: http.StatusForbidden, message: "cannot tell which local user"},
		"unsupported":   {err: peercred.ErrUnsupported, code: http.StatusOK, message: "ok"},
		"no connection": {noConn: true, code: http.StatusForbidden, message: "cannot tell which local user"},
	} {
		t.Run(name, func(t *testing.T) {
			lookups := 0
			c := guardedConsole(t, tc.cred, tc.err, &lookups)
			h := c.localUser(ok)
			ctx := withConn(context.Background(), nil)
			if tc.noConn {
				ctx = context.Background()
			}
			for range 2 { // two requests on one connection
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
				if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.message) {
					t.Fatalf("= %d %q, want %d containing %q", rec.Code, rec.Body.String(), tc.code, tc.message)
				}
			}
			if want := 1; !tc.noConn && lookups != want {
				t.Errorf("looked up the user %d times for one connection, want %d", lookups, want)
			}
		})
	}
}

func TestGuardsRefuseWithSecurityHeaders(t *testing.T) {
	c := newConsole(t, config.Console{Listen: "127.0.0.1:9465"}, &syncBuffer{})
	c.lookup = func(net.Conn) (peercred.Cred, error) {
		return peercred.Cred{UID: uint32(os.Getuid()), GID: peercred.UnknownGID}, nil // #nosec G115 -- a test UID.
	}
	for name, tc := range map[string]struct {
		host    string
		headers map[string]string
		code    int
		message string
	}{
		"DNS rebinding": {host: "evil.example:9465", code: http.StatusMisdirectedRequest,
			message: `answers only requests addressed to 127.0.0.1, [::1] or localhost, not to "evil.example:9465"`},
		"no host":     {host: "", code: http.StatusMisdirectedRequest, message: "answers only requests addressed to"},
		"cross-site":  {host: "127.0.0.1:9465", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors"}, code: http.StatusForbidden, message: "another web site"},
		"passes":      {host: "127.0.0.1:9465", code: http.StatusNotFound, message: "404 page not found"},
		"via forward": {host: "localhost:10000", code: http.StatusNotFound, message: "404 page not found"},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(withConn(context.Background(), nil))
			r.Host = tc.host
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			c.handler.ServeHTTP(rec, r)
			if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.message) {
				t.Errorf("= %d %q, want %d containing %q", rec.Code, rec.Body.String(), tc.code, tc.message)
			}
			for k, v := range map[string]string{
				"Content-Security-Policy":      contentSecurityPolicy,
				"X-Frame-Options":              "DENY",
				"X-Content-Type-Options":       "nosniff",
				"Referrer-Policy":              "no-referrer",
				"Cross-Origin-Opener-Policy":   "same-origin",
				"Cross-Origin-Resource-Policy": "same-origin",
				"Cache-Control":                "no-store",
			} {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Error("CORS header sent")
			}
		})
	}
}
