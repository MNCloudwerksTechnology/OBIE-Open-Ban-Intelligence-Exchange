package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
)

// testClock is a clock tests move forward.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// rulesFixture is a node's configuration file, allow-list file, store and
// engine, read by consoleRules.
type rulesFixture struct {
	dir, configPath, allowPath string
	clock                      *testClock
	st                         *store.DB
	rules                      *consoleRules
}

// newRulesFixture loads a configuration file that sets node.mode and
// allowlist.cidrs and names one allow-list file and a bootstrap peer by a
// DNS name that does not resolve.
func newRulesFixture(t *testing.T) *rulesFixture {
	t.Helper()
	f := &rulesFixture{dir: t.TempDir(), clock: &testClock{t: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}}
	f.configPath, f.allowPath = filepath.Join(f.dir, "obie.yaml"), filepath.Join(f.dir, "allow.txt")
	f.write(t, f.allowPath, "# partners\n185.0.1.0/24\n185.0.2.7\n")
	f.write(t, f.configPath, f.config("enforce"))
	file, err := config.LoadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	allow, err := sovereignty.Build(context.Background(), file.Config, noHost, log)
	if err != nil {
		t.Fatal(err)
	}
	f.st = store.NewMemory(log, store.Options{SweepInterval: time.Hour, GCInterval: time.Hour, Now: f.clock.now})
	if err := f.st.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.st.Stop(context.Background()) })
	engine := decision.New(f.st, decision.NewPolicy(self, file.Config.Trust, file.Config.Decision), log,
		decision.Options{Allowlist: allow, RefreshInterval: time.Hour})
	f.rules = &consoleRules{store: f.st, engine: engine, loads: newConfigLoads(file, f.clock.now(), f.clock.now), now: f.clock.now,
		loadFile: config.LoadFile, checkFile: sovereignty.CheckFile}
	return f
}

// config is the configuration file in node mode mode.
func (f *rulesFixture) config(mode string) string {
	return "node:\n  mode: " + mode + "\n  state_dir: " + f.dir + "\nmesh:\n  listen: [/ip4/127.0.0.1/tcp/4001]\n" +
		"  bootstrap: [/dns4/peer.invalid/tcp/4001/p2p/" + self + "]\n" +
		"allowlist:\n  cidrs: [185.0.3.0/24]\n  files: [" + f.allowPath + "]\n"
}

func (f *rulesFixture) write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *rulesFixture) override(t *testing.T, cidr string, action store.Action, note string, ttl time.Duration) {
	t.Helper()
	p := netip.MustParsePrefix(cidr)
	o := store.Override{Indicator: rangeIndicator(p), Action: action, Note: note}
	if ttl > 0 {
		o.ExpiresAt = f.clock.now().Add(ttl)
	}
	if err := f.st.SetOverride(o); err != nil {
		t.Fatal(err)
	}
}

