package enforce

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// fakeEnforcer records what reaches the enforcer.
type fakeEnforcer struct {
	mu      sync.Mutex
	changes []decision.Change
}

func (f *fakeEnforcer) Apply(c decision.Change) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, c)
}

func (f *fakeEnforcer) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.changes))
	for i, c := range f.changes {
		out[i] = string(c.Type) + "/" + c.Cause + "/" + c.Key
	}
	f.changes = nil
	return out
}

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func block(key string, typ decision.ChangeType, expires time.Time) decision.Change {
	return decision.Change{Type: typ, Key: key, Cause: "verdict", Decision: decision.Decision{State: decision.StateBlock, ExpiresAt: expires, Reason: "consensus"}}
}

func newGate(mode config.Mode, enf Enforcer) (*Gate, *bytes.Buffer) {
	var logs bytes.Buffer
	g := NewGate(mode, enf, slog.New(slog.NewJSONHandler(&logs, nil)))
	g.now = func() time.Time { return t0 }
	return g, &logs
}

func wantSeq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("enforcer got %v, want %v", got, want)
	}
}

// TestObserveNeverReachesEnforcer: in observe mode every change is logged,
// none is forwarded.
func TestObserveNeverReachesEnforcer(t *testing.T) {
	enf := &fakeEnforcer{}
	g, logs := newGate(config.ModeObserve, enf)
	g.Handle(block("ipv4:185.0.0.1", decision.ChangeAdded, t0.Add(time.Hour)))
	g.Handle(block("ipv4:185.0.0.1", decision.ChangeUpdated, t0.Add(2*time.Hour)))
	g.Handle(block("ipv4:185.0.0.1", decision.ChangeRemoved, time.Time{}))
	wantSeq(t, enf.take())
	// Updates are logged at debug level, the rest at info.
	if n := strings.Count(logs.String(), "observe mode: block decision not enforced"); n != 2 {
		t.Errorf("logged %d observe lines:\n%s", n, logs)
	}
	if g.Mode() != config.ModeObserve {
		t.Errorf("Mode() = %s", g.Mode())
	}
}

func TestEnforceForwards(t *testing.T) {
	enf := &fakeEnforcer{}
	g, _ := newGate(config.ModeEnforce, enf)
	g.Handle(block("ipv4:185.0.0.1", decision.ChangeAdded, t0.Add(time.Hour)))
	g.Handle(block("ipv4:185.0.0.1", decision.ChangeRemoved, time.Time{}))
	wantSeq(t, enf.take(), "added/verdict/ipv4:185.0.0.1", "removed/verdict/ipv4:185.0.0.1")

	// Without a backend nothing is applied, but it is logged.
	g, logs := newGate(config.ModeEnforce, nil)
	g.Handle(block("ipv4:185.0.0.1", decision.ChangeAdded, t0.Add(time.Hour)))
	if !strings.Contains(logs.String(), "no enforcement backend") {
		t.Errorf("logs = %s", logs)
	}
}

// TestSetMode: switching to enforce applies the current blocks, switching
// back withdraws them.
func TestSetMode(t *testing.T) {
	enf := &fakeEnforcer{}
	g, _ := newGate(config.ModeObserve, enf)
	g.Handle(block("ipv4:185.0.0.2", decision.ChangeAdded, t0.Add(time.Hour)))
	g.Handle(block("ipv4:185.0.0.1", decision.ChangeAdded, t0.Add(time.Hour)))
	g.Handle(block("ipv4:185.0.0.3", decision.ChangeAdded, t0)) // expired
	g.Handle(block("ipv4:185.0.0.4", decision.ChangeAdded, t0.Add(time.Hour)))
	g.Handle(block("ipv4:185.0.0.4", decision.ChangeRemoved, time.Time{}))
	wantSeq(t, enf.take())

	g.SetMode(config.ModeObserve) // unchanged: nothing happens
	wantSeq(t, enf.take())
	g.SetMode(config.ModeEnforce)
	wantSeq(t, enf.take(), "added/mode/ipv4:185.0.0.1", "added/mode/ipv4:185.0.0.2")
	g.SetMode(config.ModeObserve)
	got := enf.changes
	wantSeq(t, enf.take(), "removed/mode/ipv4:185.0.0.1", "removed/mode/ipv4:185.0.0.2", "removed/mode/ipv4:185.0.0.3")
	if len(got) > 0 && (got[0].Decision.State != decision.StateNone || !got[0].Decision.ExpiresAt.IsZero()) {
		t.Errorf("withdrawal = %+v", got[0])
	}
	g.Handle(block("ipv4:185.0.0.5", decision.ChangeAdded, t0.Add(time.Hour)))
	wantSeq(t, enf.take())

	// Without a backend switching is only logged.
	g, _ = newGate(config.ModeEnforce, nil)
	g.Handle(block("ipv4:185.0.0.1", decision.ChangeAdded, t0.Add(time.Hour)))
	g.SetMode(config.ModeObserve)
	g.SetMode(config.ModeEnforce)
}

// TestObserveModeWithEngine drives a real decision engine: blocks decided
// in observe mode never reach the enforcer.
func TestObserveModeWithEngine(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	st := store.NewMemory(log, store.Options{SweepInterval: time.Hour, GCInterval: time.Hour})
	if err := st.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Stop(context.Background()) })
	const self = "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd"
	cfg := config.Default()
	engine := decision.New(st, decision.NewPolicy(self, cfg.Trust, cfg.Decision), log, decision.Options{})
	enf := &fakeEnforcer{}
	g := NewGate(config.ModeObserve, enf, log)
	engine.Subscribe(g.Handle)

	ind := obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "185.0.0.9", Scope: "/32"}
	if err := st.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	if got := engine.Decisions(decision.StateBlock); len(got) != 1 {
		t.Fatalf("engine decided %+v", got)
	}
	wantSeq(t, enf.take())

	g.SetMode(config.ModeEnforce)
	wantSeq(t, enf.take(), "added/mode/ipv4:185.0.0.9")
}
