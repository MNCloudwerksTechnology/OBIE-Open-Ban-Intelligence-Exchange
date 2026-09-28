package cli

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// fakeTeardown replaces the nftables teardown for the test, counting calls
// and returning err.
func fakeTeardown(t *testing.T, err error) *int {
	t.Helper()
	calls := 0
	orig := teardownFirewall
	teardownFirewall = func(context.Context) error { calls++; return err }
	t.Cleanup(func() { teardownFirewall = orig })
	return &calls
}

func TestTeardownFirewall(t *testing.T) {
	calls := fakeTeardown(t, nil)
	code, stdout, stderr := runObied(t, "teardown-firewall")
	if code != ExitOK || *calls != 1 || stdout != "" || !strings.Contains(stderr, "table inet obie removed") {
		t.Errorf("code %d, calls %d, stdout %q, stderr %q", code, *calls, stdout, stderr)
	}
}

func TestTeardownFirewallOnStop(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		wantCalls int
		wantErr   string
	}{
		{"default keeps the blocks", "enforce:\n  backend: nftables\n", 0, "keeping table inet obie"},
		{"teardown_on_stop", "enforce:\n  backend: nftables\n  nftables:\n    teardown_on_stop: true\n", 1, "removed"},
		{"other backend", "enforce:\n  backend: dryrun\n  nftables:\n    teardown_on_stop: true\n", 0, "keeping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := fakeTeardown(t, nil)
			code, _, stderr := runObied(t, "teardown-firewall", "--on-stop", "--config", writeConfig(t, tt.config))
			if code != ExitOK || *calls != tt.wantCalls || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("code %d, calls %d, stderr %q", code, *calls, stderr)
			}
		})
	}
}

func TestTeardownFirewallErrors(t *testing.T) {
	calls := fakeTeardown(t, errors.New("netlink: boom"))
	if code, _, stderr := runObied(t, "teardown-firewall"); code != ExitFailure || !strings.Contains(stderr, "netlink: boom") {
		t.Errorf("failed teardown: code %d, stderr %q", code, stderr)
	}
	code, _, stderr := runObied(t, "teardown-firewall", "--on-stop", "--config", writeConfig(t, "enforce:\n  backend: iptables\n"))
	if code != ExitInvalidConfig || !strings.Contains(stderr, "enforce.backend") {
		t.Errorf("invalid config: code %d, stderr %q", code, stderr)
	}
	if code, _, _ := runObied(t, "teardown-firewall", "extra"); code != ExitUsage {
		t.Errorf("extra argument: code %d", code)
	}
	if *calls != 1 {
		t.Errorf("teardown called %d times, want 1", *calls)
	}
}

// TestTeardownFirewallNeedsCapNetAdmin runs the real teardown
// unprivileged: the error names the missing capability.
func TestTeardownFirewallNeedsCapNetAdmin(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("needs an unprivileged user on Linux")
	}
	code, _, stderr := runObied(t, "teardown-firewall")
	if code != ExitFailure || !strings.Contains(stderr, "CAP_NET_ADMIN") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}