// TestConsoleRulesOverrides: the overrides in effect, a force-block beaten
// by a force-allow or a protected entry marked so; the expired ones are
// read on request (AC1).
func TestConsoleRulesOverrides(t *testing.T) {
	f := newRulesFixture(t)
	f.override(t, "85.10.20.0/24", store.ForceAllow, "partner", 0)
	f.override(t, "85.10.20.7/32", store.ForceBlock, "beaten by the force-allow", 0)
	f.override(t, "127.0.0.1/32", store.ForceBlock, "beaten by the loopback range", 0)
	f.override(t, "185.0.3.9/32", store.ForceBlock, "beats allowlist.cidrs", time.Hour)
	f.override(t, "85.10.30.1/32", store.ForceBlock, "expires", time.Minute)

	got, err := f.rules.Overrides(false)
	if err != nil {
		t.Fatal(err)
	}
	byNote := map[string]console.Override{}
	for _, o := range got {
		byNote[o.Note] = o
	}
	if len(got) != 5 {
		t.Fatalf("overrides = %+v", got)
	}
	if o := byNote["partner"]; o.Range != netip.MustParsePrefix("85.10.20.0/24") || o.Action != "force_allow" || o.Overruled != nil ||
		!o.ExpiresAt.IsZero() || !o.CreatedAt.Equal(f.clock.now()) {
		t.Errorf("force-allow = %+v", o)
	}
	if o := byNote["beaten by the force-allow"]; o.Overruled == nil || o.Overruled.Rule != "force_allow" || o.Overruled.Match != "cidr:85.10.20.0/24" {
		t.Errorf("force-block under a force-allow = %+v", o)
	}
	if o := byNote["beaten by the loopback range"]; o.Overruled == nil || o.Overruled.Rule != "allowlist" || !o.Overruled.Protected ||
		o.Overruled.Match != "127.0.0.0/8" {
		t.Errorf("force-block on a protected address = %+v", o)
	}
	if o := byNote["beats allowlist.cidrs"]; o.Overruled != nil || !o.ExpiresAt.Equal(f.clock.now().Add(time.Hour)) {
		t.Errorf("force-block over an operator entry = %+v", o)
	}
	if expired, err := f.rules.Overrides(true); err != nil || len(expired) != 0 {
		t.Errorf("expired before any expiry = %+v, %v", expired, err)
	}

	// The override that expired is kept, swept or not.
	f.clock.add(2 * time.Minute)
	expired, err := f.rules.Overrides(true)
	if err != nil || len(expired) != 1 || expired[0].Note != "expires" || expired[0].Overruled != nil {
		t.Fatalf("expired = %+v, %v", expired, err)
	}
	if err := f.st.Sweep(f.clock.now()); err != nil {
		t.Fatal(err)
	}
	if expired, err := f.rules.Overrides(true); err != nil || len(expired) != 1 || expired[0].Range != netip.MustParsePrefix("85.10.30.1/32") {
		t.Errorf("expired after the sweep = %+v, %v", expired, err)
	}
	if active, _ := f.rules.Overrides(false); len(active) != 4 {
		t.Errorf("%d overrides in effect after one expired", len(active))
	}
	if f.rules.OverrideRetention() != store.OverrideRetention {
		t.Error("retention differs from the store's")
	}
}

// TestConsoleRulesAllowlist: the running allow-list by origin, what was
// loaded from its file and what the file holds now: changed, with
// rejected lines, missing (AC2, edge case 3).
func TestConsoleRulesAllowlist(t *testing.T) {
	f := newRulesFixture(t)
	a := f.rules.Allowlist()
	if !a.LoadedAt.Equal(f.clock.now()) {
		t.Errorf("loaded at %s", a.LoadedAt)
	}
	sources := map[string][]string{}
	for _, e := range a.Entries {
		sources[e.Source] = append(sources[e.Source], e.Range.String()+" "+e.Label)
	}
	if !slices.Contains(sources["builtin"], "127.0.0.0/8 loopback") || len(sources["builtin"]) != len(sovereignty.Builtin()) {
		t.Errorf("built-in entries = %v", sources["builtin"])
	}
	if !slices.Equal(sources["self"], []string{"127.0.0.1/32 /ip4/127.0.0.1/tcp/4001"}) ||
		!slices.Equal(sources["config"], []string{"185.0.3.0/24 "}) ||
		!slices.Equal(sources["file"], []string{"185.0.1.0/24 " + f.allowPath + ":2", "185.0.2.7/32 " + f.allowPath + ":3"}) {
		t.Errorf("entries = %v", sources)
	}
	if len(a.Files) != 1 || !reflect.DeepEqual(a.Files[0], console.AllowFile{Path: f.allowPath, Loaded: 2, Entries: 2}) {
		t.Errorf("files = %+v", a.Files)
	}
	if len(a.Warnings) != 1 || a.Warnings[0].Source != "bootstrap" || !strings.Contains(a.Warnings[0].Subject, "peer.invalid") ||
		a.Warnings[0].Err == "" {
		t.Errorf("warnings = %+v", a.Warnings)
	}

	f.write(t, f.allowPath, "185.0.1.0/24\n185.0.2.7\n185.0.4.0/24\n")
	if got := f.rules.Allowlist().Files[0]; !got.Changed || got.Entries != 3 || got.Loaded != 2 || got.Err != "" {
		t.Errorf("changed file = %+v", got)
	}
	f.write(t, f.allowPath, "185.0.1.0/24\n185.0.2.0/33\nnope\n")
	got := f.rules.Allowlist().Files[0]
	if got.RejectedLines != 2 || len(got.Rejected) != 2 || got.Rejected[0] != (console.RejectedLine{Line: 2, Text: "185.0.2.0/33",
		Err: `invalid CIDR "185.0.2.0/33": want e.g. 192.0.2.0/24 or 2001:db8::/32`}) {
		t.Errorf("file with rejected lines = %+v", got)
	}
	if err := os.Remove(f.allowPath); err != nil {
		t.Fatal(err)
	}
	if got := f.rules.Allowlist().Files[0]; !strings.Contains(got.Err, "no such file") || got.Loaded != 2 {
		t.Errorf("missing file = %+v", got)
	}
	// The entries loaded stay in effect.
	if pr, err := f.rules.Protection(netip.MustParsePrefix("185.0.2.7/32")); err != nil || pr.Ruling.Source != "file" {
		t.Errorf("entry of the missing file = %+v, %v", pr, err)
	}
}

