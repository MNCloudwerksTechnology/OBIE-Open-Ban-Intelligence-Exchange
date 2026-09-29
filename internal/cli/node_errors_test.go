package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce/nft"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/statedir"
)

// checkProblem fails unless p has the id, what starts with what, why
// contains why, and every one of next is part of a next step.
func checkProblem(t *testing.T, p problem, id, what, why string, next ...string) {
	t.Helper()
	if p.id != id || !strings.HasPrefix(p.what, what) || !strings.Contains(p.why, why) || len(p.next) == 0 {
		t.Errorf("problem = %+v, want id %s, what %q, why containing %q and a next step", p, id, what, why)
	}
	steps := strings.Join(p.next, "\n")
	for _, want := range next {
		if !strings.Contains(steps, want) {
			t.Errorf("next steps %q lack %q", p.next, want)
		}
	}
}

func TestConfigProblems(t *testing.T) {
	invalid := writeConfig(t, "node:\n  mode: block\n")
	_, err := config.Load(invalid)
	p := configProblem(invalid, err)
	checkProblem(t, p, "config-invalid", "the configuration "+invalid+" is invalid:", "",
		"fix these settings, then check the file again: sudo obied --check-config --config "+invalid, "configuration.md")
	if len(p.details) != 1 || !strings.HasPrefix(p.details[0], invalid+":2: node.mode: must be one of") {
		t.Errorf("details = %q, want file:line: setting: message", p.details)
	}
	if got := configMistake("/etc/obie/obie.yaml", config.Problem{Path: "(document)", Message: "empty"}); got != "/etc/obie/obie.yaml: (document): empty" {
		t.Errorf("mistake without a line = %q", got)
	}

	missing := filepath.Join(t.TempDir(), "obie.yaml")
	_, err = config.Load(missing)
	checkProblem(t, configProblem(missing, err), "config-missing", "the configuration file "+missing+" does not exist", "",
		"sudo obied setup --config "+missing)
	_, err = config.Load(config.DefaultPath + ".no-such-file")
	checkProblem(t, configProblem(config.DefaultPath, err), "config-missing", "", "", "write it after a few questions: sudo obied setup")

	denied := fmt.Errorf("read config: open /etc/obie/obie.yaml: %w", fs.ErrPermission)
	checkProblem(t, configProblem(config.DefaultPath, denied), "config-unreadable",
		"cannot read the configuration file /etc/obie/obie.yaml as user "+currentUserName()+": permission denied",
		"only root and the group obie may read it", "with sudo in front of it")

	yaml := writeConfig(t, "node: [\n")
	_, err = config.Load(yaml)
	checkProblem(t, configProblem(yaml, err), "config-invalid", "the configuration "+yaml, "", "check the file again")
}

func TestIdentityProblems(t *testing.T) {
	dir := newStateDir(t)
	key := identity.Path(dir)
	_, err := identity.Load(dir)
	checkProblem(t, identityProblem(dir, "", err), "identity-missing", "this node has no identity yet: "+key+" does not exist",
		"the node creates its identity key the first time it starts",
		"sudo systemctl enable --now obied", "sudo -u obie obied keygen", "restore the key from your backup to "+key)
	// The commands it suggests keep the state directory the user named.
	checkProblem(t, identityProblem(dir, " --state-dir ./node-a", err), "identity-missing", "this node has no identity yet",
		"", "sudo -u obie obied keygen --state-dir ./node-a")

	gone := filepath.Join(dir, "gone")
	_, err = identity.Load(gone)
	checkProblem(t, identityProblem(gone, "", err), "identity-missing", "this node has no identity yet: its state directory "+gone+" does not exist",
		"state directory and identity key", "--state-dir <directory>")

	denied := fmt.Errorf("read key file: %w", fs.ErrPermission)
	checkProblem(t, identityProblem(dir, "", denied), "identity-unreadable", "cannot read the identity key in "+dir,
		"belongs to the user the node runs as", "with sudo in front of it")

	insecure := fmt.Errorf("%w: %s has mode 0644 and is accessible by group or others; fix with: chmod 600 %s", identity.ErrInsecure, key, key)
	checkProblem(t, identityProblem(dir, "", insecure), "identity-unusable", "insecure key file: "+key+" has mode 0644 and is accessible by group or others",
		"", "fix with: chmod 600 "+key)
	checkProblem(t, identityProblem(dir, "", errors.New("boom")), "identity-unusable", "boom", "", "sudo obied self-check")
}

