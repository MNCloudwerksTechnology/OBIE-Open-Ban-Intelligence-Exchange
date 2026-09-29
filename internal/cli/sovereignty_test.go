package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

// ctlFunc runs obiectl against a test node and returns its output.
func ctlFunc(t *testing.T, n testNode) func(args ...string) string {
	return func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", n.socket}, args...), &stdout, &stderr); code != ExitOK {
			t.Fatalf("obiectl %v: exit code = %d, stderr %q", args, code, stderr.String())
		}
		return stdout.String()
	}
}

// waitMode waits until the node reports mode.
func waitMode(t *testing.T, n testNode, mode string) {
	t.Helper()
	client := admin.NewClient(n.socket)
	deadline := time.Now().Add(10 * time.Second)
	for {
		s, err := client.Status(context.Background())
		if err == nil && s.Mode == mode {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("mode is not %s: %+v, %v", mode, s, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// syncBuffer is a bytes.Buffer the daemon can log into while the test
// reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestSovereigntyAgainstInProcessDaemon exercises overrides, the
// allow-list files and reloads end to end: obied in observe mode, a
// force-block, an allow-list file, then reloads that switch to enforce,
// allow-list the blocked address and are rejected.
func TestSovereigntyAgainstInProcessDaemon(t *testing.T) {
	allowFile := filepath.Join(t.TempDir(), "allow.txt")
	writeFile := func(content string) {
		t.Helper()
		if err := os.WriteFile(allowFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("# partners\n198.18.1.0/24\n")
	const mesh = "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n"
	n := newTestNodeWith(t, "", mesh+"allowlist:\n  files: ["+allowFile+"]\n")
	writeNodeConfig := func(nodeKeys, sections string) {
		t.Helper()
		fresh := newTestNodeWith(t, nodeKeys, sections)
		data, err := os.ReadFile(fresh.config)
		if err != nil {
			t.Fatal(err)
		}
		// Same node, new settings: keep this node's paths and ports.
		content := strings.NewReplacer(fresh.stateDir, n.stateDir, fresh.socket, n.socket, fresh.metrics, n.metrics).Replace(string(data))
		if err := os.WriteFile(n.config, []byte(content), 0o600); err != nil { // #nosec G703 -- test config path.
			t.Fatal(err)
		}
	}
	key, err := identity.Create(n.stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	putVerdicts(t, n.stateDir,
		banVerdict("01900000-0000-7000-8000-000000000011", key.PeerID(), "198.18.0.7", 1),
		banVerdict("01900000-0000-7000-8000-000000000012", key.PeerID(), "198.18.1.7", 1))

	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reload := make(chan struct{})
	exit := startDaemon(ctx, t, n, &logs.buf, func(ctx context.Context, args []string) int {
		return runDaemonWith(ctx, reload, args, &bytes.Buffer{}, &logs)
	})
	ctl := ctlFunc(t, n)

	// Observe mode by default, shown first.
	if out := ctl("status"); !strings.HasPrefix(out, "Mode:     OBSERVE (decisions are logged, nothing is blocked)\n") {
		t.Errorf("status:\n%s", out)
	}
	// The allow-list file entry wins over the local autoblock.
	if out := ctl("explain", "198.18.1.7"); !strings.Contains(out, "Decision:              allowed\n") ||
		!strings.Contains(out, "allow-list file entry 198.18.1.0/24 ("+allowFile+":2)") {
		t.Errorf("explain of a file entry:\n%s", out)
	}

	// A force-block on an address without verdicts blocks it; a
	// force-block on the loopback does not.
	if out := ctl("block", "198.18.0.99", "--ttl", "1h", "--note", "scanner"); !strings.Contains(out, "Decision now: block — operator force-block override on ipv4:198.18.0.99 until ") {
		t.Errorf("block:\n%s", out)
	}
	var stdout, ctlStderr bytes.Buffer
	if code := RunCtl([]string{"--socket", n.socket, "block", "127.0.0.1"}, &stdout, &ctlStderr); code != ExitOK ||
		!strings.Contains(ctlStderr.String(), "warning: the force-block does not take effect: allow-listed: built-in range 127.0.0.0/8 (loopback)") {
		t.Errorf("block of loopback: %d %q", code, ctlStderr.String())
	}
	if out := ctl("overrides"); !strings.Contains(out, "ipv4:127.0.0.1") || !strings.Contains(out, "ipv4:198.18.0.99") || !strings.Contains(out, "scanner") {
		t.Errorf("overrides:\n%s", out)
	}
	// A force-allow on the range lifts the local block of 198.18.0.7.
	if out := ctl("allow", "198.18.0.0/24"); !strings.Contains(out, "Decision now: allowed") {
		t.Errorf("allow:\n%s", out)
	}
	if out := ctl("explain", "198.18.0.7"); !strings.Contains(out, "Allow-list/overrides:  operator force-allow override on cidr:198.18.0.0/24\n") {
		t.Errorf("explain after allow:\n%s", out)
	}
	ctl("unoverride", "198.18.0.0/24")
	var blocks admin.DecisionsResponse
	waitBlocks := func(want int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if err := json.Unmarshal([]byte(ctl("decisions", "--state", "block", "--json")), &blocks); err != nil {
				t.Fatal(err)
			}
			if len(blocks.Decisions) == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("blocks = %+v, want %d", blocks.Decisions, want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitBlocks(2) // 198.18.0.7 (autoblock) and 198.18.0.99 (force-block)

	// Reload: enforce mode, and the file now covers 198.18.0.7.
	writeFile("198.18.1.0/24\n198.18.0.7\n")
	writeNodeConfig("  mode: enforce\n", mesh+"allowlist:\n  files: ["+allowFile+"]\n")
	reload <- struct{}{}
	waitMode(t, n, "enforce")
	waitBlocks(1)
	if out := ctl("status"); !strings.HasPrefix(out, "Mode:     ENFORCE (blocks are sent to the enforcer)\n") {
		t.Errorf("status after reload:\n%s", out)
	}

	// waitRejected waits until want reloads have been rejected, so that the
	// last reload is done before the files change again.
	const rejectedMsg = `"msg":"configuration reload rejected; the running configuration is kept"`
	waitRejected := func(want int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for strings.Count(logs.String(), rejectedMsg) < want {
			if time.Now().After(deadline) {
				t.Fatalf("fewer than %d rejected reloads:\n%s", want, logs.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// An invalid configuration is rejected and the running one kept.
	if err := os.WriteFile(n.config, []byte("node:\n  mode: observe\ndecision:\n  quorum: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reload <- struct{}{}
	reload <- struct{}{}
	waitRejected(2)
	// An invalid allow-list file too.
	writeNodeConfig("  mode: observe\n", mesh+"allowlist:\n  files: ["+allowFile+"]\n")
	writeFile("198.18.0.7/33\n")
	reload <- struct{}{}
	reload <- struct{}{}
	waitRejected(4)
	waitMode(t, n, "enforce")
	// A valid reload afterwards.
	writeFile("198.18.1.0/24\n")
	writeNodeConfig("  mode: enforce\n", mesh+"allowlist:\n  files: ["+allowFile+"]\n")
	reload <- struct{}{}
	waitBlocks(2)
	waitMode(t, n, "enforce")

	cancel()
	if code := waitExit(t, exit, &logs.buf); code != ExitOK {
		t.Fatalf("obied exit code = %d:\n%s", code, logs.String())
	}
	lines := logLines(t, &logs.buf)
	var rejected []string
	for _, l := range lines {
		if l["component"] == "reload" && l["msg"] == "configuration reload rejected; the running configuration is kept" {
			rejected = append(rejected, l["error"].(string))
			if next, _ := l["next"].(string); !strings.Contains(next, "sudo obied --check-config") {
				t.Errorf("rejected reload without the next step: %v", l)
			}
		}
	}
	if len(rejected) != 4 || !strings.Contains(rejected[1], "decision.quorum") || !strings.Contains(rejected[3], allowFile+":1") {
		t.Errorf("rejected reloads = %q", rejected)
	}
	if findLog(lines, "enforce", "observe mode: block decision not enforced") == nil ||
		findLog(lines, "enforce", "node mode changed") == nil || findLog(lines, "reload", "configuration reloaded") == nil {
		t.Errorf("missing mode or reload log lines:\n%s", logs.String())
	}
}

// TestRunDaemonInvalidAllowlist: obied does not start with an invalid
// allow-list file.
func TestRunDaemonInvalidAllowlist(t *testing.T) {
	allowFile := filepath.Join(t.TempDir(), "allow.txt")
	n := newTestNodeWith(t, "", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\nallowlist:\n  files: ["+allowFile+"]\n")
	var stdout, stderr bytes.Buffer
	if code := runDaemon(context.Background(), []string{"--config", n.config}, &stdout, &stderr); code != ExitInvalidConfig ||
		!strings.Contains(stderr.String(), "allow-list file") {
		t.Errorf("exit code = %d:\n%s", code, stderr.String())
	}
}

// TestReloadSignals checks that SIGHUP reaches the reload channel and that
// signals are coalesced while a reload is pending.
func TestReloadSignals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reload := reloadSignals(ctx)
	for range 3 {
		if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-reload:
	case <-time.After(5 * time.Second):
		t.Fatal("no reload after SIGHUP")
	}
	time.Sleep(50 * time.Millisecond)
	select {
	case <-reload: // at most one more, coalesced
	default:
	}
	select {
	case <-reload:
		t.Error("SIGHUPs were not coalesced")
	case <-time.After(50 * time.Millisecond):
	}
}
