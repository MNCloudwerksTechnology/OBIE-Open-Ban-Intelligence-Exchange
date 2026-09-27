package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Syntactically valid peer IDs for tests.
const (
	peerA = "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd"
	peerB = "QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N"
)

// problemsOf returns the problems of a *Error, failing the test otherwise.
func problemsOf(t *testing.T, err error) []Problem {
	t.Helper()
	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %v (%T), want *config.Error", err, err)
	}
	return cfgErr.Problems
}

// wantProblem asserts that err reports a problem at path whose message
// contains msg.
func wantProblem(t *testing.T, err error, path, msg string) Problem {
	t.Helper()
	ps := problemsOf(t, err)
	for _, p := range ps {
		if p.Path == path && strings.Contains(p.Message, msg) {
			return p
		}
	}
	t.Fatalf("no problem at %q containing %q; got %v", path, msg, ps)
	return Problem{}
}

func TestParseEmptyYieldsDefaults(t *testing.T) {
	for name, input := range map[string]string{
		"empty":         "",
		"comments only": "# nothing configured\n",
		"null sections": "node:\nadmin:\ndecision:\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Parse([]byte(input))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if want := Default(); !reflect.DeepEqual(*cfg, want) {
				t.Errorf("config = %+v, want defaults %+v", *cfg, want)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	d := Default()
	checks := []struct {
		name      string
		got, want any
	}{
		{"node.state_dir", d.Node.StateDir, "/var/lib/obie"},
		{"node.mode", d.Node.Mode, ModeObserve},
		{"node.shutdown_timeout", d.Node.ShutdownTimeout.Std(), 10 * time.Second},
		{"admin.socket", d.Admin.Socket, "/run/obie/obie.sock"},
		{"admin.socket_group", d.Admin.SocketGroup, "obie"},
		{"mesh.listen", d.Mesh.Listen, []string{"/ip4/0.0.0.0/tcp/4001", "/ip4/0.0.0.0/udp/4001/quic-v1", "/ip6/::/tcp/4001", "/ip6/::/udp/4001/quic-v1"}},
		{"mesh.bootstrap", d.Mesh.Bootstrap, []string{}},
		{"trust.publishers", d.Trust.Publishers, []Publisher{}},
		{"trust.default_weight", d.Trust.DefaultWeight, 0.0},
		{"trust.local_weight", d.Trust.LocalWeight, 1.0},
		{"decision.threshold", d.Decision.Threshold, 1.8},
		{"decision.quorum", d.Decision.Quorum, 2},
		{"decision.max_ttl", d.Decision.MaxTTL.Std(), 30 * 24 * time.Hour},
		{"decision.default_ttl", d.Decision.DefaultTTL.Std(), 7 * 24 * time.Hour},
		{"decision.local_autoblock", d.Decision.LocalAutoblock, true},
		{"allowlist.cidrs", d.Allowlist.CIDRs, []string{}},
		{"enforce.backend", d.Enforce.Backend, BackendDryRun},
		{"enforce.max_entries", d.Enforce.MaxEntries, 100000},
		{"enforce.reconcile_interval", d.Enforce.ReconcileInterval.Std(), 10 * time.Second},
		{"metrics.listen", d.Metrics.Listen, "127.0.0.1:9464"},
		{"audit.path", d.Audit.Path, ""},
		{"log.level", d.Log.Level, "info"},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("default %s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if err := d.Validate(); err != nil {
		t.Errorf("defaults do not validate: %v", err)
	}
}

func TestParseOverridesDefaults(t *testing.T) {
	input := `
node:
  mode: enforce
mesh:
  bootstrap:
    - /dns4/seed.example.org/tcp/4001/p2p/` + peerA + `
trust:
  publishers:
    - {peer_id: ` + peerA + `, name: seed, weight: 1}
    - peer_id: ` + peerB + `
      name: friend
      weight: 0.5
decision:
  quorum: 3
  default_ttl: 36h
  local_autoblock: false
enforce:
  max_entries: 0x10
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Node.Mode != ModeEnforce || cfg.Node.StateDir != "/var/lib/obie" {
		t.Errorf("node = %+v", cfg.Node)
	}
	if len(cfg.Mesh.Bootstrap) != 1 || len(cfg.Mesh.Listen) != 4 {
		t.Errorf("mesh = %+v", cfg.Mesh)
	}
	wantPubs := []Publisher{{PeerID: peerA, Name: "seed", Weight: 1}, {PeerID: peerB, Name: "friend", Weight: 0.5}}
	if !reflect.DeepEqual(cfg.Trust.Publishers, wantPubs) {
		t.Errorf("publishers = %+v, want %+v", cfg.Trust.Publishers, wantPubs)
	}
	if cfg.Decision.Quorum != 3 || cfg.Decision.DefaultTTL.Std() != 36*time.Hour || cfg.Decision.Threshold != 1.8 || cfg.Decision.LocalAutoblock {
		t.Errorf("decision = %+v", cfg.Decision)
	}
	if cfg.Enforce.MaxEntries != 16 {
		t.Errorf("enforce.max_entries = %d, want 16", cfg.Enforce.MaxEntries)
	}
}

func TestParseEmptyListReplacesDefault(t *testing.T) {
	for name, input := range map[string]string{
		"flow": "mesh:\n  listen: []\n",
		"null": "mesh:\n  listen:\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Parse([]byte(input))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cfg.Mesh.Listen == nil || len(cfg.Mesh.Listen) != 0 {
				t.Errorf("mesh.listen = %#v, want empty list", cfg.Mesh.Listen)
			}
		})
	}
}

func TestParseAliases(t *testing.T) {
	input := "trust:\n  default_weight: &w 0.25\n  local_weight: *w\n"
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Trust.LocalWeight != 0.25 {
		t.Errorf("trust.local_weight = %v, want 0.25", cfg.Trust.LocalWeight)
	}
}

func TestParseDecodeErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		path  string
		msg   string
	}{
		{"unknown top-level key", "nodes:\n  mode: observe\n", "nodes", "unknown key (valid keys: admin, allowlist,"},
		{"unknown nested key", "node:\n  sate_dir: /x\n", "node.sate_dir", "unknown key (valid keys: mode, shutdown_timeout, state_dir)"},
		{"unknown publisher key", "trust:\n  publishers:\n    - {peer_id: " + peerA + ", name: a, weight: 1, wieght: 1}\n", "trust.publishers[0].wieght", "unknown key"},
		{"duplicate key", "node:\n  mode: observe\n  mode: enforce\n", "node.mode", "duplicate key"},
		{"non-string key", "node:\n  1: x\n", "node.1", "keys must be strings, got an integer"},
		{"root not a mapping", "- a\n", "(root)", "must be a mapping, got a list"},
		{"section not a mapping", "node: observe\n", "node", "must be a mapping, got a string"},
		{"list expected", "mesh:\n  listen: /ip4/0.0.0.0/tcp/4001\n", "mesh.listen", "must be a list, got a string"},
		{"string expected got int", "admin:\n  socket_group: 1000\n", "admin.socket_group", "must be a string, got an integer"},
		{"string expected got bool", "admin:\n  socket_group: true\n", "admin.socket_group", "must be a string, got a boolean"},
		{"string expected got mapping", "log:\n  level: {a: b}\n", "log.level", "must be a string, got a mapping"},
		{"string expected got null", "log:\n  level:\n", "log.level", "must be a string, got null"},
		{"int expected got string", "decision:\n  quorum: \"2\"\n", "decision.quorum", "must be an integer, got a string"},
		{"int expected got float", "decision:\n  quorum: 2.5\n", "decision.quorum", "must be an integer, got a number"},
		{"int out of range", "enforce:\n  max_entries: 99999999999999999999\n", "enforce.max_entries", "out of range"},
		{"number expected got string", "decision:\n  threshold: high\n", "decision.threshold", "must be an integer or a number, got a string"},
		{"number expected got bool", "trust:\n  local_weight: true\n", "trust.local_weight", "must be an integer or a number, got a boolean"},
		{"bool expected got string", "decision:\n  local_autoblock: \"false\"\n", "decision.local_autoblock", "must be a boolean, got a string"},
		{"bool expected got int", "decision:\n  local_autoblock: 0\n", "decision.local_autoblock", "must be a boolean, got an integer"},
		{"list item wrong type", "allowlist:\n  cidrs: [10.0.0.0/8, 42]\n", "allowlist.cidrs[1]", "must be a string, got an integer"},
		{"duration not a string", "decision:\n  max_ttl: 30\n", "decision.max_ttl", "must be a string, got an integer"},
		{"duration syntax", "enforce:\n  reconcile_interval: ten seconds\n", "enforce.reconcile_interval", "invalid duration"},
		{"duration fractional days", "decision:\n  max_ttl: 1.5d\n", "decision.max_ttl", "whole number"},
		{"duration empty", "decision:\n  max_ttl: \"\"\n", "decision.max_ttl", "invalid duration"},
		{"publisher missing peer_id", "trust:\n  publishers:\n    - {name: a, weight: 1}\n", "trust.publishers[0].peer_id", "required key is missing"},
		{"publisher missing weight", "trust:\n  publishers:\n    - {peer_id: " + peerA + ", name: a}\n", "trust.publishers[0].weight", "required key is missing"},
		{"publisher not a mapping", "trust:\n  publishers:\n    - " + peerA + "\n", "trust.publishers[0]", "must be a mapping"},
		{"multiple documents", "node: {}\n---\nnode: {}\n", "(document)", "exactly one YAML document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.input))
			wantProblem(t, err, tt.path, tt.msg)
		})
	}
}

func TestParseYAML11BooleanIsAString(t *testing.T) {
	// YAML 1.2 (yaml.v3) reads "yes" as a string, so this is only an
	// invalid enum value, not a type error.
	_, err := Parse([]byte("node:\n  mode: yes\n"))
	wantProblem(t, err, "node.mode", `must be one of ["observe" "enforce"], got "yes"`)
}

func TestParseReportsAllProblemsWithLines(t *testing.T) {
	// Decoding and validation problems are reported together.
	input := "node:\n  mode: fast\nbogus: 1\ndecision:\n  quorum: 0\n"
	_, err := Parse([]byte(input))
	if got := len(problemsOf(t, err)); got != 3 {
		t.Errorf("got %d problems, want 3: %v", got, err)
	}
	if p := wantProblem(t, err, "bogus", "unknown key"); p.Line != 3 {
		t.Errorf("bogus line = %d, want 3", p.Line)
	}
	if p := wantProblem(t, err, "decision.quorum", "at least 1"); p.Line != 5 {
		t.Errorf("decision.quorum line = %d, want 5", p.Line)
	}
	if !strings.Contains(err.Error(), "node.mode (line 2): must be one of") {
		t.Errorf("Error() = %q, want it to name path and line", err.Error())
	}
}

func TestParseReportsEachPathOnce(t *testing.T) {
	// A value that failed to decode is not reported again by validation.
	input := "trust:\n  publishers:\n    - {name: a, weight: 1}\nallowlist:\n  cidrs: [42]\n"
	_, err := Parse([]byte(input))
	ps := problemsOf(t, err)
	if len(ps) != 2 {
		t.Fatalf("got %d problems, want 2: %v", len(ps), ps)
	}
	wantProblem(t, err, "trust.publishers[0].peer_id", "required key is missing")
	wantProblem(t, err, "allowlist.cidrs[0]", "must be a string")
}

func TestParseSyntaxError(t *testing.T) {
	_, err := Parse([]byte("node:\n  mode: [observe\n"))
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("error = %v, want a parse error", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obie.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log.level = %q, want debug", cfg.Log.Level)
	}

	if _, err := Load(filepath.Join(dir, "missing.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load(missing) error = %v, want ErrNotExist", err)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "10s", want: 10 * time.Second},
		{in: "1h30m", want: 90 * time.Minute},
		{in: "7d", want: 7 * 24 * time.Hour},
		{in: "0d", want: 0},
		{in: "-1d", want: -24 * time.Hour},
		{in: "d", wantErr: true},
		{in: "1.5d", wantErr: true},
		{in: "7 d", wantErr: true},
		{in: "7", wantErr: true},
		{in: "999999999d", wantErr: true},
		{in: "forever", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseDuration(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDuration(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if !tt.wantErr && got.Std() != tt.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.in, got.Std(), tt.want)
			}
		})
	}
}

func TestDurationString(t *testing.T) {
	for d, want := range map[Duration]string{
		Duration(30 * day):              "30d",
		Duration(36 * time.Hour):        "36h0m0s",
		Duration(10 * time.Second):      "10s",
		Duration(0):                     "0s",
		Duration(-2 * day):              "-2d",
		Duration(day + time.Nanosecond): "24h0m0.000000001s",
	} {
		if got := d.String(); got != want {
			t.Errorf("Duration(%d).String() = %q, want %q", int64(d), got, want)
		}
	}
}