func TestKeygenAndStateDirProblems(t *testing.T) {
	checkProblem(t, keygenProblem("/var/lib/obie", "", fmt.Errorf("write key file: %w", fs.ErrPermission)), "keygen-failed",
		"cannot create the identity key in /var/lib/obie: write key file: permission denied",
		"must belong to the user the node runs as", "sudo -u obie obied keygen")
	// Replacing a key names the state directory the user named, never the
	// default one of another node.
	checkProblem(t, keygenProblem("./node-a", " --state-dir ./node-a", identity.ErrKeyExists), "identity-exists",
		"node-a/node.key exists already", "", "obied identity --state-dir ./node-a shows its peer ID", "obied keygen --state-dir ./node-a --force")
	newer := fmt.Errorf("%w: /var/lib/obie/FORMAT has format 9, but obied dev only understands format 1 or older; run a newer obied", statedir.ErrNewerFormat)
	checkProblem(t, stateDirProblem(newer), "state-dir-unusable", "state directory has a newer format: /var/lib/obie/FORMAT has format 9", "", "run a newer obied")
	checkProblem(t, stateDirProblem(errors.New("read FORMAT: boom")), "state-dir-unusable", "read FORMAT: boom", "", "sudo obied self-check")
}

func TestTeardownProblems(t *testing.T) {
	checkProblem(t, teardownProblem(nft.ErrPermission), "teardown-failed", "cannot remove the table inet obie: the kernel refused",
		"needs root", "sudo obied teardown-firewall")
	checkProblem(t, teardownProblem(errors.New("netlink: boom")), "teardown-failed", "cannot remove the table inet obie: netlink: boom",
		"", "sudo nft delete table inet obie")
}

func TestStartNext(t *testing.T) {
	for err, want := range map[error]string{
		fmt.Errorf("mesh: listen: %w", syscall.EADDRINUSE):       "another process uses the address",
		fmt.Errorf("admin socket: %w", fs.ErrPermission):         "obied lacks a permission for what the error names: run it as the service",
		errors.New("something else"):                             "sudo obied self-check names what is wrong",
		fmt.Errorf("x: %w", fmt.Errorf("y: %w", syscall.EACCES)): "obied lacks a permission",
	} {
		if got := startNext(err); !strings.HasPrefix(got, want) || !strings.Contains(got, "troubleshooting.md#obied-does-not-start") {
			t.Errorf("startNext(%v) = %q, want it to start with %q", err, got, want)
		}
	}
}

// TestHintsOfAnEmptyOrUnreadyNode checks that obiectl points to the
// self-check when the node has no peers or is not ready.
func TestHintsOfAnEmptyOrUnreadyNode(t *testing.T) {
	dir, err := os.MkdirTemp("", "obie")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "obie.sock")
	info := admin.Info{
		Mode: func() string { return "observe" },
		Status: func() []lifecycle.Status {
			return []lifecycle.Status{{Name: "mesh", State: lifecycle.StateFailed, Error: "listen: address already in use"}}
		},
	}
	s := admin.New(socket, "obie-no-such-group", info, slog.New(slog.DiscardHandler))
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	for _, tc := range []struct{ command, stdout, hint string }{
		{"peers", "No peers connected.\n", "obiectl peers: if this node should have peers, sudo obied self-check tests"},
		{"status", "Ready:    no\n", "obiectl status: the node is not ready; sudo obied self-check says what to do"},
	} {
		var stdout, stderr bytes.Buffer
		if code := RunCtl([]string{"--socket", socket, tc.command}, &stdout, &stderr); code != ExitOK ||
			!strings.Contains(stdout.String(), tc.stdout) || !strings.HasPrefix(stderr.String(), tc.hint) {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", tc.command, code, stdout.String(), stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
		if code := RunCtl([]string{"--socket", socket, tc.command, "--json"}, &stdout, &stderr); code != ExitOK || stderr.Len() > 0 {
			t.Errorf("%s --json: exit code %d, stderr %q; scripts get no hint", tc.command, code, stderr.String())
		}
	}
}
