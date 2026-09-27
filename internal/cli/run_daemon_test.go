package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

// testNode describes the listeners of a daemon started by a test.
type testNode struct {
	config  string // path of the configuration file
	socket  string // admin socket
	metrics string // ops listen address
}

// newTestNode writes a configuration with a temporary admin socket and a
// free ops port; extra is appended to the YAML.
func newTestNode(t *testing.T, extra string) testNode {
	t.Helper()
	// Unix socket paths are limited to about 100 bytes; t.TempDir can exceed that.
	dir, err := os.MkdirTemp("", "obie")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	n := testNode{socket: filepath.Join(dir, "obie.sock"), metrics: freeAddr(t)}
	n.config = writeConfig(t, fmt.Sprintf(
		"admin:\n  socket: %s\n  socket_group: obie-test-no-such-group\nmetrics:\n  listen: %s\n%s",
		n.socket, n.metrics, extra))
	return n
}

// freeAddr returns a loopback address with a port that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

// startDaemon runs obied for n in the background until ctx is canceled and
// waits until its admin API answers. The returned channel yields the exit code.
func startDaemon(ctx context.Context, t *testing.T, n testNode, stderr *bytes.Buffer, run func(ctx context.Context, args []string) int) <-chan int {
	t.Helper()
	exit := make(chan int, 1)
	go func() { exit <- run(ctx, []string{"--config", n.config}) }()

	client := admin.NewClient(n.socket)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := client.Status(ctx); err == nil {
			return exit
		}
		select {
		case code := <-exit:
			t.Fatalf("obied exited early with %d:\n%s", code, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("obied did not become reachable:\n%s", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitExit(t *testing.T, exit <-chan int, stderr *bytes.Buffer) int {
	t.Helper()
	select {
	case code := <-exit:
		return code
	case <-time.After(15 * time.Second):
		t.Fatalf("obied did not shut down:\n%s", stderr.String())
		return -1
	}
}

// logLines parses the JSON log lines in stderr.
func logLines(t *testing.T, stderr *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(stderr.Bytes()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("stderr line %q is not JSON: %v", sc.Text(), err)
		}
		if m["component"] == nil {
			t.Errorf("log line without component: %s", sc.Text())
		}
		lines = append(lines, m)
	}
	return lines
}

// findLog returns the first line with component and msg, or nil.
func findLog(lines []map[string]any, component, msg string) map[string]any {
	for _, l := range lines {
		if l["component"] == component && l["msg"] == msg {
			return l
		}
	}
	return nil
}

func TestRunDaemonGracefulShutdown(t *testing.T) {
	n := newTestNode(t, "")
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &stderr, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &stdout, &stderr)
	})

	resp, err := http.Get("http://" + n.metrics + "/readyz") // #nosec G107 -- test daemon URL.
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /readyz = %d, want 200 once started", resp.StatusCode)
	}

	cancel()
	if code := waitExit(t, exit, &stderr); code != ExitOK {
		t.Fatalf("exit code = %d, want %d:\n%s", code, ExitOK, stderr.String())
	}
	if _, err := os.Lstat(n.socket); !os.IsNotExist(err) {
		t.Errorf("admin socket left behind after shutdown: %v", err)
	}
	lines := logLines(t, &stderr)
	for _, msg := range []string{"obied started", "shutdown requested", "shutdown complete"} {
		if findLog(lines, "obied", msg) == nil {
			t.Errorf("no %q log line:\n%s", msg, stderr.String())
		}
	}
}

func TestRunDaemonStopsOnSIGTERM(t *testing.T) {
	n := newTestNode(t, "node:\n  shutdown_timeout: 5s\n")
	var stdout, stderr bytes.Buffer
	exit := startDaemon(context.Background(), t, n, &stderr, func(_ context.Context, args []string) int {
		return RunDaemon(args, &stdout, &stderr)
	})

	// RunDaemon has registered its signal handler before starting subsystems.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := waitExit(t, exit, &stderr); code != ExitOK {
		t.Fatalf("exit code = %d, want %d:\n%s", code, ExitOK, stderr.String())
	}
	if l := findLog(logLines(t, &stderr), "obied", "shutdown requested"); l == nil || l["timeout"] != "5s" {
		t.Errorf("shutdown line = %v, want timeout 5s", l)
	}
}

func TestRunDaemonStartFailure(t *testing.T) {
	// The ops port is taken, so the first subsystem fails.
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	n := newTestNode(t, "")
	n.config = writeConfig(t, fmt.Sprintf("admin:\n  socket: %s\nmetrics:\n  listen: %s\nlog:\n  level: warn\n",
		n.socket, taken.Addr()))

	var stdout, stderr bytes.Buffer
	if code := runDaemon(context.Background(), []string{"--config", n.config}, &stdout, &stderr); code != ExitFailure {
		t.Fatalf("exit code = %d, want %d:\n%s", code, ExitFailure, stderr.String())
	}
	l := findLog(logLines(t, &stderr), "obied", "obied failed")
	if l == nil {
		t.Fatalf("no failure line:\n%s", stderr.String())
	}
	if msg, _ := l["error"].(string); !strings.Contains(msg, "startup failed: start subsystem ops") ||
		!strings.Contains(msg, "address already in use") {
		t.Errorf("failure line error = %q", msg)
	}
	if _, err := os.Lstat(n.socket); !os.IsNotExist(err) {
		t.Errorf("admin socket created although startup failed first: %v", err)
	}
}

func TestRunDaemonAdminFailureStopsOps(t *testing.T) {
	n := newTestNode(t, "")
	// A regular file where the socket should go makes the admin API fail.
	if err := os.WriteFile(n.socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runDaemon(context.Background(), []string{"--config", n.config}, &stdout, &stderr); code != ExitFailure {
		t.Fatalf("exit code = %d, want %d:\n%s", code, ExitFailure, stderr.String())
	}
	lines := logLines(t, &stderr)
	stopped := false
	for _, l := range lines {
		if l["msg"] == "subsystem stopped" && l["subsystem"] == "ops" {
			stopped = true
		}
	}
	if !stopped {
		t.Errorf("ops was not stopped after admin failed:\n%s", stderr.String())
	}
	if l := findLog(lines, "obied", "obied failed"); l == nil || !strings.Contains(l["error"].(string), "start subsystem admin") {
		t.Errorf("failure line = %v", l)
	}
	if _, err := net.DialTimeout("tcp", n.metrics, time.Second); err == nil {
		t.Error("ops port still open after failed startup")
	}
}

func TestRunDaemonCanceledDuringStartup(t *testing.T) {
	n := newTestNode(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := runDaemon(ctx, []string{"--config", n.config}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("exit code = %d, want %d:\n%s", code, ExitOK, stderr.String())
	}
	if findLog(logLines(t, &stderr), "obied", "shutdown requested during startup") == nil {
		t.Errorf("no startup-interrupted line:\n%s", stderr.String())
	}
}
