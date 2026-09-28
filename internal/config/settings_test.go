package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestSettingsMatchTheReference keeps the settings the console explains
// equal to the configuration: every leaf key once, in the order of the
// configuration reference, applied on reload or restart as the reference
// says (ADR 0024).
func TestSettingsMatchTheReference(t *testing.T) {
	data, err := os.ReadFile(referencePath) // #nosec G304 -- fixed repository path.
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	applied := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := referenceRow.FindStringSubmatch(line); m != nil {
			keys = append(keys, m[1])
			applied[m[1]] = m[3]
		}
	}
	var got []string
	defaults := Default()
	for _, s := range Settings() {
		got = append(got, s.Key)
		if string(s.Applied) != applied[s.Key] {
			t.Errorf("%s: applied on %q, the reference says %q", s.Key, s.Applied, applied[s.Key])
		}
		if s.Summary == "" || strings.ContainsAny(s.Summary, "\n`") || len(s.Summary) > 120 {
			t.Errorf("%s: summary %q must be one plain line of at most 120 bytes", s.Key, s.Summary)
		}
		if !strings.HasSuffix(s.Summary, ".") {
			t.Errorf("%s: summary %q must end with a full stop", s.Key, s.Summary)
		}
		if _, ok := defaults.Value(s.Key); !ok {
			t.Errorf("%s: no value", s.Key)
		}
	}
	if !slices.Equal(got, keys) {
		t.Errorf("settings = %v\nreference = %v", got, keys)
	}
	leaves := leafKeyPaths()
	if len(got) != len(leaves) {
		t.Errorf("%d settings for %d keys", len(got), len(leaves))
	}
}

// TestSecretSettingsAreMarked fails for a key whose name suggests a secret
// but whose value the console would show.
func TestSecretSettingsAreMarked(t *testing.T) {
	for _, s := range Settings() {
		name := strings.ToLower(s.Key)
		for _, word := range []string{"token", "secret", "password", "passphrase", "credential", "private"} {
			if strings.Contains(name, word) && !s.Secret {
				t.Errorf("%s looks like a secret (%q) but is not marked Secret", s.Key, word)
			}
		}
	}
}

func TestSettingSection(t *testing.T) {
	for key, want := range map[string]string{"node.mode": "node", "mesh.rate_limit.peer.burst": "mesh", "log.level": "log"} {
		if got := (Setting{Key: key}).Section(); got != want {
			t.Errorf("Section(%s) = %q, want %q", key, got, want)
		}
	}
}

func TestValueSpellsTheSetting(t *testing.T) {
	cfg := Default()
	cfg.Mesh.Bootstrap = []string{"/ip4/192.0.2.1/tcp/4001/p2p/" + peerA}
	cfg.Trust.Publishers = []Publisher{{PeerID: peerA, Name: "alpha", Weight: 0.5}, {PeerID: peerB, Name: "beta", Weight: 1}}
	cfg.Decision.MaxTTL = Duration(36 * time.Hour)
	for key, want := range map[string]Value{
		"node.mode":                         {Text: "observe"},
		"node.shutdown_timeout":             {Text: "10s"},
		"decision.max_ttl":                  {Text: "36h0m0s"},
		"decision.default_ttl":              {Text: "7d"},
		"decision.threshold":                {Text: "1.8"},
		"trust.local_weight":                {Text: "1"},
		"decision.local_autoblock":          {Text: "true"},
		"store.max_indicators":              {Text: "1000000"},
		"audit.path":                        {Text: `""`},
		"allowlist.cidrs":                   {List: true},
		"mesh.bootstrap":                    {List: true, Items: []string{"/ip4/192.0.2.1/tcp/4001/p2p/" + peerA}},
		"mesh.rate_limit.publisher.burst":   {Text: "50"},
		"enforce.nftables.teardown_on_stop": {Text: "false"},
		"trust.publishers": {List: true, Items: []string{
			"{peer_id: " + peerA + ", name: alpha, weight: 0.5}", "{peer_id: " + peerB + ", name: beta, weight: 1}"}},
	} {
		got, ok := cfg.Value(key)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("Value(%s) = %+v, %v; want %+v", key, got, ok, want)
		}
	}
	for _, key := range []string{"", "node", "mesh.rate_limit", "node.mode.x", "nope", "trust.publishers.name"} {
		if got, ok := cfg.Value(key); ok {
			t.Errorf("Value(%q) = %+v, want no setting", key, got)
		}
	}
}

