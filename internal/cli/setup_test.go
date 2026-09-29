package cli

import (
	"bytes"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/setup"
)

// Peer IDs of the documentation's examples.
const (
	friendPeerID  = "12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"
	partnerPeerID = "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd"
)

// testSetupEnv is a host with an SSH session from sessionAddr ("" for
// none) that lets the test write anywhere it may.
func testSetupEnv(sessionAddr string) setupEnv {
	return setupEnv{
		session: func() (netip.Addr, bool) {
			if sessionAddr == "" {
				return netip.Addr{}, false
			}
			return netip.MustParseAddr(sessionAddr), true
		},
		checkWritable: setup.CheckWritable,
		group:         "obie-test-no-such-group",
		groupExists:   func(string) bool { return true },
		euid:          func() int { return 1000 },
		userName:      func() string { return "alice" },
	}
}

// runSetupTest runs obied setup with stdin as the operator's input.
func runSetupTest(t *testing.T, env setupEnv, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = runSetupWith(args, strings.NewReader(stdin), &out, &errOut, env)
	return code, out.String(), errOut.String()
}

func configPathIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "etc", "obie", "obie.yaml")
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSetupInteractiveMatchesNonInteractive answers every question — with
// a wrong answer to each first — and checks that the flags carrying the
// same answers write the same bytes, a configuration obied accepts.
func TestSetupInteractiveMatchesNonInteractive(t *testing.T) {
	path := configPathIn(t)
	env := testSetupEnv("85.10.0.7")
	input := strings.Join([]string{
		"var/lib/obie", "/var/lib/obie", // 1: relative, then absolute
		"audit.jsonl", "", // 2: relative, then the default
		"obie.friend.example:4001", // 3: not a multiaddr
		"/dns4/obie.friend.example/tcp/4001/p2p/" + friendPeerID, "friend", "2", "1",
		"/dns4/obie.friend.example/tcp/4002/p2p/" + friendPeerID, // the same peer again
		"/ip4/198.51.100.20/tcp/4001/p2p/" + partnerPeerID, "", "",
		"",                // done with the peers
		"maybe", "n", "y", // 4: enforce, confirmed
		"198.51.100.7/24", "85.10.0.7 198.51.100.0/24", // 5: host bits, then two entries
		"", // write
	}, "\n") + "\n"
	code, stdout, stderr := runSetupTest(t, env, input, "--config", path)
	if code != ExitOK {
		t.Fatalf("interactive setup: exit code %d\n%s\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"1/5  Where should the node keep its state?", "2/5  Where should the node write its audit log?",
		"3/5  Which peers should this node connect to?", "4/5  Should the node start in observe mode?",
		"5/5  Which addresses must never be blocked?",
		"State directory [/var/lib/obie]: ", "Audit log [/var/log/obie/audit.jsonl]: ",
		"is not an absolute path", "is not an absolute file path", "is not a peer address",
		"is not a trust weight", "is already listed", "Please answer y or n.", "has host bits set",
		"Your SSH session comes from 85.10.0.7, which is not protected yet.",
		"Addresses or networks, separated by spaces, or none [85.10.0.7/32]: ",
		"Start in enforce mode anyway? [y/N]: ",
		"friend (trust 1) /dns4/obie.friend.example/tcp/4001/p2p/" + friendPeerID,
		"Write " + path + "? [Y/n]: ", "Wrote " + path + ".",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("interactive output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "LOCKOUT RISK") {
		t.Errorf("the protected session gets a lockout warning:\n%s", stdout)
	}
	interactive := readText(t, path)
	if code, _, stderr := runObied(t, "--config", path, "--check-config"); code != ExitOK {
		t.Fatalf("obied --check-config rejects the written file: %s\n%s", stderr, interactive)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runSetupTest(t, env, "", "--config", path, "--non-interactive",
		"--state-dir", "/var/lib/obie", "--mode", "enforce",
		"--peer", "/dns4/obie.friend.example/tcp/4001/p2p/"+friendPeerID+",name=friend,weight=1",
		"--peer", "/ip4/198.51.100.20/tcp/4001/p2p/"+partnerPeerID,
		"--allow", "85.10.0.7", "--allow", "198.51.100.0/24")
	if code != ExitOK {
		t.Fatalf("non-interactive setup: exit code %d\n%s\n%s", code, stdout, stderr)
	}
	if got := readText(t, path); got != interactive {
		t.Errorf("the same answers wrote different files:\ninteractive:\n%s\nnon-interactive:\n%s", interactive, got)
	}
}

// TestSetupEnterTakesSafeDefaults checks that pressing Enter everywhere
// writes an observe-mode node that protects the SSH session, and that the
// next steps follow.
func TestSetupEnterTakesSafeDefaults(t *testing.T) {
	path := configPathIn(t)
	code, stdout, stderr := runSetupTest(t, testSetupEnv("85.10.0.7"), strings.Repeat("\n", 7), "--config", path)
	if code != ExitOK {
		t.Fatalf("exit code %d\n%s\n%s", code, stdout, stderr)
	}
	written := readText(t, path)
	for _, want := range []string{"  mode: observe\n", "  state_dir: \"/var/lib/obie\"\n", "  path: \"/var/log/obie/audit.jsonl\"\n",
		"  bootstrap: []\n", "    - \"85.10.0.7/32\"\n"} {
		if !strings.Contains(written, want) {
			t.Errorf("written file lacks %q:\n%s", want, written)
		}
	}
	assertNextSteps(t, stdout, path)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != setup.FileMode {
		t.Errorf("file mode = %v, %v; want %04o", info.Mode(), err, setup.FileMode)
	}
}

// assertNextSteps checks the steps after a successful setup of the file at
// path: start the service, run the self-check of that file, open the
// tutorial.
func assertNextSteps(t *testing.T, stdout, path string) {
	t.Helper()
	i := strings.Index(stdout, "Next steps:")
	if i < 0 {
		t.Fatalf("no next steps:\n%s", stdout)
	}
	rest := stdout[i:]
	start := strings.Index(rest, "sudo systemctl enable --now obied")
	check := strings.Index(rest, "sudo obied self-check"+config.PathFlag(path)+"\n")
	tutorial := strings.Index(rest, TutorialURL)
	if start < 0 || check < start || tutorial < check {
		t.Errorf("next steps are not start, self-check, tutorial:\n%s", rest)
	}
}

// TestNextSteps checks that the next steps select the file written: the
// self-check checks it, and the shipped service, which reads the default
// file, is changed before it starts.
func TestNextSteps(t *testing.T) {
	steps := nextSteps(config.DefaultPath)
	if strings.Contains(steps, "systemctl edit") || !strings.Contains(steps, "       sudo obied self-check\n") {
		t.Errorf("next steps for the default file:\n%s", steps)
	}
	steps = nextSteps("/srv/obie.yaml")
	edit := strings.Index(steps, "The shipped\n     service reads /etc/obie/obie.yaml, so first set both --config in it")
	start := strings.Index(steps, "sudo systemctl edit --full obied\n       sudo systemctl enable --now obied")
	if edit < 0 || start < edit || !strings.Contains(steps, "       sudo obied self-check --config /srv/obie.yaml\n") {
		t.Errorf("next steps for another file:\n%s", steps)
	}
}

// TestSetupNeverReplacesWithoutAsking checks the final question when a
// configuration exists: No by default for a file of the operator's own,
// Yes for the unchanged example; a replaced file is kept as a backup.
func TestSetupNeverReplacesWithoutAsking(t *testing.T) {
	path := configPathIn(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	const own = "node:\n  mode: enforce\n"
	if err := os.WriteFile(path, []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testSetupEnv("")
	answers := strings.Repeat("\n", 5) // state, audit, peers done, observe, allow

	code, stdout, _ := runSetupTest(t, env, answers+"\n", "--config", path)
	if code != ExitOK || !strings.Contains(stdout, "exists already (with settings of your own)") ||
		!strings.Contains(stdout, "? The old file is kept as a backup. [y/N]: ") || !strings.Contains(stdout, "Nothing was written.") {
		t.Errorf("declined replacement: exit code %d\n%s", code, stdout)
	}
	if got := readText(t, path); got != own {
		t.Fatalf("the configuration was replaced without consent:\n%s", got)
	}

	code, stdout, stderr := runSetupTest(t, env, answers+"y\n", "--config", path)
	if code != ExitOK {
		t.Fatalf("accepted replacement: exit code %d\n%s\n%s", code, stdout, stderr)
	}
	if got := readText(t, path+".bak"); got != own || !strings.Contains(stdout, "The previous file is kept as "+path+".bak.") {
		t.Errorf("backup holds %q; output:\n%s", got, stdout)
	}

	// The unchanged example is offered for replacement by default.
	if err := os.WriteFile(path+".example", []byte(readText(t, path)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = runSetupTest(t, env, answers+"\n", "--config", path)
	if code != ExitOK || !strings.Contains(stdout, "(the unchanged example that install.sh installed)") ||
		!strings.Contains(stdout, "kept as a backup. [Y/n]: ") || !strings.Contains(stdout, "kept as "+path+".bak.1.") {
		t.Errorf("unchanged example: exit code %d\n%s", code, stdout)
	}
}

func TestSetupNonInteractiveNeedsForce(t *testing.T) {
	path := configPathIn(t)
	env := testSetupEnv("")
	if code, _, stderr := runSetupTest(t, env, "", "--config", path, "--non-interactive"); code != ExitOK {
		t.Fatalf("first run: exit code %d: %s", code, stderr)
	}
	first := readText(t, path)
	code, _, stderr := runSetupTest(t, env, "", "--config", path, "--non-interactive", "--mode", "enforce")
	if code != ExitFailure || !strings.Contains(stderr, "exists already") || !strings.Contains(stderr, "pass --force") {
		t.Errorf("second run without --force: exit code %d: %s", code, stderr)
	}
	if readText(t, path) != first {
		t.Fatal("the configuration was replaced without --force")
	}
	code, stdout, stderr := runSetupTest(t, env, "", "--config", path, "--non-interactive", "--mode", "enforce", "--force")
	if code != ExitOK || readText(t, path+".bak") != first || !strings.Contains(readText(t, path), "mode: enforce") {
		t.Errorf("run with --force: exit code %d\n%s\n%s", code, stdout, stderr)
	}
	assertNextSteps(t, stdout, path)
}

// TestSetupWithoutPermission checks that the assistant says it needs root
// before it asks anything.
func TestSetupWithoutPermission(t *testing.T) {
	env := testSetupEnv("")
	env.checkWritable = func(string) error {
		return &fs.PathError{Op: "write", Path: "/etc/obie", Err: syscall.EACCES}
	}
	for path, want := range map[string][]string{
		"/etc/obie/obie.yaml": {"cannot write /etc/obie/obie.yaml as user alice: permission denied", "run it as root: sudo obied setup\n"},
		"/srv/obie.yaml":      {"cannot write /srv/obie.yaml as user alice", "run it as root: sudo obied setup --config /srv/obie.yaml\n"},
	} {
		code, stdout, stderr := runSetupTest(t, env, "\n\n\n\n\n\n", "--config", path)
		if code != ExitFailure || stdout != "" {
			t.Errorf("%s: exit code %d, stdout %q", path, code, stdout)
		}
		for _, w := range want {
			if !strings.Contains(stderr, w) {
				t.Errorf("stderr lacks %q: %s", w, stderr)
			}
		}
	}
}

// TestSetupAsRootWithoutPermission checks that root, who cannot write the
// file either (on a read-only file system), is not told to run it as root.
func TestSetupAsRootWithoutPermission(t *testing.T) {
	env := testSetupEnv("")
	env.euid = func() int { return 0 }
	env.userName = func() string { return "root" }
	env.checkWritable = func(string) error {
		return &fs.PathError{Op: "write", Path: "/etc/obie", Err: syscall.EROFS}
	}
	code, stdout, stderr := runSetupTest(t, env, "", "--config", "/etc/obie/obie.yaml")
	if code != ExitFailure || stdout != "" || strings.Contains(stderr, "as root") ||
		!strings.Contains(stderr, "cannot write /etc/obie/obie.yaml as user root: read-only file system") ||
		!strings.Contains(stderr, "choose a place it can write with --config FILE") {
		t.Errorf("exit code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// TestSetupRefusesAPathBeforeAsking checks that a configuration path the
// file cannot be written to fails before the first question.
func TestSetupRefusesAPathBeforeAsking(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "managed.yaml")
	if err := os.WriteFile(target, []byte("node: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "obie.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		link:                            "is not a regular file (a symbolic link?)",
		filepath.Join(dir, "a\nb.yaml"): "holds a line break",
	} {
		code, stdout, stderr := runSetupTest(t, testSetupEnv(""), "\n\n\n\n\n\n", "--config", path)
		if code != ExitFailure || stdout != "" || !strings.Contains(stderr, want) {
			t.Errorf("%q: exit code %d, stdout %q, stderr %q; want %q", path, code, stdout, stderr, want)
		}
	}
	if got := readText(t, target); got != "node: {}\n" {
		t.Errorf("the file behind the link holds %q", got)
	}
}

func TestSetupInputEnds(t *testing.T) {
	path := configPathIn(t)
	code, _, stderr := runSetupTest(t, testSetupEnv(""), "\n\n", "--config", path)
	if code != ExitFailure || !strings.Contains(stderr, "the input ended before every question was answered; nothing was written") ||
		!strings.Contains(stderr, "--non-interactive") {
		t.Errorf("exit code %d: %s", code, stderr)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a file was written")
	}
}

func TestSetupUsageErrors(t *testing.T) {
	path := configPathIn(t)
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--peer", "/ip4/198.51.100.20/tcp/4001/p2p/" + partnerPeerID}, "--peer answers a question up front and needs --non-interactive"},
		{[]string{"--force"}, "--force answers a question up front"},
		{[]string{"--non-interactive", "--mode", "block"}, "--mode: \"block\" is not a mode"},
		{[]string{"--non-interactive", "--peer", "/ip4/198.51.100.20/tcp/4001"}, "--peer: "},
		{[]string{"--non-interactive", "--allow", "example.org"}, "--allow: "},
		{[]string{"--non-interactive", "--state-dir", "obie"}, "--state-dir: "},
		{[]string{"--non-interactive", "--audit-log", "log"}, "--audit-log: "},
		{[]string{"--non-interactive", "--peer", "/ip4/198.51.100.20/tcp/4001/p2p/" + partnerPeerID,
			"--peer", "/ip4/198.51.100.21/tcp/4001/p2p/" + partnerPeerID}, "peer already added"},
		{[]string{"surplus"}, "unexpected argument"},
	}
	for _, tt := range tests {
		code, _, stderr := runSetupTest(t, testSetupEnv(""), "", append([]string{"--config", path}, tt.args...)...)
		if code != ExitUsage || !strings.Contains(stderr, tt.want) {
			t.Errorf("%v: exit code %d, stderr %q; want %q", tt.args, code, stderr, tt.want)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a usage error wrote a file")
	}
}

// TestSetupLockoutWarning checks the banner for a session the written
// configuration leaves unprotected, and the notes for paths the service
// cannot write and for a missing group.
func TestSetupLockoutWarning(t *testing.T) {
	path := configPathIn(t)
	env := testSetupEnv("85.10.0.7")
	env.groupExists = func(string) bool { return false }
	code, stdout, stderr := runSetupTest(t, env, "", "--config", path, "--non-interactive", "--mode", "enforce",
		"--state-dir", "/srv/obie")
	if code != ExitOK {
		t.Fatalf("exit code %d: %s", code, stderr)
	}
	for _, want := range []string{"LOCKOUT RISK: your SSH session comes from 85.10.0.7", "enforce mode: this can happen at any moment",
		"ReadWritePaths=/srv/obie", "The group obie-test-no-such-group does not exist"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if code, stdout, _ = runSetupTest(t, testSetupEnv("85.10.0.7"), "", "--config", path, "--non-interactive", "--force",
		"--allow", "85.10.0.0/24"); code != ExitOK || strings.Contains(stdout, "LOCKOUT RISK") {
		t.Errorf("protected session: exit code %d\n%s", code, stdout)
	}
}

func TestSetupHelp(t *testing.T) {
	code, stdout, _ := runSetupTest(t, testSetupEnv(""), "", "--help")
	if code != ExitOK || !strings.Contains(stdout, "obied setup --non-interactive") || !strings.Contains(stdout, "--peer address[,name=NAME][,weight=0..1]") {
		t.Errorf("exit code %d:\n%s", code, stdout)
	}
	if code, stdout, _ := runObied(t, "--help"); code != ExitOK || !strings.Contains(stdout, "  setup ") {
		t.Errorf("obied --help does not list setup:\n%s", stdout)
	}
}
