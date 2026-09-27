package decision

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// clock is a settable test clock. It starts at the current second, because
// Badger applies TTLs against the wall clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Now().UTC().Truncate(time.Second)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// recorder collects the block change stream.
type recorder struct {
	mu      sync.Mutex
	changes []Change
	ch      chan Change
}

func newRecorder() *recorder { return &recorder{ch: make(chan Change, 100)} }

func (r *recorder) record(c Change) {
	r.mu.Lock()
	r.changes = append(r.changes, c)
	r.mu.Unlock()
	r.ch <- c
}

// take returns and clears the recorded changes.
func (r *recorder) take() []Change {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.changes
	r.changes = nil
	for len(r.ch) > 0 {
		<-r.ch
	}
	return out
}

type fixture struct {
	clock  *clock
	store  *store.DB
	engine *Engine
	rec    *recorder
}

// newFixture starts an in-memory store; the engine is created with p but
// not started. Its refresh ticker is effectively disabled; tests drive the
// worker's steps directly.
func newFixture(t *testing.T, p Policy) *fixture {
	t.Helper()
	clk := newClock()
	st := store.NewMemory(discardLogger(), store.Options{Now: clk.Now, SweepInterval: time.Hour, GCInterval: time.Hour})
	if err := st.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Stop(context.Background()) })
	f := &fixture{clock: clk, store: st, rec: newRecorder()}
	f.engine = New(st, p, discardLogger(), Options{Now: clk.Now, RefreshInterval: time.Hour})
	f.engine.Subscribe(f.rec.record)
	return f
}

func (f *fixture) start(t *testing.T) {
	t.Helper()
	if err := f.engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.engine.Stop(context.Background()) })
}

// put stores a ban verdict by publisher on ind, issued now.
func (f *fixture) put(t *testing.T, ind obieproto.Indicator, publisher string, confidence float64, ttl time.Duration) *obieproto.Event {
	t.Helper()
	v := verdictOn(ind, publisher, obieproto.ActionBan, confidence, f.clock.Now(), ttl)
	f.putEvent(t, v)
	return v
}

func (f *fixture) putEvent(t *testing.T, ev *obieproto.Event) {
	t.Helper()
	if ok, err := f.store.Put(ev); err != nil || !ok {
		t.Fatalf("Put(%s) = %v, %v", ev.ID, ok, err)
	}
}

func (f *fixture) revoke(t *testing.T, v *obieproto.Event) {
	t.Helper()
	f.putEvent(t, &obieproto.Event{
		ID:        newID(),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeRevoke,
		IssuedAt:  obieproto.NewTimestamp(f.clock.Now()),
		Indicator: v.Indicator,
		Revokes:   v.ID,
		Reason:    "false_positive",
		Publisher: obieproto.Publisher{PeerID: v.Publisher.PeerID},
	})
}

// wantChanges checks the recorded changes' types and causes.
func wantChanges(t *testing.T, got []Change, want ...string) {
	t.Helper()
	gotS := make([]string, len(got))
	for i, c := range got {
		gotS[i] = string(c.Type) + "/" + c.Cause
	}
	if strings.Join(gotS, " ") != strings.Join(want, " ") {
		t.Errorf("changes = %v, want %v", gotS, want)
	}
}

func (f *fixture) decision(key string) (Decision, bool) {
	f.engine.mu.RLock()
	defer f.engine.mu.RUnlock()
	d, ok := f.engine.decisions[key]
	return d, ok
}

func TestEngineStartupBuildsDecisions(t *testing.T) {
	f := newFixture(t, testPolicy())
	blocked, watched := ipv4("203.0.113.1"), ipv4("203.0.113.2")
	f.put(t, blocked, pubA, 0.9, time.Hour)
	f.put(t, blocked, pubB, 0.9, time.Hour)
	f.put(t, watched, pubA, 1, time.Hour)
	f.start(t)

	changes := f.rec.take()
	wantChanges(t, changes, "added/startup")
	if len(changes) == 1 && (changes[0].Key != blocked.Key() || changes[0].Decision.State != StateBlock) {
		t.Errorf("change = %+v", changes[0])
	}
	all := f.engine.Decisions("")
	if len(all) != 2 || all[0].Indicator != blocked || all[1].Indicator != watched || all[1].State != StateNone {
		t.Errorf("Decisions() = %+v", all)
	}
	if got := f.engine.Decisions(StateBlock); len(got) != 1 || got[0].Publishers != nil {
		t.Errorf("Decisions(block) = %+v", got)
	}
	if got := f.engine.Detail(); got != "1 blocked of 2 indicators" {
		t.Errorf("Detail() = %q", got)
	}
	if err := f.engine.Ready(); err != nil {
		t.Errorf("Ready() = %v", err)
	}
	if err := f.engine.Start(context.Background()); err == nil {
		t.Error("second Start succeeded")
	}
}

