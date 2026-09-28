package enforce

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// block returns a change of the block on the IPv4 address or CIDR range
// value with score 1.
func block(value string, typ decision.ChangeType, expires time.Time) decision.Change {
	ind := obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"}
	if strings.Contains(value, "/") {
		ind = obieproto.Indicator{Kind: obieproto.KindCIDR, Value: value, Scope: value[strings.Index(value, "/"):]}
	}
	d := decision.Decision{Indicator: ind, State: decision.StateBlock, Score: 1, ExpiresAt: expires, Reason: "consensus"}
	if typ == decision.ChangeRemoved {
		d.State, d.ExpiresAt = decision.StateNone, time.Time{}
	}
	return decision.Change{Type: typ, Key: ind.Key(), Cause: "verdict", Decision: d}
}

// counter counts notifications.
type counter struct{ n atomic.Int32 }

func (c *counter) notify()     { c.n.Add(1) }
func (c *counter) take() int32 { return c.n.Swap(0) }

func newGate(mode config.Mode, notify func()) (*Gate, *bytes.Buffer) {
	var logs bytes.Buffer
	return NewGate(mode, notify, slog.New(slog.NewJSONHandler(&logs, nil))), &logs
}

func blockKeys(g *Gate) string {
	var keys []string
	for _, d := range g.Blocks() {
		keys = append(keys, d.Indicator.Key())
	}
	return strings.Join(keys, " ")
}

// TestObserveNeverNotifies: in observe mode every change is logged and
// tracked, the reconciler is never notified.
func TestObserveNeverNotifies(t *testing.T) {
	var c counter
	g, logs := newGate(config.ModeObserve, c.notify)
	g.Handle(block("185.0.0.1", decision.ChangeAdded, t0.Add(time.Hour)))
	g.Handle(block("185.0.0.1", decision.ChangeUpdated, t0.Add(2*time.Hour)))
	g.Handle(block("185.0.0.2", decision.ChangeAdded, t0.Add(time.Hour)))
	g.Handle(block("185.0.0.2", decision.ChangeRemoved, time.Time{}))
	if n := c.take(); n != 0 {
		t.Errorf("notified %d times", n)
	}
	// Updates are logged at debug level, the rest at info.
	if n := strings.Count(logs.String(), "observe mode: block decision not enforced"); n != 3 {
		t.Errorf("logged %d observe lines:\n%s", n, logs)
	}
	if got := blockKeys(g); got != "ipv4:185.0.0.1" {
		t.Errorf("blocks = %q", got)
	}
	if b := g.Blocks(); !b[0].ExpiresAt.Equal(t0.Add(2 * time.Hour)) {
		t.Errorf("block not updated: %+v", b[0])
	}
	if g.Mode() != config.ModeObserve {
		t.Errorf("Mode() = %s", g.Mode())
	}
}

func TestEnforceNotifies(t *testing.T) {
	var c counter
	g, _ := newGate(config.ModeEnforce, c.notify)
	g.Handle(block("185.0.0.1", decision.ChangeAdded, t0.Add(time.Hour)))
	g.Handle(block("185.0.0.1", decision.ChangeRemoved, time.Time{}))
	if n := c.take(); n != 2 {
		t.Errorf("notified %d times, want 2", n)
	}

	// Without a backend nothing is applied, but it is logged.
	g, logs := newGate(config.ModeEnforce, nil)
	g.Handle(block("185.0.0.1", decision.ChangeAdded, t0.Add(time.Hour)))
	if !strings.Contains(logs.String(), "no enforcement backend") {
		t.Errorf("logs = %s", logs)
	}
	g.SetMode(config.ModeObserve) // no backend: only logged
}

// TestSetMode: every mode switch notifies the reconciler, which applies or
// withdraws the tracked blocks.
func TestSetMode(t *testing.T) {
	var c counter
	g, logs := newGate(config.ModeObserve, c.notify)
	g.SetMode(config.ModeObserve) // unchanged: nothing happens
	if n := c.take(); n != 0 {
		t.Errorf("notified %d times", n)
	}
	g.SetMode(config.ModeEnforce)
	g.SetMode(config.ModeObserve)
	if n := c.take(); n != 2 {
		t.Errorf("notified %d times, want 2", n)
	}
	if !strings.Contains(logs.String(), "node mode changed") {
		t.Errorf("logs = %s", logs)
	}
}

// TestObserveModeWithEngine drives a real decision engine: the gate sees
// the blocks decided in observe mode without notifying the reconciler.
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
	var c counter
	g := NewGate(config.ModeObserve, c.notify, log)
	engine.Subscribe(g.Handle)

	ind := obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "185.0.0.9", Scope: "/32"}
	if err := st.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	if got := blockKeys(g); got != "ipv4:185.0.0.9" {
		t.Fatalf("gate blocks = %q", got)
	}
	if n := c.take(); n != 0 {
		t.Errorf("notified %d times", n)
	}
	g.SetMode(config.ModeEnforce)
	if n := c.take(); n != 1 {
		t.Errorf("notified %d times, want 1", n)
	}
}