func TestChangedNamesTheKeysThatDiffer(t *testing.T) {
	a, b := Default(), Default()
	if got := Changed(&a, &b); len(got) != 0 {
		t.Errorf("unchanged: %v", got)
	}
	// An empty list and a missing one are the same setting.
	b.Allowlist.CIDRs, b.Mesh.Bootstrap = nil, []string{}
	if got := Changed(&a, &b); len(got) != 0 {
		t.Errorf("nil and empty lists: %v", got)
	}
	b.Node.Mode = ModeEnforce
	b.Mesh.RateLimit.Peer.Burst = 1
	b.Trust.Publishers = []Publisher{{PeerID: peerA, Name: "alpha", Weight: 1}}
	b.Log.Level = "debug"
	want := []string{"node.mode", "mesh.rate_limit.peer.burst", "trust.publishers", "log.level"}
	if got := Changed(&a, &b); !slices.Equal(got, want) {
		t.Errorf("Changed = %v, want %v", got, want)
	}
}

func TestChangedOnRestart(t *testing.T) {
	a, b := Default(), Default()
	b.Node.Mode = ModeEnforce
	b.Decision.DefaultTTL = Duration(time.Hour)
	b.Decision.MaxTTL = Duration(2 * time.Hour)
	b.Log.Level = "debug"
	if got, want := ChangedOnRestart(&a, &b), []string{"decision.default_ttl", "log.level"}; !slices.Equal(got, want) {
		t.Errorf("ChangedOnRestart = %v, want %v", got, want)
	}
}

func TestLookup(t *testing.T) {
	if s, ok := Lookup("decision.default_ttl"); !ok || s.Applied != OnRestart || s.Section() != "decision" {
		t.Errorf("Lookup(decision.default_ttl) = %+v, %v", s, ok)
	}
	if s, ok := Lookup("decision"); ok {
		t.Errorf("Lookup(decision) = %+v, want no setting", s)
	}
}

// TestFileReload: a reload takes the values and the set marks of exactly
// the settings it applies from the new file; the others keep running as
// they were loaded (ADR 0024).
func TestFileReload(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) *File {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	running := write("old.yaml", "node:\n  mode: enforce\nlog:\n  level: debug\ntrust:\n  local_weight: 0.5\n")
	next := write("new.yaml", "decision:\n  quorum: 3\n  default_ttl: 1d\nlog:\n  level: warn\nallowlist:\n  cidrs: [198.51.100.0/24]\n")
	got := running.Reload(next)

	c := got.Config
	if c.Node.Mode != ModeObserve || c.Decision.Quorum != 3 || c.Trust.LocalWeight != 1 || !slices.Equal(c.Allowlist.CIDRs, []string{"198.51.100.0/24"}) {
		t.Errorf("reloaded settings: mode %s, quorum %d, local weight %v, cidrs %v", c.Node.Mode, c.Decision.Quorum, c.Trust.LocalWeight, c.Allowlist.CIDRs)
	}
	if c.Log.Level != "debug" || c.Decision.DefaultTTL != Default().Decision.DefaultTTL {
		t.Errorf("restart settings changed: log %s, default_ttl %s", c.Log.Level, c.Decision.DefaultTTL)
	}
	for key, want := range map[string]bool{"node.mode": false, "decision.quorum": true, "trust.local_weight": false,
		"allowlist.cidrs": true, "log.level": true, "decision.default_ttl": false} {
		if got.Sets(key) != want {
			t.Errorf("Sets(%s) = %v, want %v", key, !want, want)
		}
	}
	if got.Path != next.Path {
		t.Errorf("Path = %s, want %s", got.Path, next.Path)
	}
	if running.Config.Node.Mode != ModeEnforce || !running.Sets("node.mode") || next.Config.Log.Level != "warn" {
		t.Error("Reload changed its files")
	}
}

func TestLoadFileRecordsTheKeysItSets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "obie.yaml")
	data := "node:\n  mode: enforce\nmesh:\n  rate_limit:\n    peer:\n      burst: 300\ntrust:\n  publishers: []\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != path || f.Config.Node.Mode != ModeEnforce || f.Config.Mesh.RateLimit.Peer.Burst != 300 {
		t.Errorf("LoadFile = %+v", f)
	}
	for key, want := range map[string]bool{"node.mode": true, "mesh.rate_limit.peer.burst": true, "trust.publishers": true,
		"node.state_dir": false, "mesh.rate_limit.peer.events_per_second": false, "log.level": false} {
		if got := f.Sets(key); got != want {
			t.Errorf("Sets(%s) = %v, want %v", key, got, want)
		}
	}
	var none *File
	if none.Sets("node.mode") {
		t.Error("a nil File sets a key")
	}

	if err := os.WriteFile(path, []byte("node:\n  mode: sometimes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Error("LoadFile accepted an invalid file")
	}
	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml")); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Errorf("LoadFile of a missing file = %v", err)
	}
}
