package selfcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// testHost is a node's files in a temporary directory, checked through a
// fake environment in which the test's own user is the service user and
// its own group the admin socket's group.
type testHost struct {
	dir, config, stateDir, socket string
	env                           Env
	node                          *fakeNode
	dialer                        fakeDialer
	clock                         ClockState
	// commands are the programs on the fake PATH, by name.
	commands map[string]func(args []string) ([]byte, error)
	nftErr   error
	session  netip.Addr
}

// newTestHost writes a configuration with the state directory and admin
// socket in a temporary directory; extra are further top-level YAML
// sections.
func newTestHost(t *testing.T, extra string) *testHost {
	t.Helper()
	// Unix socket paths are limited to about 100 bytes; t.TempDir can exceed that.
	dir, err := os.MkdirTemp("", "selfcheck")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	h := &testHost{dir: dir, config: filepath.Join(dir, "obie.yaml"), stateDir: filepath.Join(dir, "state"),
		socket: filepath.Join(dir, "obie.sock")}
	h.writeConfig(t, extra)
	uid, gid := strconv.Itoa(os.Geteuid()), strconv.Itoa(os.Getegid())
	h.env = Env{
		ConfigPath:  h.config,
		ServiceUser: "obie-test",
		Version:     "1.2.3",
		Euid:        func() int { return 0 },
		UserName: func(uid int) string {
			if uid == 0 {
				return "root"
			}
			return "alice"
		},
		Operator: func() string { return "alice" },
		LookupUser: func(name string) (*user.User, error) {
			if name != "obie-test" {
				return nil, user.UnknownUserError(name)
			}
			return &user.User{Username: name, Uid: uid, Gid: gid}, nil
		},
		LookupGroup: func(name string) (*user.Group, error) {
			if name != "obie-test" {
				return nil, user.UnknownGroupError(name)
			}
			return &user.Group{Name: name, Gid: gid}, nil
		},
		InGroup: func(string, string) (bool, error) { return false, nil },
		Now:     func() time.Time { return time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC) },
	}
	// Not running until a test starts it.
	h.node = &fakeNode{statusErr: admin.ErrDaemonNotRunning, identity: admin.IdentityResponse{PeerID: "12D3KooWSelf"}}
	h.env.Node = func(string) NodeClient { return h.node }
	h.dialer = fakeDialer{up: map[string]bool{}}
	h.env.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return h.dialer.dial(ctx, network, address)
	}
	h.clock = ClockState{Synced: true, MaxError: 12 * time.Millisecond}
	h.env.Clock = func() (ClockState, error) { return h.clock, nil }
	// No Fail2Ban, nftables available, no SSH session.
	h.env.Fail2BanDir = filepath.Join(dir, "fail2ban")
	h.env.LookPath = func(file string) (string, error) {
		if h.commands[file] == nil {
			return "", errors.New("executable file not found in $PATH")
		}
		return "/usr/bin/" + file, nil
	}
	h.commands = map[string]func(args []string) ([]byte, error){}
	h.env.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		run := h.commands[filepath.Base(name)]
		if run == nil {
			return nil, errors.New("executable file not found in $PATH")
		}
		return run(args)
	}
	h.env.NFTables = func(context.Context) error { return h.nftErr }
	h.env.Session = func() (netip.Addr, bool) { return h.session, h.session.IsValid() }
	h.env.Allowlist = sovereignty.Env{
		InterfaceAddrs: func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr("192.0.2.10")}, nil },
		LookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("198.51.100.30")}, nil
		},
	}
	return h
}

func (h *testHost) writeConfig(t *testing.T, extra string) {
	t.Helper()
	h.writeConfigWith(t, "", extra)
}

