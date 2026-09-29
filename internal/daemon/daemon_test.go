package daemon

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce/nft"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

const self = "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd"

func newStore(t *testing.T) *store.DB {
	t.Helper()
	st := store.NewMemory(slog.New(slog.DiscardHandler), store.Options{SweepInterval: time.Hour, GCInterval: time.Hour})
	if err := st.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Stop(context.Background()) })
	return st
}

// fakeMesh records SetTrust and fails it while fail is set.
type fakeMesh struct {
	trust config.Trust
	fail  bool
}

func (m *fakeMesh) SetTrust(trust config.Trust) error {
	if m.fail {
		return errors.New("trusted publisher \"x\": invalid")
	}
	m.trust = trust
	return nil
}

var noHost = sovereignty.Env{
	InterfaceAddrs: func() ([]netip.Addr, error) { return nil, nil },
	LookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, errors.New("no DNS in tests")
	},
}

type reloadFixture struct {
	st     *store.DB
	engine *decision.Engine
	gate   *enforce.Gate
	mesh   *fakeMesh
	rl     *reloader
	next   *config.Config
	err    error
	logs   *syncBuffer
}

// syncBuffer is a bytes.Buffer safe for the engine's worker to log into
// while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newReloadFixture(t *testing.T) *reloadFixture {
	t.Helper()
	f := &reloadFixture{st: newStore(t), mesh: &fakeMesh{}, logs: &syncBuffer{}}
	running := config.Default()
	log := slog.New(slog.NewJSONHandler(f.logs, nil))
	f.engine = decision.New(f.st, decision.NewPolicy(self, running.Trust, running.Decision), log, decision.Options{RefreshInterval: time.Hour})
	f.gate = enforce.NewGate(running.Node.Mode, nil, log)
	f.engine.Subscribe(f.gate.Handle)
	if err := f.engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.engine.Stop(context.Background()) })
	next := config.Default()
	f.next = &next
	f.rl = &reloader{running: &config.File{Config: &running}, self: self, env: noHost, engine: f.engine, gate: f.gate, mesh: f.mesh,
		log: log, load: func() (*config.File, error) {
			if f.err != nil {
				return nil, f.err
			}
			c := *f.next
			return &config.File{Config: &c}, nil
		}}
	return f
}

func (f *reloadFixture) put(t *testing.T, value, publisher string) {
	t.Helper()
	ev := &obieproto.Event{
		ID: "01900000-0000-7000-8000-0000000000" + value[len(value)-2:], Spec: obieproto.Spec, Type: obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(time.Now().Add(-time.Minute)),
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"},
		Protocol:  "ssh", Evidence: &obieproto.Evidence{Events: 5, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 1, TTLSeconds: 3600},
		Publisher: obieproto.Publisher{PeerID: publisher},
	}
	if ok, err := f.st.Put(ev); err != nil || !ok {
		t.Fatalf("Put = %v, %v", ok, err)
	}
}