// TestConsoleRulesProtection: the lookup judges like the engine and names
// the deciding rule, for any range (AC5).
func TestConsoleRulesProtection(t *testing.T) {
	f := newRulesFixture(t)
	f.override(t, "85.10.20.0/24", store.ForceAllow, "partner", 0)
	f.override(t, "185.0.3.9/32", store.ForceBlock, "", 0)
	for _, tc := range []struct {
		cidr, rule, source, match string
		protected, decidable      bool
		overlapping               int
	}{
		{"127.0.0.1/32", "allowlist", "builtin", "127.0.0.0/8", true, true, 2}, // loopback and this node's address
		{"10.0.0.0/8", "allowlist", "builtin", "10.0.0.0/8", true, false, 1},
		{"185.0.1.9/32", "allowlist", "file", "185.0.1.0/24", false, true, 1},
		{"185.0.3.9/32", "force_block", "override", "ipv4:185.0.3.9", false, true, 1},
		{"85.10.20.7/32", "force_allow", "override", "cidr:85.10.20.0/24", false, true, 0},
		{"85.10.30.1/32", "", "", "", false, true, 0},
	} {
		pr, err := f.rules.Protection(netip.MustParsePrefix(tc.cidr))
		if err != nil {
			t.Fatalf("%s: %v", tc.cidr, err)
		}
		r := pr.Ruling
		if r.Rule != tc.rule || r.Source != tc.source || r.Match != tc.match || r.Protected != tc.protected ||
			pr.Decidable != tc.decidable || len(pr.Overlapping) != tc.overlapping || pr.Range != netip.MustParsePrefix(tc.cidr) {
			t.Errorf("%s: %+v", tc.cidr, pr)
		}
	}
	if err := f.st.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.rules.Protection(netip.MustParsePrefix("85.10.30.1/32")); err == nil {
		t.Error("the lookup answered without the overrides")
	}
}

