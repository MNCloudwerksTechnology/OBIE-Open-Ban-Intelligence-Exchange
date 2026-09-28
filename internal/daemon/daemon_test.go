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
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
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
	f.rl = &reloader{running: &running, self: self, env: noHost, engine: f.engine, gate: f.gate, mesh: f.mesh, log: log,
		load: func() (*config.Config, error) {
			if f.err != nil {
				return nil, f.err
			}
			c := *f.next
			return &c, nil
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
	if f.gate.Mode() != config.ModeEnforce || f.rl.running.Node.Mode != config.ModeEnforce || len(f.mesh.trust.Publishers) != 1 {
		t.Errorf("mode %s / running %s / mesh trust %+v", f.gate.Mode(), f.rl.running.Node.Mode, f.mesh.trust)
	}
	if !strings.Contains(f.logs.String(), `"msg":"configuration changes that need a restart were not applied","keys":["log"]`) {
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
	f.rl.running.Mesh.Bootstrap = []string{"/ip4/198.18.0.21/tcp/4001/p2p/" + self}
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
			breakIt(f, t)
			if err := f.rl.reload(context.Background()); err == nil {
				t.Fatal("reload succeeded")
			}
			if f.gate.Mode() != config.ModeObserve || f.rl.running.Node.Mode != config.ModeObserve || len(f.rl.running.Allowlist.CIDRs) != 0 {
				t.Errorf("running configuration changed: mode %s, %+v", f.gate.Mode(), f.rl.running.Allowlist)
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

func TestRestartKeys(t *testing.T) {
	a, b := config.Default(), config.Default()
	if keys := restartKeys(&a, &b); len(keys) != 0 {
		t.Errorf("unchanged: %v", keys)
	}
	b.Node.Mode = config.ModeEnforce
	b.Trust.DefaultWeight = 0.5
	b.Decision.Quorum = 3
	b.Allowlist.CIDRs = []string{"198.18.0.0/24"}
	if keys := restartKeys(&a, &b); len(keys) != 0 {
		t.Errorf("reloadable changes: %v", keys)
	}
	b.Node.StateDir = "/srv/obie"
	b.Mesh.Bootstrap = []string{"/ip4/198.18.0.1/tcp/4001/p2p/" + self}
	b.Admin.Socket = "/run/x.sock"
	b.Metrics.Listen = "127.0.0.1:1"
	if got := strings.Join(restartKeys(&a, &b), ","); got != "node.state_dir,admin,mesh.bootstrap,metrics" {
		t.Errorf("restart keys = %s", got)
	}
}

func TestStoreOverrides(t *testing.T) {
	st := newStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	s := storeOverrides{store: st, now: func() time.Time { return now }}
	ind := obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "198.18.0.1", Scope: "/32"}

	o, err := s.Set(ind, admin.ActionForceBlock, 90*time.Minute, "scanner")
	if err != nil {
		t.Fatal(err)
	}
	if o.Action != admin.ActionForceBlock || o.Note != "scanner" || o.ExpiresAt == nil || !o.ExpiresAt.Equal(now.Add(90*time.Minute)) || !o.CreatedAt.Equal(now) {
		t.Errorf("Set = %+v", o)
	}
	if o, err := s.Set(ind, admin.ActionForceAllow, 0, ""); err != nil || o.ExpiresAt != nil || o.Action != admin.ActionForceAllow {
		t.Errorf("replace = %+v, %v", o, err)
	}
	if _, err := s.Set(ind, admin.ActionForceAllow, 0, strings.Repeat("x", store.MaxNoteLength+1)); !errors.Is(err, admin.ErrInvalid) {
		t.Errorf("long note = %v", err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Indicator != ind {
		t.Errorf("List = %+v, %v", list, err)
	}
	if ok, err := s.Delete(ind); !ok || err != nil {
		t.Errorf("Delete = %v, %v", ok, err)
	}
	if ok, err := s.Delete(ind); ok || err != nil {
		t.Errorf("second Delete = %v, %v", ok, err)
	}
	if err := st.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(); err == nil {
		t.Error("List on a closed store succeeded")
	}
	if _, err := s.Set(ind, admin.ActionForceAllow, 0, ""); err == nil || errors.Is(err, admin.ErrInvalid) {
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

func TestNewEnforcer(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	if enf, err := newEnforcer(config.BackendDryRun, log); err != nil || enf == nil {
		t.Errorf("dryrun = %v, %v", enf, err)
	}
	if _, err := newEnforcer(config.BackendNFTables, log); err == nil || !strings.Contains(err.Error(), `enforce.backend "nftables" is not available`) {
		t.Errorf("nftables error = %v", err)
	}
}