// TestEngineStream follows one indicator through the change stream.
func TestEngineStream(t *testing.T) {
	p := testPolicy()
	p.Quorum = 2
	p.Threshold = 1.5
	f := newFixture(t, p)
	f.start(t)
	ind := ipv4("203.0.113.9")
	key := ind.Key()

	a := f.put(t, ind, pubA, 1, 2*time.Hour)
	f.engine.processDirty()
	wantChanges(t, f.rec.take()) // single peer: no block
	if d, ok := f.decision(key); !ok || d.State != StateNone || d.Contributors != 1 {
		t.Errorf("after one verdict: %+v, %v", d, ok)
	}

	f.put(t, ind, pubB, 1, time.Hour)
	f.engine.processDirty()
	changes := f.rec.take()
	wantChanges(t, changes, "added/verdict")
	if len(changes) == 1 && !changes[0].Decision.ExpiresAt.Equal(a.ExpiresAt()) {
		t.Errorf("expiry = %v, want the latest contributing %v", changes[0].Decision.ExpiresAt, a.ExpiresAt())
	}

	c := f.put(t, ind, pubC, 1, time.Hour)
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "updated/verdict")

	// Revoke removes a contribution: still 2 publishers.
	f.revoke(t, c)
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "updated/revoke")

	// Expiry removes a contribution: b expires, one publisher is left.
	f.clock.Advance(time.Hour)
	if err := f.store.Sweep(f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	changes = f.rec.take()
	wantChanges(t, changes, "removed/expiry")
	if len(changes) == 1 && (changes[0].Decision.State != StateNone || changes[0].Decision.Contributors != 1) {
		t.Errorf("removed decision = %+v", changes[0].Decision)
	}

	// Revoking the last verdict forgets the indicator.
	f.revoke(t, a)
	f.engine.processDirty()
	wantChanges(t, f.rec.take())
	if d, ok := f.decision(key); ok {
		t.Errorf("decision kept without verdicts: %+v", d)
	}
}

func TestEngineRevokeAndExpiryRemoveBlocks(t *testing.T) {
	tests := []struct {
		name   string
		remove func(t *testing.T, f *fixture, v *obieproto.Event)
		cause  string
	}{
		{"revoke", func(t *testing.T, f *fixture, v *obieproto.Event) { f.revoke(t, v) }, string(store.ReasonRevoke)},
		{"expiry", func(t *testing.T, f *fixture, _ *obieproto.Event) {
			f.clock.Advance(time.Hour)
			if err := f.store.Sweep(f.clock.Now()); err != nil {
				t.Fatal(err)
			}
		}, string(store.ReasonExpiry)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, testPolicy())
			ind := ipv4("198.51.100.4")
			v := f.put(t, ind, self, 1, time.Hour) // local autoblock
			f.start(t)
			wantChanges(t, f.rec.take(), "added/startup")

			tt.remove(t, f, v)
			f.engine.processDirty()
			wantChanges(t, f.rec.take(), "removed/"+tt.cause)
			if got := f.engine.Decisions(""); len(got) != 0 {
				t.Errorf("Decisions() = %+v", got)
			}
		})
	}
}

func TestEngineLocalAutoblockOff(t *testing.T) {
	p := testPolicy()
	p.LocalAutoblock = false
	f := newFixture(t, p)
	f.start(t)
	ind := ipv4("198.51.100.5")
	f.put(t, ind, self, 1, time.Hour)
	f.engine.processDirty()
	wantChanges(t, f.rec.take())
	f.put(t, ind, pubA, 0.8, time.Hour)
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "added/verdict")
}

// TestEngineRefreshesCappedBlocks checks that a block capped by max_ttl is
// extended while its verdicts are still active, and removed when they are
// not.
func TestEngineRefreshesCappedBlocks(t *testing.T) {
	p := testPolicy()
	p.MaxTTL = time.Hour
	f := newFixture(t, p)
	ind := ipv4("198.51.100.6")
	f.put(t, ind, self, 1, 3*time.Hour)
	f.start(t)
	changes := f.rec.take()
	wantChanges(t, changes, "added/startup")
	if len(changes) == 1 && !changes[0].Decision.ExpiresAt.Equal(f.clock.Now().Add(time.Hour)) {
		t.Errorf("expiry = %v, want capped at %v", changes[0].Decision.ExpiresAt, f.clock.Now().Add(time.Hour))
	}

	f.clock.Advance(59 * time.Minute)
	f.engine.refreshExpired()
	wantChanges(t, f.rec.take()) // not yet due

	f.clock.Advance(time.Minute)
	f.engine.refreshExpired()
	changes = f.rec.take()
	wantChanges(t, changes, "updated/refresh")
	if len(changes) == 1 && !changes[0].Decision.ExpiresAt.Equal(f.clock.Now().Add(time.Hour)) {
		t.Errorf("refreshed expiry = %v, want %v", changes[0].Decision.ExpiresAt, f.clock.Now().Add(time.Hour))
	}

	// The verdict expires at +3h; the block's last refresh ends there.
	f.clock.Advance(time.Hour)
	f.engine.refreshExpired()
	changes = f.rec.take()
	wantChanges(t, changes, "updated/refresh")
	f.clock.Advance(time.Hour)
	f.engine.refreshExpired()
	wantChanges(t, f.rec.take(), "removed/refresh")
}

