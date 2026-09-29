package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeConsole hands out token-1, token-2, … and counts rotations.
type fakeConsole struct {
	mu       sync.Mutex
	rotation int
}

func (f *fakeConsole) response() ConsoleResponse {
	return ConsoleResponse{Enabled: true, Listen: "127.0.0.1:9465", URL: "http://127.0.0.1:9465/",
		Token: "token-" + strconv.Itoa(1+f.rotation)}
}

func (f *fakeConsole) Console() ConsoleResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.response()
}

func (f *fakeConsole) RotateConsoleToken() ConsoleResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotation++
	return f.response()
}

func TestConsoleHandlers(t *testing.T) {
	serving := `{"enabled":true,"listen":"127.0.0.1:9465","url":"http://127.0.0.1:9465/","token":"token-1"}` + "\n"
	rotated := `{"enabled":true,"listen":"127.0.0.1:9465","url":"http://127.0.0.1:9465/","token":"token-2"}` + "\n"
	for name, tc := range map[string]struct {
		console      ConsoleService
		method, path string
		code         int
		body         string
	}{
		"show":                   {&fakeConsole{}, http.MethodGet, ConsolePath, http.StatusOK, serving},
		"rotate":                 {&fakeConsole{}, http.MethodPost, ConsoleTokenPath, http.StatusOK, rotated},
		"rotate needs POST":      {&fakeConsole{}, http.MethodGet, ConsoleTokenPath, http.StatusMethodNotAllowed, ""},
		"show without console":   {nil, http.MethodGet, ConsolePath, http.StatusServiceUnavailable, "the web console is not available\n"},
		"rotate without console": {nil, http.MethodPost, ConsoleTokenPath, http.StatusServiceUnavailable, "the web console is not available\n"},
	} {
		t.Run(name, func(t *testing.T) {
			info := testInfo()
			info.Console = tc.console
			rec := httptest.NewRecorder()
			Handler(info, discardLogger()).ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.code || (tc.body != "" && rec.Body.String() != tc.body) {
				t.Errorf("%s %s = %d %q, want %d %q", tc.method, tc.path, rec.Code, rec.Body.String(), tc.code, tc.body)
			}
		})
	}
}

func TestClientConsoleOverSocket(t *testing.T) {
	path := socketPath(t)
	info := testInfo()
	info.Console = &fakeConsole{}
	s := New(path, "obie-no-such-group", info, discardLogger())
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	client := NewClient(path)
	got, err := client.Console(context.Background())
	if err != nil || got.Token != "token-1" || got.URL != "http://127.0.0.1:9465/" || !got.Enabled {
		t.Fatalf("Console = %+v, %v", got, err)
	}
	got, err = client.RotateConsoleToken(context.Background())
	if err != nil || got.Token != "token-2" {
		t.Fatalf("RotateConsoleToken = %+v, %v", got, err)
	}

	// The test server of the other endpoints has no console.
	other := socketPath(t)
	startServer(t, other, "obie-no-such-group", discardLogger())
	_, err = NewClient(other).Console(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable || !strings.Contains(apiErr.Message, "not available") {
		t.Errorf("Console without console = %v", err)
	}
}