// writeConfigWith writes the configuration with further YAML lines of the
// node section, indented by two spaces, and further top-level sections.
func (h *testHost) writeConfigWith(t *testing.T, nodeKeys, extra string) {
	t.Helper()
	content := fmt.Sprintf("node:\n  state_dir: %s\n%sadmin:\n  socket: %s\n  socket_group: obie-test\n%s",
		h.stateDir, nodeKeys, h.socket, extra)
	if err := os.WriteFile(h.config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run runs the self-check and returns the check with id.
func (h *testHost) run(t *testing.T, id string) Check {
	t.Helper()
	report := Run(context.Background(), h.env)
	for _, c := range report.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %q in %+v", id, report.Checks)
	return Check{}
}

// assertCheck fails unless c has status and its summary contains text and
// every next step contains the corresponding one of next.
func assertCheck(t *testing.T, c Check, status Status, text string, next ...string) {
	t.Helper()
	if c.Status != status || !strings.Contains(c.Summary, text) {
		t.Errorf("%s = %s %q (details %q), want %s containing %q", c.ID, c.Status, c.Summary, c.Details, status, text)
	}
	if len(c.NextSteps) != len(next) {
		t.Errorf("%s next steps = %q, want %d", c.ID, c.NextSteps, len(next))
		return
	}
	for i, n := range next {
		if !strings.Contains(c.NextSteps[i], n) {
			t.Errorf("%s next step %d = %q, want it to contain %q", c.ID, i, c.NextSteps[i], n)
		}
	}
}

func TestNewCheckTakesTheWorstFinding(t *testing.T) {
	c := newCheck("x", "X", ok("a"), warn("b", "fix b"), problem("c", "fix c"), problem("d", "fix c"), ok("e"))
	if c.Status != Problem || c.Summary != "c" || strings.Join(c.Details, ",") != "a,b,d,e" ||
		strings.Join(c.NextSteps, ",") != "fix b,fix c" {
		t.Errorf("check = %+v", c)
	}
	if c := newCheck("x", "X", ok("a"), ok("b")); c.Status != OK || c.Summary != "a" || len(c.NextSteps) != 0 {
		t.Errorf("all OK = %+v", c)
	}
}

func TestReportSumsUp(t *testing.T) {
	h := newTestHost(t, "")
	report := Run(context.Background(), h.env)
	if report.Config != h.config || report.Version != "1.2.3" || report.User != "root" || !report.Root {
		t.Errorf("report header = %+v", report)
	}
	counts := map[Status]int{}
	worst := OK
	var ids []string
	for _, c := range report.Checks {
		ids = append(ids, c.ID)
		counts[c.Status]++
		if c.Status.rank() > worst.rank() {
			worst = c.Status
		}
		if c.Status != OK && len(c.NextSteps) == 0 {
			t.Errorf("%s is %s without a next step", c.ID, c.Status)
		}
	}
	for _, s := range []Status{OK, Warning, Problem} {
		if report.Summary[s] != counts[s] {
			t.Errorf("summary[%s] = %d, want %d", s, report.Summary[s], counts[s])
		}
	}
	if report.Status != worst {
		t.Errorf("status = %s, want %s", report.Status, worst)
	}
	if !slices.Equal(ids, IDs) {
		t.Errorf("checks = %q, want %q", ids, IDs)
	}
}

func TestCheckConfig(t *testing.T) {
	h := newTestHost(t, "")
	assertCheck(t, h.run(t, "config"), OK, "is valid; the node runs in observe mode")

	h.writeConfig(t, "trust:\n  default_weight: 2\nlog:\n  level: loud\n")
	c := h.run(t, "config")
	assertCheck(t, c, Problem, "is invalid: 2 problems", "--check-config")
	if len(c.Details) != 2 || !strings.HasPrefix(c.Details[0], "trust.default_weight (line") {
		t.Errorf("details = %q", c.Details)
	}
	// Checks that need the configuration say so.
	assertCheck(t, h.run(t, "identity"), Warning, "not checked: the configuration could not be loaded", "fix the configuration first")

	h.writeConfig(t, "allowlist:\n  files: ["+filepath.Join(h.dir, "missing.txt")+"]\n")
	assertCheck(t, h.run(t, "config"), Problem, "missing.txt", "--check-config")
	if c := h.run(t, "identity"); strings.Contains(c.Summary, "not checked") {
		t.Errorf("a missing allow-list file keeps the identity from being checked: %+v", c)
	}

	if err := os.Remove(h.config); err != nil {
		t.Fatal(err)
	}
	assertCheck(t, h.run(t, "config"), Problem, "there is no configuration file at", "sudo obied setup")
}

func TestCheckConfigUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	h := newTestHost(t, "")
	h.env.Euid = os.Geteuid
	allowFile := filepath.Join(h.dir, "allow.txt")
	if err := os.WriteFile(allowFile, []byte("192.0.2.0/24\n"), 0); err != nil {
		t.Fatal(err)
	}
	// Closed to this user by design: a warning, as ADR 0027 has it, with the
	// next step to look as root.
	h.writeConfig(t, "allowlist:\n  files: ["+allowFile+"]\n")
	assertCheck(t, h.run(t, "config"), Warning, "is valid, but its allow-list files cannot be read as user alice", "sudo obied self-check")

	if err := os.Chmod(h.config, 0); err != nil {
		t.Fatal(err)
	}
	// The next step checks the same file: h.config is not the default one.
	assertCheck(t, h.run(t, "config"), Warning, "cannot read "+h.config+" as user alice", "sudo obied self-check --config "+h.config)
	assertCheck(t, h.run(t, "identity"), Warning, "not checked", "sudo obied self-check")
	// Stopped, and whether it ran before cannot be seen.
	assertCheck(t, h.run(t, "node"), Warning, "the node is not running, and whether it has run before cannot be told as user alice",
		"sudo obied self-check --config "+h.config+"; to start the node: sudo systemctl enable --now obied")
}

func TestCheckIdentity(t *testing.T) {
	h := newTestHost(t, "")
	// Before the first start: no state directory, no key.
	assertCheck(t, h.run(t, "identity"), Warning, "there is no identity yet: obied creates", "sudo systemctl enable --now obied")

	// After a start (FORMAT stamped), a missing key is a problem.
	if err := os.Mkdir(h.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.stateDir, "FORMAT"), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertCheck(t, h.run(t, "identity"), Problem, "node.key is missing: the next start creates a new identity", "restore node.key")

	key := createKey(t, h.stateDir)
	assertCheck(t, h.run(t, "identity"), OK, "only obie-test can read it; the node's peer ID is "+key)

	keyFile := filepath.Join(h.stateDir, "node.key")
	if err := os.Chmod(keyFile, 0o640); err != nil { // #nosec G302 -- the insecure mode under test.
		t.Fatal(err)
	}
	assertCheck(t, h.run(t, "identity"), Problem, "accessible by group or others", "sudo chmod 600 "+keyFile)
	if err := os.Chmod(keyFile, 0o600); err != nil {
		t.Fatal(err)
	}

	// The key belongs to the test's user, not to another service user.
	h.env.LookupUser = func(name string) (*user.User, error) {
		return &user.User{Username: name, Uid: strconv.Itoa(os.Geteuid() + 1)}, nil
	}
	assertCheck(t, h.run(t, "identity"), Problem, "is owned by", "sudo chown obie-test "+keyFile)

	h.env.LookupUser = func(name string) (*user.User, error) { return nil, user.UnknownUserError(name) }
	assertCheck(t, h.run(t, "identity"), Problem, "the service user obie-test does not exist", "--service-user")
}

func TestCheckIdentityCorrupt(t *testing.T) {
	h := newTestHost(t, "")
	if err := os.Mkdir(h.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.stateDir, "node.key"), []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := h.run(t, "identity")
	assertCheck(t, c, Problem, "corrupted key file", "obied keygen --force")
}

func TestCheckIdentityWithoutPrivileges(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	h := newTestHost(t, "")
	h.env.Euid = os.Geteuid
	createKey(t, h.stateDir)
	if err := os.Chmod(h.stateDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(h.stateDir, 0o700) }) // #nosec G302 -- restore the directory for its removal.
	assertCheck(t, h.run(t, "identity"), Warning, "cannot look into "+h.stateDir+" as user alice", "sudo obied self-check")
}

