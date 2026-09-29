package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// trustedNodes trust each other; nobody trusts the stranger.
var trustedNodes = []string{"node1", "node2", "node3"}

const stranger = "stranger"

// runInit runs sandbox-init.sh on the node directories below dir, with the
// obied of this tree, as the init container does.
func runInit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join(sandboxDir, "sandbox-init.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", append([]string{script}, args...)...) // #nosec G204 -- test script.
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+obiedDir(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func initArgs(dir string) []string {
	args := []string{"--stranger", filepath.Join(dir, stranger)}
	for _, n := range trustedNodes {
		args = append(args, filepath.Join(dir, n))
	}
	return args
}

func peerID(t *testing.T, dir, node string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, node, "peer-id")) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func loadNode(t *testing.T, dir, node string) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(dir, node, "obie.yaml"))
	if err != nil {
		t.Fatalf("%s: %v", node, err)
	}
	return cfg
}

// TestSandboxInitWritesTheStory checks the configurations of the sandbox:
// the trusted nodes bootstrap to and trust each other and nobody else, the
// stranger connects to all of them, one report never blocks and two do,
// and no node can touch a firewall.
func TestSandboxInitWritesTheStory(t *testing.T) {
	dir := t.TempDir()
	out, err := runInit(t, dir, initArgs(dir)...)
	if err != nil {
		t.Fatalf("sandbox-init: %v\n%s", err, out)
	}
	ids := map[string]string{stranger: peerID(t, dir, stranger)}
	for _, n := range trustedNodes {
		ids[n] = peerID(t, dir, n)
		if !strings.Contains(out, "sandbox-init: "+n+" is "+ids[n]+"\n") {
			t.Errorf("sandbox-init does not name %s's peer ID:\n%s", n, out)
		}
	}
	if !strings.Contains(out, "sandbox-init: stranger is "+ids[stranger]+" (trusted by nobody)\n") {
		t.Errorf("sandbox-init does not name the stranger's peer ID:\n%s", out)
	}

	for _, n := range append(slices.Clone(trustedNodes), stranger) {
		cfg := loadNode(t, dir, n)
		if cfg.Node.Mode != config.ModeEnforce || cfg.Enforce.Backend != config.BackendDryRun {
			t.Errorf("%s: mode %s with backend %s, want enforce with dryrun", n, cfg.Node.Mode, cfg.Enforce.Backend)
		}
		var want []string
		for _, other := range trustedNodes {
			if other != n {
				want = append(want, "/dns4/"+other+"/tcp/4001/p2p/"+ids[other])
			}
		}
		if !slices.Equal(cfg.Mesh.Bootstrap, want) {
			t.Errorf("%s bootstraps to %q, want %q", n, cfg.Mesh.Bootstrap, want)
		}
	}

	for _, n := range trustedNodes {
		cfg := loadNode(t, dir, n)
		var trusted []string
		for _, p := range cfg.Trust.Publishers {
			trusted = append(trusted, p.Name)
			if p.PeerID != ids[p.Name] || p.Weight != 1 {
				t.Errorf("%s trusts %s as %s with weight %v, want %s with weight 1", n, p.Name, p.PeerID, p.Weight, ids[p.Name])
			}
		}
		want := slices.DeleteFunc(slices.Clone(trustedNodes), func(o string) bool { return o == n })
		if !slices.Equal(trusted, want) {
			t.Errorf("%s trusts %q, want %q", n, trusted, want)
		}
		if cfg.Trust.DefaultWeight != 0 {
			t.Errorf("%s: trust.default_weight %v, want 0: the stranger must count nothing", n, cfg.Trust.DefaultWeight)
		}
		d := cfg.Decision
		// One report at obiectl's default confidence (0.8) must not block,
		// two must.
		if d.LocalAutoblock || d.Quorum != 2 || d.Threshold <= 0.8 || d.Threshold > 1.6 {
			t.Errorf("%s: threshold %v, quorum %d, local autoblock %v; want one report of 0.8 below and two above, quorum 2, no autoblock",
				n, d.Threshold, d.Quorum, d.LocalAutoblock)
		}
		if !cfg.Console.Enabled || cfg.Audit.Path == "" {
			t.Errorf("%s: console enabled %v, audit log %q; want the console and the audit log on", n, cfg.Console.Enabled, cfg.Audit.Path)
		}
	}
	if cfg := loadNode(t, dir, stranger); len(cfg.Trust.Publishers) != 0 || cfg.Console.Enabled {
		t.Errorf("the stranger trusts %v and has a console %v; want neither", cfg.Trust.Publishers, cfg.Console.Enabled)
	}

	// Every start rewrites the configurations and keeps the keys.
	if out, err := runInit(t, dir, initArgs(dir)...); err != nil {
		t.Fatalf("second sandbox-init: %v\n%s", err, out)
	}
	for n, id := range ids {
		if got := peerID(t, dir, n); got != id {
			t.Errorf("a second start gave %s the peer ID %s, want %s kept", n, got, id)
		}
	}
}

// TestSandboxInitNeedsTheStranger checks the usage error of sandbox-init.
func TestSandboxInitNeedsTheStranger(t *testing.T) {
	dir := t.TempDir()
	out, err := runInit(t, dir, filepath.Join(dir, "node1"), filepath.Join(dir, "node2"))
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(out, "usage: sandbox-init --stranger") {
		t.Errorf("sandbox-init without --stranger: %v\n%s\nwant exit status 2 and the usage", err, out)
	}
}
