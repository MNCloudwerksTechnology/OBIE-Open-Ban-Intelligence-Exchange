package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

func TestWriteEnforcedTable(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	resp := &admin.EnforcedResponse{Mode: "enforce", Entries: []admin.EnforcedEntry{
		{Prefix: "198.51.100.0/24", ExpiresAt: now.Add(90*time.Minute + 500*time.Millisecond)},
		{Prefix: "2001:db8::1/128", ExpiresAt: now.Add(-time.Second)},
	}}
	if err := writeEnforcedTable(&out, resp, now); err != nil {
		t.Fatal(err)
	}
	want := `PREFIX           EXPIRES               REMAINING
198.51.100.0/24  2026-09-28T13:30:00Z  1h30m0s
2001:db8::1/128  2026-09-28T11:59:59Z  0s
`
	if out.String() != want {
		t.Errorf("table =\n%s\nwant\n%s", out.String(), want)
	}
	for mode, want := range map[string]string{
		"enforce": "No entries applied.\n",
		"observe": "No entries applied: the node is in observe mode.\n",
	} {
		out.Reset()
		if err := writeEnforcedTable(&out, &admin.EnforcedResponse{Mode: mode}, now); err != nil || out.String() != want {
			t.Errorf("empty table in %s mode = %q, %v", mode, out.String(), err)
		}
	}
}

func TestEnforcedUsage(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"argument":    {[]string{"enforced", "all"}, ExitUsage, `unexpected argument "all"`},
		"not running": {[]string{"enforced"}, ExitFailure, "obied is not running"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--socket", "/nonexistent/obie.sock"}, tc.args...)
			if code := RunCtl(args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("exit code = %d, stderr %q; want %d containing %q", code, stderr.String(), tc.code, tc.stderr)
			}
		})
	}
}

// TestObiectlEnforcedAgainstInProcessDaemon starts obied in enforce mode
// with the dry-run backend and a local ban verdict: the block is applied
// and listed by obiectl enforced, and every add is logged as JSON.
func TestObiectlEnforcedAgainstInProcessDaemon(t *testing.T) {
	n := newTestNodeWith(t, "  mode: enforce\n", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n")
	key, err := identity.Create(n.stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	putVerdicts(t, n.stateDir,
		banVerdict("01900000-0000-7000-8000-000000000001", key.PeerID(), "198.18.0.7", 0.8),
		banVerdict("01900000-0000-7000-8000-000000000002", key.PeerID(), "203.0.113.9", 1)) // allow-listed

	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &stderr, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderr)
	})
	ctl := func(args ...string) string {
		t.Helper()
		var stdout, ctlStderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", n.socket}, args...), &stdout, &ctlStderr); code != ExitOK {
			t.Fatalf("obiectl %v: exit code = %d, stderr %q", args, code, ctlStderr.String())
		}
		return stdout.String()
	}

	var resp admin.EnforcedResponse
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := json.Unmarshal([]byte(ctl("enforced", "--json")), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Entries) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp.Mode != "enforce" || len(resp.Entries) != 1 || resp.Entries[0].Prefix != "198.18.0.7/32" ||
		time.Until(resp.Entries[0].ExpiresAt) <= 0 {
		t.Errorf("enforced = %+v", resp)
	}
	if out := ctl("enforced"); !strings.HasPrefix(out, "PREFIX") || !strings.Contains(out, "198.18.0.7/32") {
		t.Errorf("enforced table:\n%s", out)
	}
	if out := ctl("status"); !strings.Contains(out, "enforcing via dryrun: 1 entries") {
		t.Errorf("status lacks the enforcement:\n%s", out)
	}

	cancel()
	if code := waitExit(t, exit, &stderr); code != ExitOK {
		t.Fatalf("obied exit code = %d:\n%s", code, stderr.String())
	}
	add := findLog(logLines(t, &stderr), "enforce", "dryrun: add")
	if add == nil || add["prefix"] != "198.18.0.7/32" || add["expires_at"] == nil || add["timeout"] == nil {
		t.Errorf("dryrun add log line = %v", add)
	}
}