func TestCheckAdmin(t *testing.T) {
	h := newTestHost(t, "")
	c := h.run(t, "admin")
	assertCheck(t, c, OK, "appears when the node runs; only root, obie-test and the group obie-test may use it")
	if len(c.Details) != 1 || !strings.Contains(c.Details[0], "alice is not in the group obie-test: use sudo obiectl, or join the group with sudo usermod -aG obie-test alice") {
		t.Errorf("details = %q", c.Details)
	}
	h.env.InGroup = func(name, gid string) (bool, error) { return name == "alice" && gid == strconv.Itoa(os.Getegid()), nil }
	if c := h.run(t, "admin"); len(c.Details) != 1 || !strings.Contains(c.Details[0], "alice is in the group obie-test and may use obiectl without sudo") {
		t.Errorf("member details = %q", c.Details)
	}
	h.env.Operator = func() string { return "root" }
	if c := h.run(t, "admin"); len(c.Details) != 1 || !strings.Contains(c.Details[0], "you work as root") {
		t.Errorf("root details = %q", c.Details)
	}

	ln := listenUnix(t, h.socket)
	if err := os.Chmod(h.socket, 0o660); err != nil { // #nosec G302 -- the admin socket's mode.
		t.Fatal(err)
	}
	assertCheck(t, h.run(t, "admin"), OK, "is open to root, obie-test and the group obie-test only (mode 0660)")
	if err := os.Chmod(h.socket, 0o666); err != nil { // #nosec G302 -- the insecure mode under test.
		t.Fatal(err)
	}
	assertCheck(t, h.run(t, "admin"), Problem, "has mode 0666: every user of this server can control the node", "sudo systemctl restart obied")
	if err := os.Chmod(h.socket, 0o660); err != nil { // #nosec G302 -- the admin socket's mode.
		t.Fatal(err)
	}
	h.env.LookupGroup = func(name string) (*user.Group, error) {
		return &user.Group{Name: name, Gid: strconv.Itoa(os.Getegid() + 1)}, nil
	}
	assertCheck(t, h.run(t, "admin"), Problem, "does not belong to the group obie-test", "the unit's Group=")
	_ = ln.Close()

	if err := os.WriteFile(h.socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	assertCheck(t, h.run(t, "admin"), Problem, "is not a socket", "remove it")

	h.env.LookupGroup = func(name string) (*user.Group, error) { return nil, errors.New("unknown group") }
	assertCheck(t, h.run(t, "admin"), Problem, "the group obie-test (admin.socket_group) does not exist", "sudo groupadd --system obie-test")
}
