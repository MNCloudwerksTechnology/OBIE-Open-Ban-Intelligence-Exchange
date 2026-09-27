package admin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// socketPath returns a socket path in a short temporary directory: Unix
// socket paths are limited to about 100 bytes, which t.TempDir can exceed.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "obie")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "obie.sock")
}

var started = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

func testInfo(statuses ...lifecycle.Status) Info {
	return Info{
		Version:   "v0.1.0",
		Mode:      "observe",
		StartedAt: started,
		Status:    func() []lifecycle.Status { return statuses },
		Now:       func() time.Time { return started.Add(90 * time.Second) },
	}
}

func TestStatusHandler(t *testing.T) {
	h := Handler(testInfo(
		lifecycle.Status{Name: "ops", State: lifecycle.StateRunning, Ready: true},
		lifecycle.Status{Name: "admin", State: lifecycle.StateStarting},
	), discardLogger())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, StatusPath, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET %s = %d %q", StatusPath, rec.Code, rec.Header().Get("Content-Type"))
	}
	want := `{"version":"v0.1.0","mode":"observe","started_at":"2026-09-27T10:00:00Z","uptime_seconds":90,"ready":false,` +
		`"subsystems":{"admin":{"state":"starting","ready":false},"ops":{"state":"running","ready":true}}}` + "\n"
	if rec.Body.String() != want {
		t.Errorf("body =\n%s\nwant\n%s", rec.Body.String(), want)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, StatusPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s = %d, want 405", StatusPath, rec.Code)
	}
}

// startServer starts the admin subsystem on path and stops it at cleanup.
func startServer(t *testing.T, path, group string, log *slog.Logger) {
	t.Helper()
	s := New(path, group, testInfo(lifecycle.Status{Name: "admin", State: lifecycle.StateRunning, Ready: true}), log)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
}

func TestClientStatusOverSocket(t *testing.T) {
	path := socketPath(t)
	startServer(t, path, "obie-no-such-group", discardLogger())

	got, err := NewClient(path).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := &StatusResponse{
		Version: "v0.1.0", Mode: "observe", StartedAt: started, UptimeSeconds: 90, Ready: true,
		Subsystems: map[string]SubsystemStatus{"admin": {State: lifecycle.StateRunning, Ready: true}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Status = %+v, want %+v", got, want)
	}
	if got.Uptime() != 90*time.Second {
		t.Errorf("Uptime = %v", got.Uptime())
	}
}

func TestSocketModeAndMissingGroup(t *testing.T) {
	path := socketPath(t)
	var logs bytes.Buffer
	startServer(t, path, "obie-no-such-group", slog.New(slog.NewTextHandler(&logs, nil)))

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Type() != os.ModeSocket || fi.Mode().Perm() != SocketMode {
		t.Errorf("socket mode = %v, want socket with %v", fi.Mode(), SocketMode)
	}
	if !strings.Contains(logs.String(), "admin socket group not found") {
		t.Errorf("logs = %q, want a warning about the missing group", logs.String())
	}
}

func TestSocketGroupApplied(t *testing.T) {
	g, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	if err != nil {
		t.Skipf("primary group has no name: %v", err)
	}
	path := socketPath(t)
	startServer(t, path, g.Name, discardLogger())

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gid := fi.Sys().(*syscall.Stat_t).Gid; strconv.Itoa(int(gid)) != g.Gid {
		t.Errorf("socket gid = %d, want %s (%s)", gid, g.Gid, g.Name)
	}
}

func TestStaleSocketRemoved(t *testing.T) {
	path := socketPath(t)
	// A socket file nobody listens on, as left behind by a crashed daemon.
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale socket not left behind: %v", err)
	}

	startServer(t, path, "obie-no-such-group", discardLogger())
	if _, err := NewClient(path).Status(context.Background()); err != nil {
		t.Errorf("Status after replacing stale socket: %v", err)
	}
}

func TestRefusesSocketInUse(t *testing.T) {
	path := socketPath(t)
	startServer(t, path, "obie-no-such-group", discardLogger())

	second := New(path, "obie-no-such-group", testInfo(), discardLogger())
	err := second.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "another obied running") {
		t.Fatalf("second Start = %v, want in-use error", err)
	}
	if _, err := NewClient(path).Status(context.Background()); err != nil {
		t.Errorf("first server no longer reachable: %v", err)
	}
}

func TestRefusesSocketItCannotProbe(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses socket permissions")
	}
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	// A daemon we may not connect to must not lose its socket.
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	err = New(path, "obie", testInfo(), discardLogger()).Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot tell whether") {
		t.Fatalf("Start = %v, want a refusal", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("socket of the other process was removed: %v", err)
	}
}

func TestRefusesNonSocketFile(t *testing.T) {
	path := socketPath(t)
	if err := os.WriteFile(path, []byte("important"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := New(path, "obie", testInfo(), discardLogger()).Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("Start = %v, want not-a-socket error", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "important" { // #nosec G304 -- test file.
		t.Error("regular file was replaced")
	}
}

func TestClientDaemonNotRunning(t *testing.T) {
	missing := socketPath(t)

	stale := socketPath(t)
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()

	for name, path := range map[string]string{"no socket file": missing, "stale socket file": stale} {
		t.Run(name, func(t *testing.T) {
			_, err := NewClient(path).Status(context.Background())
			if !errors.Is(err, ErrDaemonNotRunning) || !strings.Contains(err.Error(), path) {
				t.Errorf("Status = %v, want ErrDaemonNotRunning naming %s", err, path)
			}
		})
	}
}

func TestClientReportsHTTPErrors(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == StatusPath {
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	_, err = NewClient(path).Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "500 Internal Server Error: boom") {
		t.Errorf("Status = %v, want the HTTP error", err)
	}
}
