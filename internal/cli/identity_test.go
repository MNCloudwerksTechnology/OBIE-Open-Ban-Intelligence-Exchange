package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/statedir"
)

// newStateDir returns a new empty directory with mode 0700: t.TempDir
// applies the umask, which may leave it group-writable.
func newStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil { // #nosec G302 -- a directory needs the x bit.
		t.Fatal(err)
	}
	return dir
}

// runObied runs obied with args and returns exit code, stdout and stderr.
func runObied(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = RunDaemon(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// readKeyFile returns the content of the key file in stateDir.
func readKeyFile(t *testing.T, stateDir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, "node.key")) // #nosec G304 -- test directory.
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// assertNoPrivateKey fails if output contains the private half of the key
// file in stateDir in a common encoding.
func assertNoPrivateKey(t *testing.T, stateDir, output string) {
	t.Helper()
	data := readKeyFile(t, stateDir)
	seed := data[4:36]
	for _, secret := range []string{
		hex.EncodeToString(seed), base64.StdEncoding.EncodeToString(seed), base64.RawURLEncoding.EncodeToString(seed),
		base64.StdEncoding.EncodeToString(data), base64.RawStdEncoding.EncodeToString(data),
	} {
		if strings.Contains(output, secret) {
			t.Errorf("output reveals the private key:\n%s", output)
		}
	}
}

// identityLines parses the "Peer ID:" / "Fingerprint:" table.
func identityLines(t *testing.T, out string) admin.IdentityResponse {
	t.Helper()
	var id admin.IdentityResponse
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		key, value, _ := strings.Cut(line, ":")
		switch key {
		case "Peer ID":
			id.PeerID = strings.TrimSpace(value)
		case "Fingerprint":
			id.Fingerprint = strings.TrimSpace(value)
		default:
			t.Errorf("unexpected line %q", line)
		}
	}
	if !strings.HasPrefix(id.PeerID, "12D3KooW") || !strings.HasPrefix(id.Fingerprint, "SHA256:") {
		t.Errorf("identity output = %q", out)
	}
	return id
}

func TestKeygenAndIdentity(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")

	code, stdout, stderr := runObied(t, "keygen", "--state-dir", stateDir)
	if code != ExitOK {
		t.Fatalf("keygen: exit code = %d, stderr %q", code, stderr)
	}
	if want := "obied keygen: wrote a new node key to " + filepath.Join(stateDir, "node.key") + "\n"; stderr != want {
		t.Errorf("keygen stderr = %q, want %q", stderr, want)
	}
	generated := identityLines(t, stdout)
	assertNoPrivateKey(t, stateDir, stdout+stderr)

	code, stdout, stderr = runObied(t, "identity", "--state-dir", stateDir)
	if code != ExitOK {
		t.Fatalf("identity: exit code = %d, stderr %q", code, stderr)
	}
	if shown := identityLines(t, stdout); shown != generated {
		t.Errorf("identity shows %+v, keygen generated %+v", shown, generated)
	}
	assertNoPrivateKey(t, stateDir, stdout+stderr)

	code, stdout, _ = runObied(t, "identity", "--state-dir", stateDir, "--json")
	var fromJSON admin.IdentityResponse
	if err := json.Unmarshal([]byte(stdout), &fromJSON); code != ExitOK || err != nil || fromJSON != generated {
		t.Errorf("identity --json: exit code %d, %v, %+v", code, err, fromJSON)
	}
}