func (f *reloadFixture) explain(t *testing.T, value string) decision.Decision {
	t.Helper()
	d, err := f.engine.Explain(obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReloadApplies(t *testing.T) {
	f := newReloadFixture(t)
	f.put(t, "198.18.0.11", self)
	if d := f.explain(t, "198.18.0.11"); d.State != decision.StateBlock {
		t.Fatalf("before reload: %+v", d)
	}

	allowFile := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(allowFile, []byte("198.18.0.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.next.Node.Mode = config.ModeEnforce
	f.next.Allowlist.Files = []string{allowFile}
	f.next.Trust.LocalWeight = 0.5
	f.next.Trust.Publishers = []config.Publisher{{PeerID: self, Name: "me", Weight: 1}}
	f.next.Log.Level = "debug"
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := f.explain(t, "198.18.0.11")
	if d.State != decision.StateAllowed || d.Sovereignty.Source != sovereignty.SourceFile || d.Publishers[0].Weight != 0.5 {
		t.Errorf("after reload: %+v", d)
	}
	if f.gate.Mode() != config.ModeEnforce || f.rl.running.Config.Node.Mode != config.ModeEnforce || len(f.mesh.trust.Publishers) != 1 {
		t.Errorf("mode %s / running %s / mesh trust %+v", f.gate.Mode(), f.rl.running.Config.Node.Mode, f.mesh.trust)
	}
	if f.rl.running.Config.Log.Level != "info" {
		t.Errorf("running log level %s after a reload", f.rl.running.Config.Log.Level)
	}
	if !strings.Contains(f.logs.String(), `"msg":"configuration changes that need a restart were not applied","keys":["log.level"]`) {
		t.Errorf("no restart warning:\n%s", f.logs)
	}
	if got := f.engine.Decisions(decision.StateBlock); len(got) != 0 {
		t.Errorf("blocks after reload: %+v", got)
	}
}

// TestReloadKeepsMeshAddresses: the mesh keeps its bootstrap peers until a
// restart, so a reload keeps protecting them rather than the new ones.
func TestReloadKeepsMeshAddresses(t *testing.T) {
	f := newReloadFixture(t)
	f.rl.running.Config.Mesh.Bootstrap = []string{"/ip4/198.18.0.21/tcp/4001/p2p/" + self}
	f.put(t, "198.18.0.21", self)
	f.put(t, "198.18.0.22", self)
	f.next.Mesh.Bootstrap = []string{"/ip4/198.18.0.22/tcp/4001/p2p/" + self}
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := f.explain(t, "198.18.0.21"); d.State != decision.StateAllowed || d.Sovereignty.Source != sovereignty.SourceBootstrap {
		t.Errorf("running bootstrap peer: %+v", d)
	}
	if d := f.explain(t, "198.18.0.22"); d.State != decision.StateBlock {
		t.Errorf("new bootstrap peer: %+v", d)
	}
	if !strings.Contains(f.logs.String(), `"keys":["mesh.bootstrap"]`) {
		t.Errorf("no restart warning:\n%s", f.logs)
	}
}

func TestReloadAppliesConsole(t *testing.T) {
	f := newReloadFixture(t)
	var applied []config.Console
	f.rl.console = func(c config.Console) { applied = append(applied, c) }
	f.next.Console = config.Console{Enabled: true, Listen: "127.0.0.1:9470"}
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != f.next.Console || f.rl.running.Config.Console != f.next.Console {
		t.Errorf("console applied %+v, running %+v; want %+v once", applied, f.rl.running.Config.Console, f.next.Console)
	}
	if strings.Contains(f.logs.String(), "need a restart") {
		t.Errorf("console changes reported as needing a restart:\n%s", f.logs)
	}
	if !strings.Contains(f.logs.String(), `"console_enabled":true`) {
		t.Errorf("reload line without the console:\n%s", f.logs)
	}
}

func TestReloadRejects(t *testing.T) {
	for name, breakIt := range map[string]func(*reloadFixture, *testing.T){
		"invalid config": func(f *reloadFixture, _ *testing.T) { f.err = errors.New("invalid configuration: decision.quorum") },
		"missing allow-list file": func(f *reloadFixture, t *testing.T) {
			f.next.Allowlist.Files = []string{filepath.Join(t.TempDir(), "missing.txt")}
		},
		"mesh rejects trust": func(f *reloadFixture, _ *testing.T) { f.mesh.fail = true },
	} {
		t.Run(name, func(t *testing.T) {
			f := newReloadFixture(t)
			f.put(t, "198.18.0.12", self)
			f.next.Node.Mode = config.ModeEnforce
			f.next.Allowlist.CIDRs = []string{"198.18.0.0/24"}
			f.next.Console.Enabled = true
			consoleApplied := false
			f.rl.console = func(config.Console) { consoleApplied = true }
			breakIt(f, t)
			if err := f.rl.reload(context.Background()); err == nil {
				t.Fatal("reload succeeded")
			}
			if consoleApplied || f.rl.running.Config.Console.Enabled {
				t.Error("a rejected reload switched the console")
			}
			running := f.rl.running.Config
			if f.gate.Mode() != config.ModeObserve || running.Node.Mode != config.ModeObserve || len(running.Allowlist.CIDRs) != 0 {
				t.Errorf("running configuration changed: mode %s, %+v", f.gate.Mode(), running.Allowlist)
			}
			if d := f.explain(t, "198.18.0.12"); d.State != decision.StateBlock {
				t.Errorf("decision changed: %+v", d)
			}
			if !strings.Contains(f.logs.String(), "configuration reload rejected; the running configuration is kept") {
				t.Errorf("no rejection logged:\n%s", f.logs)
			}
		})
	}
}

// TestReloadRecordsLoads: the console learns when the running
// configuration was loaded, why the last reload was rejected and what
// waits for a restart.
func TestReloadRecordsLoads(t *testing.T) {
	f := newReloadFixture(t)
	if got := f.rl.loads.record(); !got.LoadedAt.IsZero() {
		t.Errorf("record of no configLoads = %+v", got)
	}
	f.rl.loads.reloaded(f.rl.running, nil) // a nil configLoads records nothing
	started := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	clock := started
	atStart := f.rl.running
	f.rl.loads = newConfigLoads(atStart, started, func() time.Time { return clock })
	if got := f.rl.loads.record(); !got.LoadedAt.Equal(started) || got.Reloaded || got.Rejected != nil || got.RestartKeys != nil ||
		got.File != atStart {
		t.Errorf("at start: %+v", got)
	}

	clock = started.Add(time.Minute)
	f.err = errors.New("invalid configuration: decision.quorum")
	if err := f.rl.reload(context.Background()); err == nil {
		t.Fatal("reload succeeded")
	}
	got := f.rl.loads.record()
	if !got.LoadedAt.Equal(started) || got.Reloaded || !got.RejectedAt.Equal(clock) || !errors.Is(got.Rejected, f.err) ||
		got.File != atStart {
		t.Errorf("after a rejected reload: %+v", got)
	}

	clock = started.Add(2 * time.Minute)
	f.err = nil
	f.next.Log.Level = "debug"
	f.next.Metrics.Listen = "127.0.0.1:9999"
	f.next.Decision.Quorum = 3
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	got = f.rl.loads.record()
	if !got.LoadedAt.Equal(clock) || !got.Reloaded || !got.RejectedAt.IsZero() || got.Rejected != nil ||
		strings.Join(got.RestartKeys, ",") != "metrics.listen,log.level" {
		t.Errorf("after a reload with restart keys: %+v", got)
	}
	if got.File != f.rl.running || got.File.Config.Decision.Quorum != 3 || got.File.Config.Log.Level != "info" ||
		atStart.Config.Decision.Quorum != 2 {
		t.Errorf("running configuration after the reload: %+v", got.File.Config)
	}
	got.RestartKeys[0] = "changed"
	if f.rl.loads.record().RestartKeys[0] != "metrics.listen" {
		t.Error("record shares its restart keys")
	}

	// A rejected reload keeps what waits for a restart.
	f.mesh.fail = true
	if err := f.rl.reload(context.Background()); err == nil {
		t.Fatal("reload succeeded")
	}
	if got := f.rl.loads.record(); got.Rejected == nil || len(got.RestartKeys) != 2 {
		t.Errorf("after another rejected reload: %+v", got)
	}

	// Reverting the file clears the restart keys.
	f.mesh.fail = false
	*f.next = config.Default()
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.rl.loads.record(); got.Rejected != nil || got.RestartKeys != nil {
		t.Errorf("after reverting: %+v", got)
	}
}

// TestConsoleFactsConversion: the console gets the enforcement's and the
// reloads' numbers in its own types.
func TestConsoleFactsConversion(t *testing.T) {
	st := enforce.Status{Mode: config.ModeEnforce, Applied: 10, Blocks: 14, Covered: 1, Failures: 2, RetryIn: 4 * time.Second,
		Skipped:       map[string]int{enforce.SkipAllowlist: 1, enforce.SkipMaxEntries: 1}, // ranges
		SkippedBlocks: map[string]int{enforce.SkipAllowlist: 2, enforce.SkipMaxEntries: 1}, Err: errors.New("netlink: busy")}
	got := enforceFacts(st, config.Enforce{Backend: config.BackendNFTables, MaxEntries: 10})
	want := console.EnforceFacts{Backend: "nftables", MaxEntries: 10, Mode: "enforce", Applied: 10, Blocks: 14, Covered: 1,
		Refused: 2, Capped: 1, Failures: 2, Err: "netlink: busy", RetryIn: 4 * time.Second}
	if got != want {
		t.Errorf("enforceFacts = %+v\nwant          %+v", got, want)
	}
	if got := enforceFacts(enforce.Status{}, config.Enforce{Backend: config.BackendDryRun}); got.Err != "" || got.Mode != "" || got.Backend != "dryrun" {
		t.Errorf("enforceFacts before the first pass = %+v", got)
	}

	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	c := configFacts(loadRecord{LoadedAt: at, Reloaded: true, RejectedAt: at.Add(time.Minute), Rejected: errors.New("bad"), RestartKeys: []string{"store"}})
	if !c.LoadedAt.Equal(at) || !c.Reloaded || !c.RejectedAt.Equal(at.Add(time.Minute)) || c.Rejected != "bad" || len(c.RestartKeys) != 1 {
		t.Errorf("configFacts = %+v", c)
	}
	if c := configFacts(loadRecord{LoadedAt: at}); c.Rejected != "" || c.Reloaded {
		t.Errorf("configFacts at start = %+v", c)
	}
}

// TestReloadKeepsDefaultTTL: decision.default_ttl only takes effect on a
// restart, so a reload keeps it running and reports it (ADR 0024).
func TestReloadKeepsDefaultTTL(t *testing.T) {
	f := newReloadFixture(t)
	f.next.Decision.DefaultTTL = config.Duration(time.Hour)
	f.next.Decision.Threshold = 1
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	running := f.rl.running.Config.Decision
	if running.DefaultTTL != config.Default().Decision.DefaultTTL || running.Threshold != 1 {
		t.Errorf("running decision settings = %+v", running)
	}
	if !strings.Contains(f.logs.String(), `"keys":["decision.default_ttl"]`) {
		t.Errorf("no restart warning:\n%s", f.logs)
	}
}

func TestStoreOverrides(t *testing.T) {
	st := newStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	s := storeOverrides{store: st, now: func() time.Time { return now }}
	ind := obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "198.18.0.1", Scope: "/32"}

	o, err := s.Set(context.Background(), ind, admin.ActionForceBlock, 90*time.Minute, "scanner")
	if err != nil {
		t.Fatal(err)
	}
	if o.Action != admin.ActionForceBlock || o.Note != "scanner" || o.ExpiresAt == nil || !o.ExpiresAt.Equal(now.Add(90*time.Minute)) || !o.CreatedAt.Equal(now) {
		t.Errorf("Set = %+v", o)
	}
	if o, err := s.Set(context.Background(), ind, admin.ActionForceAllow, 0, ""); err != nil || o.ExpiresAt != nil || o.Action != admin.ActionForceAllow {
		t.Errorf("replace = %+v, %v", o, err)
	}
	if _, err := s.Set(context.Background(), ind, admin.ActionForceAllow, 0, strings.Repeat("x", store.MaxNoteLength+1)); !errors.Is(err, admin.ErrInvalid) {
		t.Errorf("long note = %v", err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Indicator != ind {
		t.Errorf("List = %+v, %v", list, err)
	}
	if ok, err := s.Delete(context.Background(), ind); !ok || err != nil {
		t.Errorf("Delete = %v, %v", ok, err)
	}
	if ok, err := s.Delete(context.Background(), ind); ok || err != nil {
		t.Errorf("second Delete = %v, %v", ok, err)
	}
	if err := st.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(); err == nil {
		t.Error("List on a closed store succeeded")
	}
	if _, err := s.Set(context.Background(), ind, admin.ActionForceAllow, 0, ""); err == nil || errors.Is(err, admin.ErrInvalid) {
		t.Errorf("Set on a closed store = %v", err)
	}
}

func TestSovereigntyResponse(t *testing.T) {
	if r := sovereigntyResponse(&decision.Decision{}); !r.Applied || r.Effect != "" || r.Note != "no allow-list entry or override applies" {
		t.Errorf("none = %+v", r)
	}
	end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("x", 3600))
	d := decision.Decision{Sovereignty: sovereignty.Ruling{Effect: sovereignty.EffectBlock, Rule: sovereignty.RuleForceBlock,
		Source: sovereignty.SourceOverride, Match: "ipv4:198.18.0.1", Note: "scanner", ExpiresAt: end, Reason: "operator force-block"}}
	r := sovereigntyResponse(&d)
	if r.Effect != "block" || r.Rule != "force_block" || r.Source != "override" || r.Match != "ipv4:198.18.0.1" ||
		r.OverrideNote != "scanner" || r.ExpiresAt == nil || r.ExpiresAt.Location() != time.UTC || r.Note != "operator force-block" {
		t.Errorf("force-block = %+v", r)
	}
}

// TestExplanationResponse: every contribution keeps its reason and
// protocol in the admin API's explanation (ADR 0022).
func TestExplanationResponse(t *testing.T) {
	d := decision.Decision{Publishers: []decision.Contribution{{PeerID: self, Action: "ban", Reason: "port_scan", Protocol: "tcp",
		Weight: 1, Confidence: 0.5, Score: 0.5, Contributes: true}}}
	r := explanationResponse(d)
	if len(r.Publishers) != 1 || r.Publishers[0].Reason != "port_scan" || r.Publishers[0].Protocol != "tcp" ||
		r.Publishers[0].Score != 0.5 || !r.Publishers[0].Contributes {
		t.Errorf("publishers = %+v", r.Publishers)
	}
}

func TestNewEnforcer(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	if enf, err := newEnforcer(config.Enforce{Backend: config.BackendDryRun}, 0, log); err != nil || enf == nil {
		t.Errorf("dryrun = %v, %v", enf, err)
	}
	if enf, err := newEnforcer(config.Enforce{Backend: config.BackendNFTables}, 0, log); err != nil {
		t.Errorf("nftables = %v, %v", enf, err)
	} else if _, ok := enf.(*nft.Backend); !ok {
		t.Errorf("nftables backend is %T", enf)
	}
	if _, err := newEnforcer(config.Enforce{Backend: "iptables"}, 0, log); err == nil || !strings.Contains(err.Error(), `enforce.backend "iptables" is not available`) {
		t.Errorf("unknown backend error = %v", err)
	}
}
