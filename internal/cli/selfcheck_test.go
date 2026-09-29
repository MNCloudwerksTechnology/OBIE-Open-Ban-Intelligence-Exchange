package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/selfcheck"
)

// useSelfCheckHost makes obied self-check check this host, but without
// Fail2Ban and an SSH session and with a synchronized clock, so that its
// result does not depend on the machine the test runs on; session is the
// SSH session's address, "" for none.
func useSelfCheckHost(t *testing.T, session string) {
	t.Helper()
	host := selfCheckEnv
	t.Cleanup(func() { selfCheckEnv = host })
	fail2ban := filepath.Join(t.TempDir(), "fail2ban")
	selfCheckEnv = func(configPath, serviceUser, version string) selfcheck.Env {
		env := host(configPath, serviceUser, version)
		env.Clock = func() (selfcheck.ClockState, error) { return selfcheck.ClockState{Synced: true}, nil }
		env.Session = func() (netip.Addr, bool) {
			if session == "" {
				return netip.Addr{}, false
			}
			return netip.MustParseAddr(session), true
		}
		env.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
		env.Fail2BanDir = fail2ban
		return env
	}
}

// newSelfCheckNode is a test node whose admin socket belongs to the test
// user's group, which the test user runs obied as.
func newSelfCheckNode(t *testing.T) (n testNode, serviceUser string) {
	t.Helper()
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	group, err := user.LookupGroupId(strconv.Itoa(os.Getegid()))
	if err != nil {
		t.Skip(err)
	}
	n = newTestNode(t, "")
	data, err := os.ReadFile(n.config)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Replace(string(data), "obie-test-no-such-group", group.Name, 1)
	if err := os.WriteFile(n.config, []byte(content), 0o600); err != nil { // #nosec G703 -- test config path.
		t.Fatal(err)
	}
	return n, me.Username
}

// checkReport runs obied self-check --json and returns its exit code and
// report.
func checkReport(t *testing.T, args ...string) (int, selfcheck.Report) {
	t.Helper()
	code, stdout, stderr := runObied(t, append([]string{"self-check", "--json"}, args...)...)
	var report selfcheck.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("self-check --json: %v\n%s\n%s", err, stdout, stderr)
	}
	return code, report
}

// statusOf returns the status and summary of the check with id.
func statusOf(t *testing.T, r selfcheck.Report, id string) (selfcheck.Status, string) {
	t.Helper()
	for _, c := range r.Checks {
		if c.ID == id {
			return c.Status, c.Summary
		}
	}
	t.Fatalf("report has no check %s", id)
	return "", ""
}

// TestSelfCheckBeforeFirstStart checks a node that was set up but never
// started: nothing is a problem, and every check that cannot see the node
// yet says so as a warning.
func TestSelfCheckBeforeFirstStart(t *testing.T) {
	useSelfCheckHost(t, "")
	n, svc := newSelfCheckNode(t)
	code, stdout, stderr := runObied(t, "self-check", "--config", n.config, "--service-user", svc)
	if code != ExitOK {
		t.Fatalf("exit code %d\n%s\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"OBIE self-check of " + n.config,
		"OK       Configuration  " + n.config + " is valid; the node runs in observe mode",
		"WARNING  Identity       there is no identity yet",
		"WARNING  Node           the node has not been started yet",
		"                        Next: start it: sudo systemctl enable --now obied",
		"WARNING  Peers          stand-alone node",
		"OK       Firewall       not needed yet",
		"Result: 0 problems,",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report lacks %q:\n%s", want, stdout)
		}
	}

	code, report := checkReport(t, "--config", n.config, "--service-user", svc)
	var ids []string
	for _, c := range report.Checks {
		ids = append(ids, c.ID)
	}
	if code != ExitOK || report.Status != selfcheck.Warning ||
		strings.Join(ids, " ") != "config identity admin node peers clock fail2ban firewall session" {
		t.Errorf("JSON report: exit code %d, status %s, checks %v", code, report.Status, ids)
	}
}

// TestSelfCheckAgainstInProcessDaemon checks a running node, the SSH
// session's protection by an override, and the same node once stopped.
func TestSelfCheckAgainstInProcessDaemon(t *testing.T) {
	useSelfCheckHost(t, "85.10.0.7")
	n, svc := newSelfCheckNode(t)
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &stderr, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderr)
	})
	args := []string{"--config", n.config, "--service-user", svc}

	code, report := checkReport(t, args...)
	for id, want := range map[string]string{
		"config":   "is valid",
		"identity": "exists and only " + svc + " can read it; the node's peer ID is 12D3KooW",
		"admin":    "is open to root, " + svc,
		"node":     "is running and ready in observe mode",
	} {
		if status, summary := statusOf(t, report, id); status != selfcheck.OK || !strings.Contains(summary, want) {
			t.Errorf("%s = %s %q, want OK containing %q", id, status, summary, want)
		}
	}
	if status, summary := statusOf(t, report, "session"); status != selfcheck.Warning || !strings.Contains(summary, "85.10.0.7, which OBIE does not protect") {
		t.Errorf("session = %s %q", status, summary)
	}
	if code != ExitOK || report.Status != selfcheck.Warning {
		t.Errorf("exit code %d, status %s", code, report.Status)
	}
	if _, stdout, _ := runObied(t, append([]string{"self-check"}, args...)...); !strings.Contains(stdout, "!! LOCKOUT RISK: your SSH session comes from 85.10.0.7") {
		t.Errorf("text report has no lockout banner:\n%s", stdout)
	}

	ctlFunc(t, n)("allow", "85.10.0.7", "--note", "my SSH session")
	_, report = checkReport(t, args...)
	if status, summary := statusOf(t, report, "session"); status != selfcheck.OK || !strings.Contains(summary, "force-allow override") {
		t.Errorf("session after obiectl allow = %s %q", status, summary)
	}

	cancel()
	if code := waitExit(t, exit, &stderr); code != ExitOK {
		t.Fatalf("obied exit code %d:\n%s", code, stderr.String())
	}
	code, report = checkReport(t, args...)
	if status, summary := statusOf(t, report, "node"); code != ExitFailure || status != selfcheck.Problem ||
		!strings.Contains(summary, "the node is not running") {
		t.Errorf("stopped node: exit code %d, node %s %q", code, status, summary)
	}
}

func TestSelfCheckUsage(t *testing.T) {
	for _, args := range [][]string{{"surplus"}, {"--timeout", "0"}, {"--no-such-flag"}} {
		if code, _, stderr := runObied(t, append([]string{"self-check"}, args...)...); code != ExitUsage {
			t.Errorf("self-check %v: exit code %d, stderr %q", args, code, stderr)
		}
	}
	code, _, stderr := runObied(t, "self-check", "--help")
	if code != ExitOK || !strings.Contains(stderr, "Exit status: 0 no problem") || !strings.Contains(stderr, "-json") {
		t.Errorf("help: exit code %d\n%s", code, stderr)
	}
	if _, _, stderr := runObied(t, "--help"); !strings.Contains(stderr, "  self-check ") {
		t.Errorf("obied --help does not list self-check:\n%s", stderr)
	}
}

func TestSelfCheckWriteError(t *testing.T) {
	useSelfCheckHost(t, "")
	var stderr bytes.Buffer
	config := filepath.Join(t.TempDir(), "missing.yaml")
	if code := RunDaemon([]string{"self-check", "--config", config}, failingWriter{}, &stderr); code != ExitIOError ||
		!strings.Contains(stderr.String(), "writing the report: broken pipe") {
		t.Errorf("exit code %d, stderr %q", code, stderr.String())
	}
}