func TestKeygenRefusesOverwrite(t *testing.T) {
	stateDir := newStateDir(t)
	if code, _, stderr := runObied(t, "keygen", "--state-dir", stateDir); code != ExitOK {
		t.Fatalf("keygen: exit code = %d, stderr %q", code, stderr)
	}
	before := readKeyFile(t, stateDir)

	code, stdout, stderr := runObied(t, "keygen", "--state-dir", stateDir)
	if code != ExitFailure || stdout != "" {
		t.Fatalf("second keygen: exit code = %d, stdout %q", code, stdout)
	}
	if !strings.Contains(stderr, "already exists; pass --force to replace it") {
		t.Errorf("stderr = %q", stderr)
	}
	if !bytes.Equal(before, readKeyFile(t, stateDir)) {
		t.Error("keygen without --force changed the key file")
	}

	code, _, stderr = runObied(t, "keygen", "--force", "--state-dir", stateDir)
	if code != ExitOK {
		t.Fatalf("keygen --force: exit code = %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "a running obied keeps its old key until it is restarted") {
		t.Errorf("keygen --force stderr = %q", stderr)
	}
	if bytes.Equal(before, readKeyFile(t, stateDir)) {
		t.Error("keygen --force kept the old key")
	}
}

func TestKeygenRefusesNewerStateDir(t *testing.T) {
	stateDir := newStateDir(t)
	if err := os.WriteFile(statedir.Path(stateDir), []byte("99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runObied(t, "keygen", "--force", "--state-dir", stateDir)
	if code != ExitFailure || stdout != "" {
		t.Fatalf("keygen: exit code = %d, stdout %q", code, stdout)
	}
	if !strings.Contains(stderr, "state directory has a newer format") {
		t.Errorf("stderr = %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "node.key")); !os.IsNotExist(err) {
		t.Errorf("keygen wrote a key into a newer-format state directory: %v", err)
	}
}

func TestKeygenDoesNotStampStateDir(t *testing.T) {
	stateDir := newStateDir(t)
	if code, _, stderr := runObied(t, "keygen", "--state-dir", stateDir); code != ExitOK {
		t.Fatalf("keygen: exit code = %d, stderr %q", code, stderr)
	}
	// obied stamps it on its first start, as the user that owns the directory.
	if _, err := os.Stat(statedir.Path(stateDir)); !os.IsNotExist(err) {
		t.Errorf("keygen wrote %s: %v", statedir.FileName, err)
	}
}

func TestKeygenUsesConfiguredStateDir(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	config := writeConfig(t, "node:\n  state_dir: "+stateDir+"\n")
	if code, _, stderr := runObied(t, "keygen", "--config", config); code != ExitOK {
		t.Fatalf("keygen: exit code = %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "node.key")); err != nil {
		t.Error(err)
	}
}

func TestIdentityCommandErrors(t *testing.T) {
	insecure := newStateDir(t)
	if code, _, stderr := runObied(t, "keygen", "--state-dir", insecure); code != ExitOK {
		t.Fatalf("keygen: %q", stderr)
	}
	if err := os.Chmod(filepath.Join(insecure, "node.key"), 0o640); err != nil { // #nosec G302 -- the insecure mode under test.
		t.Fatal(err)
	}
	invalidConfig := writeConfig(t, "node:\n  state_dir: relative\n")

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr []string
	}{
		{name: "missing key", args: []string{"identity", "--state-dir", newStateDir(t)}, wantCode: ExitFailure,
			wantStderr: []string{"no such file", "create the key with obied keygen, or start obied once"}},
		{name: "insecure key", args: []string{"identity", "--state-dir", insecure}, wantCode: ExitFailure,
			wantStderr: []string{"insecure key file", "chmod 600 " + filepath.Join(insecure, "node.key")}},
		{name: "invalid config", args: []string{"identity", "--config", invalidConfig}, wantCode: ExitInvalidConfig,
			wantStderr: []string{invalidConfig, "node.state_dir"}},
		{name: "keygen invalid config", args: []string{"keygen", "--config", invalidConfig}, wantCode: ExitInvalidConfig,
			wantStderr: []string{"node.state_dir"}},
		{name: "unexpected argument", args: []string{"identity", "now"}, wantCode: ExitUsage,
			wantStderr: []string{`unexpected argument "now"`}},
		{name: "unknown flag", args: []string{"keygen", "--overwrite"}, wantCode: ExitUsage,
			wantStderr: []string{"unknown flag --overwrite", "obied keygen --help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runObied(t, tt.args...)
			if code != tt.wantCode || stdout != "" {
				t.Errorf("exit code = %d, want %d (stdout %q, stderr %q)", code, tt.wantCode, stdout, stderr)
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr = %q, want it to contain %q", stderr, want)
				}
			}
		})
	}
}

func TestIdentityWriteError(t *testing.T) {
	stateDir := newStateDir(t)
	if code, _, stderr := runObied(t, "keygen", "--state-dir", stateDir); code != ExitOK {
		t.Fatalf("keygen: %q", stderr)
	}
	var stderr bytes.Buffer
	if code := RunDaemon([]string{"identity", "--state-dir", stateDir}, failingWriter{}, &stderr); code != ExitIOError {
		t.Errorf("exit code = %d, want %d (stderr %q)", code, ExitIOError, stderr.String())
	}
}

// TestObiectlIdentityMatchesKeyFile starts obied in-process and checks that
// obiectl identity shows what obied identity reads from the key file.
func TestObiectlIdentityMatchesKeyFile(t *testing.T) {
	n := newTestNode(t, "")
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &stderr, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderr)
	})
	defer func() { cancel(); waitExit(t, exit, &stderr) }()

	var ctlOut, ctlErr bytes.Buffer
	if code := RunCtl([]string{"--socket", n.socket, "identity"}, &ctlOut, &ctlErr); code != ExitOK {
		t.Fatalf("obiectl identity: exit code = %d, stderr %q", code, ctlErr.String())
	}
	code, offline, offlineErr := runObied(t, "identity", "--config", n.config)
	if code != ExitOK {
		t.Fatalf("obied identity: exit code = %d, stderr %q", code, offlineErr)
	}
	if ctlOut.String() != offline {
		t.Errorf("obiectl identity =\n%s\nobied identity =\n%s", ctlOut.String(), offline)
	}
	identityLines(t, offline)
	assertNoPrivateKey(t, n.stateDir, ctlOut.String())

	ctlOut.Reset()
	var fromJSON admin.IdentityResponse
	if code := RunCtl([]string{"--socket", n.socket, "identity", "--json"}, &ctlOut, &ctlErr); code != ExitOK {
		t.Fatalf("obiectl identity --json: exit code = %d", code)
	}
	if err := json.Unmarshal(ctlOut.Bytes(), &fromJSON); err != nil || fromJSON != identityLines(t, offline) {
		t.Errorf("obiectl identity --json = %s (%v)", ctlOut.String(), err)
	}
}

func TestObiectlIdentityDaemonNotRunning(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "obie.sock")
	var stdout, stderr bytes.Buffer
	if code := RunCtl([]string{"--socket", socket, "identity"}, &stdout, &stderr); code != ExitFailure {
		t.Errorf("exit code = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr.String(), "obied is not running") || stdout.Len() != 0 {
		t.Errorf("stderr = %q, stdout %q", stderr.String(), stdout.String())
	}
}