func TestEngineIgnoresOverridesWithoutVerdicts(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.start(t)
	ind := ipv4("198.51.100.7")
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	wantChanges(t, f.rec.take())
	if got := f.engine.Decisions(""); len(got) != 0 {
		t.Errorf("Decisions() = %+v", got)
	}
}

func TestEngineExplain(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.start(t)
	ind := ipv4("198.51.100.8")
	f.put(t, ind, pubA, 0.9, time.Hour)
	f.putEvent(t, verdictOn(ind, pubB, obieproto.ActionWatch, 0.5, f.clock.Now(), time.Hour))

	d, err := f.engine.Explain(ind)
	if err != nil {
		t.Fatal(err)
	}
	if d.State != StateNone || len(d.Publishers) != 2 || d.Publishers[0].Name != "alpha" || !d.EvaluatedAt.Equal(f.clock.Now()) {
		t.Errorf("Explain = %+v", d)
	}
	unknown, err := f.engine.Explain(ipv4("198.51.100.99"))
	if err != nil || unknown.State != StateNone || len(unknown.Publishers) != 0 || unknown.Reason != "no active verdicts" {
		t.Errorf("Explain(unknown) = %+v, %v", unknown, err)
	}

	if err := f.store.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.engine.Explain(ind); !errors.Is(err, store.ErrClosed) {
		t.Errorf("Explain on a closed store = %v, want ErrClosed", err)
	}
}

func TestEngineReadErrorKeepsDecision(t *testing.T) {
	f := newFixture(t, testPolicy())
	ind := ipv4("198.51.100.10")
	f.put(t, ind, self, 1, time.Hour)
	f.start(t)
	f.rec.take()

	if err := f.store.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.engine.markDirty(store.Change{Key: ind.Key(), Reason: store.ReasonVerdict})
	f.engine.processDirty()
	if err := f.engine.Ready(); !errors.Is(err, store.ErrClosed) {
		t.Errorf("Ready() = %v, want ErrClosed", err)
	}
	if got := f.engine.Decisions(StateBlock); len(got) != 1 {
		t.Errorf("decision not kept: %+v", got)
	}
	wantChanges(t, f.rec.take())
}

func TestEngineStartFailsOnClosedStore(t *testing.T) {
	f := newFixture(t, testPolicy())
	if err := f.store.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.Start(context.Background()); !errors.Is(err, store.ErrClosed) {
		t.Errorf("Start = %v, want ErrClosed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.engine.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Start with canceled ctx = %v", err)
	}
}

// TestEngineWorker checks that the running worker picks up store changes on
// its own and stops cleanly.
func TestEngineWorker(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.start(t)
	f.put(t, ipv4("198.51.100.11"), self, 1, time.Hour)
	select {
	case c := <-f.rec.ch:
		if c.Type != ChangeAdded || c.Cause != string(store.ReasonVerdict) {
			t.Errorf("change = %+v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change from the worker")
	}
	if err := f.engine.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.Stop(context.Background()); err != nil {
		t.Errorf("second Stop = %v", err)
	}
	// Stopped: the store subscription is gone.
	f.rec.take()
	f.put(t, ipv4("198.51.100.12"), self, 1, time.Hour)
	f.engine.dirtyMu.Lock()
	n := len(f.engine.dirty)
	f.engine.dirtyMu.Unlock()
	if n != 0 {
		t.Errorf("stopped engine still receives store changes")
	}
}

func TestEngineUnsubscribe(t *testing.T) {
	f := newFixture(t, testPolicy())
	other := newRecorder()
	unsubscribe := f.engine.Subscribe(other.record)
	unsubscribe()
	f.put(t, ipv4("198.51.100.13"), self, 1, time.Hour)
	f.start(t)
	if got := other.take(); len(got) != 0 {
		t.Errorf("unsubscribed callback got %v", got)
	}
	wantChanges(t, f.rec.take(), "added/startup")
}