// TestConsoleRulesConfiguration: every setting with its running value and
// whether the file set it; the file on disk compared key by key, and a
// file that cannot be loaded (AC3, AC4, edge case 1).
func TestConsoleRulesConfiguration(t *testing.T) {
	f := newRulesFixture(t)
	c := f.rules.Configuration()
	if c.Path != f.configPath || c.DiskErr != "" || !c.Load.LoadedAt.Equal(f.clock.now()) || len(c.Settings) != len(config.Settings()) {
		t.Fatalf("configuration = %+v", c)
	}
	settings := map[string]console.Setting{}
	for _, s := range c.Settings {
		settings[s.Key] = s
		if s.Disk != nil {
			t.Errorf("%s changed on disk: %+v", s.Key, s.Disk)
		}
	}
	if s := settings["node.mode"]; s.Value.Text != "enforce" || s.Default || s.Applied != "reload" || s.Section != "node" || s.Summary == "" {
		t.Errorf("node.mode = %+v", s)
	}
	if s := settings["log.level"]; s.Value.Text != "info" || !s.Default || s.Applied != "restart" {
		t.Errorf("log.level = %+v", s)
	}
	if s := settings["allowlist.cidrs"]; !s.Value.List || !slices.Equal(s.Value.Items, []string{"185.0.3.0/24"}) || s.Default {
		t.Errorf("allowlist.cidrs = %+v", s)
	}

	// The file changes on disk but is not reloaded.
	f.write(t, f.configPath, f.config("observe")+"log:\n  level: debug\n")
	c = f.rules.Configuration()
	var changed []string
	for _, s := range c.Settings {
		if s.Disk != nil {
			changed = append(changed, s.Key+"="+s.Disk.Text)
		}
	}
	if !slices.Equal(changed, []string{"node.mode=observe", "log.level=debug"}) {
		t.Errorf("changed on disk = %v", changed)
	}
	if s := c.Settings[1]; s.Key != "node.mode" || s.Value.Text != "enforce" {
		t.Errorf("the running value changed: %+v", s)
	}

	// A file that cannot be loaded: a reload would be rejected.
	f.write(t, f.configPath, "decision:\n  quorum: 0\n")
	if c = f.rules.Configuration(); !strings.Contains(c.DiskErr, "decision.quorum") || c.Settings[1].Disk != nil {
		t.Errorf("invalid file: %q, %+v", c.DiskErr, c.Settings[1])
	}
	if err := os.Remove(f.configPath); err != nil {
		t.Fatal(err)
	}
	if c = f.rules.Configuration(); !strings.Contains(c.DiskErr, "no such file") {
		t.Errorf("missing file: %q", c.DiskErr)
	}

	// A rejected reload is shown with the configuration still running.
	f.rules.loads.rejected(errors.New("invalid configuration: decision.quorum"))
	if c = f.rules.Configuration(); c.Load.Rejected != "invalid configuration: decision.quorum" || c.Settings[1].Value.Text != "enforce" {
		t.Errorf("after a rejected reload: %+v", c.Load)
	}

	var none consoleRules
	if c := none.Configuration(); c.Path != "" || c.Settings != nil {
		t.Errorf("without a load record: %+v", c)
	}
	defaults := config.Default()
	f.rules.loads = newConfigLoads(&config.File{Config: &defaults}, f.clock.now(), f.clock.now)
	if c := f.rules.Configuration(); c.Path != "" || c.DiskErr != "" || !c.Settings[0].Default {
		t.Errorf("without a file: %+v", c)
	}
}

// TestSettingValueRedactsSecrets: a secret's value never reaches the
// console, only whether it is set (AC3).
func TestSettingValueRedactsSecrets(t *testing.T) {
	secret := config.Setting{Key: "x.token", Secret: true}
	for _, tc := range []struct {
		v     config.Value
		isSet bool
	}{
		{config.Value{Text: "hunter2"}, true},
		{config.Value{Text: `""`}, false},
		{config.Value{List: true, Items: []string{"hunter2"}}, true},
		{config.Value{List: true}, false},
	} {
		got := settingValue(secret, tc.v)
		if !reflect.DeepEqual(got, console.SettingValue{Secret: true, IsSet: tc.isSet}) {
			t.Errorf("settingValue(%+v) = %+v", tc.v, got)
		}
	}
	if got := settingValue(config.Setting{Key: "log.level"}, config.Value{Text: "info"}); got.Text != "info" || got.Secret {
		t.Errorf("plain value = %+v", got)
	}
}

func TestRangeIndicator(t *testing.T) {
	for cidr, want := range map[string]string{
		"203.0.113.7/32": "ipv4:203.0.113.7", "2001:db8::1/128": "ipv6:2001:db8::1",
		"198.51.100.0/24": "cidr:198.51.100.0/24", "10.1.2.3/8": "cidr:10.0.0.0/8",
	} {
		if got := rangeIndicator(netip.MustParsePrefix(cidr)).Key(); got != want {
			t.Errorf("rangeIndicator(%s) = %s, want %s", cidr, got, want)
		}
	}
	if _, err := indicatorOfRange(netip.MustParsePrefix("10.0.0.0/8")); err == nil {
		t.Error("indicatorOfRange accepted a /8")
	}
}
