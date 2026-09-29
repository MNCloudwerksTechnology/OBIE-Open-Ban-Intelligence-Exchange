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

var testIdentity = IdentityResponse{
	PeerID:      "12D3KooWJ1TsijH7H5F74hfAD5XishQz3sxrmAtVY37GtNd9CqYf",
	Fingerprint: "SHA256:ZbYGc9btiEvwHCwiLYKtoHQPKawzVdapJcgfF/R6J7g",
}

var testPeers = []PeerResponse{{
	PeerID:         "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd",
	Name:           "seed",
	Addresses:      []string{"/ip4/192.0.2.1/tcp/4001"},
	ConnectedSince: started.Add(time.Minute),
	LatencySeconds: 0.0125,
	TrustWeight:    0.8,
	Bootstrap:      true,
}}

func testInfo(statuses ...lifecycle.Status) Info {
	return Info{
		Peers:     func() []PeerResponse { return testPeers },
		Version:   "v0.1.0",
		Mode:      func() string { return "observe" },
		StartedAt: started,
		Identity:  testIdentity,
		Status:    func() []lifecycle.Status { return statuses },
		Now:       func() time.Time { return started.Add(90 * time.Second) },
		Explain:   testExplain,
		Decisions: testDecisions,
		Overrides: newFakeOverrides(),
	}
}

func TestStatusHandler(t *testing.T) {
	h := Handler(testInfo(
		lifecycle.Status{Name: "ops", State: lifecycle.StateRunning, Ready: true},
		lifecycle.Status{Name: "admin", State: lifecycle.StateStarting},
		lifecycle.Status{Name: "mesh", State: lifecycle.StateRunning, Ready: true, Detail: "degraded: 0 peers connected"},
	), discardLogger())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, StatusPath, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET %s = %d %q", StatusPath, rec.Code, rec.Header().Get("Content-Type"))
	}
	want := `{"version":"v0.1.0","mode":"observe","started_at":"2026-09-27T10:00:00Z","uptime_seconds":90,"ready":false,` +
		`"subsystems":{"admin":{"state":"starting","ready":false},` +
		`"mesh":{"state":"running","ready":true,"detail":"degraded: 0 peers connected"},"ops":{"state":"running","ready":true}}}` + "\n"
	if rec.Body.String() != want {
		t.Errorf("body =\n%s\nwant\n%s", rec.Body.String(), want)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, StatusPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s = %d, want 405", StatusPath, rec.Code)
	}
}

func TestPeersHandler(t *testing.T) {
	for name, tc := range map[string]struct {
		peers func() []PeerResponse
		want  string
	}{
		"peers": {func() []PeerResponse { return testPeers }, `{"peers":[{"peer_id":"12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd",` +
			`"name":"seed","addresses":["/ip4/192.0.2.1/tcp/4001"],"connected_since":"2026-09-27T10:01:00Z",` +
			`"latency_seconds":0.0125,"trust_weight":0.8,"bootstrap":true}]}` + "\n"},
		"none":    {func() []PeerResponse { return nil }, `{"peers":[]}` + "\n"},
		"no mesh": {nil, `{"peers":[]}` + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			info := testInfo()
			info.Peers = tc.peers
			rec := httptest.NewRecorder()
			Handler(info, discardLogger()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PeersPath, nil))
			if rec.Code != http.StatusOK || rec.Body.String() != tc.want {
				t.Errorf("GET %s = %d\n%s\nwant\n%s", PeersPath, rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestClientPeersOverSocket(t *testing.T) {
	path := socketPath(t)
	startServer(t, path, "obie-no-such-group", discardLogger())

	got, err := NewClient(path).Peers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Peers, testPeers) {
		t.Errorf("Peers = %+v, want %+v", got.Peers, testPeers)
	}
	if got.Peers[0].Latency() != 12500*time.Microsecond {
		t.Errorf("Latency = %v", got.Peers[0].Latency())
	}
}

func TestIdentityHandler(t *testing.T) {
	h := Handler(testInfo(), discardLogger())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, IdentityPath, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET %s = %d %q", IdentityPath, rec.Code, rec.Header().Get("Content-Type"))
	}
	want := `{"peer_id":"12D3KooWJ1TsijH7H5F74hfAD5XishQz3sxrmAtVY37GtNd9CqYf",` +
		`"fingerprint":"SHA256:ZbYGc9btiEvwHCwiLYKtoHQPKawzVdapJcgfF/R6J7g"}` + "\n"
	if rec.Body.String() != want {
		t.Errorf("body =\n%s\nwant\n%s", rec.Body.String(), want)
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

func TestClientIdentityOverSocket(t *testing.T) {
	path := socketPath(t)
	startServer(t, path, "obie-no-such-group", discardLogger())

	got, err := NewClient(path).Identity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *got != testIdentity {
		t.Errorf("Identity = %+v, want %+v", got, testIdentity)
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
	if !strings.Contains(logs.String(), "admin socket group not found") || !strings.Contains(logs.String(), "sudo groupadd --system obie-no-such-group") {
		t.Errorf("logs = %q, want a warning about the missing group with the next step", logs.String())
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

func TestSocketGroupOfProcessNeedsNoChown(t *testing.T) {
	g, err := user.LookupGroupId(strconv.Itoa(os.Getegid()))
	if err != nil {
		t.Skipf("effective group has no name: %v", err)
	}
	// A system call filter without @chown, as in the systemd unit.
	orig := chown
	chown = func(string, int, int) error { return syscall.EPERM }
	t.Cleanup(func() { chown = orig })

	path := socketPath(t)
	startServer(t, path, g.Name, discardLogger())
	if _, err := NewClient(path).Status(context.Background()); err != nil {
		t.Errorf("Status: %v", err)
	}
}

// supplementaryGroup returns a named group of the test process other than
// its effective group, or skips the test.
func supplementaryGroup(t *testing.T) *user.Group {
	t.Helper()
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, gid := range groups {
		if gid == os.Getegid() {
			continue
		}
		if g, err := user.LookupGroupId(strconv.Itoa(gid)); err == nil {
			return g
		}
	}
	t.Skip("the test user has no named supplementary group")
	return nil
}

func TestSocketSupplementaryGroupApplied(t *testing.T) {
	g := supplementaryGroup(t)
	path := socketPath(t)
	startServer(t, path, g.Name, discardLogger())
	if gid := socketGID(path); strconv.Itoa(gid) != g.Gid {
		t.Errorf("socket gid = %d, want %s (%s)", gid, g.Gid, g.Name)
	}
}

func TestSocketChgrpFailure(t *testing.T) {
	g := supplementaryGroup(t)
	orig := chown
	chown = func(string, int, int) error { return syscall.EPERM }
	t.Cleanup(func() { chown = orig })

	path := socketPath(t)
	srv := New(path, g.Name, testInfo(), discardLogger())
	err := srv.Start(context.Background())
	if err == nil {
		_ = srv.Stop(context.Background())
		t.Fatal("Start succeeded although the chgrp failed")
	}
	if !strings.Contains(err.Error(), "chgrp admin socket to "+g.Name) {
		t.Errorf("Start error = %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("socket left behind after a failed chgrp: %v", err)
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

// TestClientPermissionDenied checks that a socket the user may not use
// yields an error matching fs.ErrPermission that says what to do.
func TestClientPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may use every socket")
	}
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	_, err = NewClient(path).Status(context.Background())
	if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), "permission denied on admin socket "+path+": run as root") {
		t.Errorf("Status = %v, want a permission error naming %s", err, path)
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
