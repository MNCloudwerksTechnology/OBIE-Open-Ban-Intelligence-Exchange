package decision

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
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
	return f.engine.Decision(key)
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

// TestEngineForceBlockWithoutVerdicts: a force-block needs no verdicts; it
// blocks until its override ends, capped at max_ttl, and is kept as a
// decision only while it blocks.
func TestEngineForceBlockWithoutVerdicts(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.start(t)
	ind := ipv4("198.51.100.7")
	end := f.clock.Now().Add(2 * time.Hour)
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock, ExpiresAt: end, Note: "abuse"}); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	changes := f.rec.take()
	wantChanges(t, changes, "added/override")
	if len(changes) == 1 {
		d := changes[0].Decision
		if d.State != StateBlock || !d.ExpiresAt.Equal(end) || d.Sovereignty.Rule != sovereignty.RuleForceBlock ||
			!strings.HasPrefix(d.Reason, "operator force-block override on ipv4:198.51.100.7 until ") {
			t.Errorf("decision = %+v", d)
		}
	}
	if got := f.engine.Decisions(StateBlock); len(got) != 1 {
		t.Errorf("Decisions(block) = %+v", got)
	}

	// Without TTL the block lasts max_ttl and is refreshed while the
	// override lives.
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	changes = f.rec.take()
	wantChanges(t, changes, "updated/override")
	if len(changes) == 1 && !changes[0].Decision.ExpiresAt.Equal(f.clock.Now().Add(testPolicy().MaxTTL)) {
		t.Errorf("uncapped force-block: %+v", changes[0].Decision)
	}
	f.clock.Advance(testPolicy().MaxTTL)
	f.engine.refreshExpired()
	wantChanges(t, f.rec.take(), "updated/refresh")

	if ok, err := f.store.DeleteOverride(ind.Key()); err != nil || !ok {
		t.Fatalf("DeleteOverride = %v, %v", ok, err)
	}
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "removed/override")
	if got := f.engine.Decisions(""); len(got) != 0 {
		t.Errorf("Decisions() = %+v", got)
	}
}

// TestEngineOverrideNotHiddenByVerdict: an override change followed by a
// verdict on the same indicator before the worker runs is still applied.
func TestEngineOverrideNotHiddenByVerdict(t *testing.T) {
	f := newFixture(t, testPolicy())
	ind := ipv4("198.51.100.18")
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.put(t, ind, pubA, 0.1, time.Hour)
	// The engine is not started: deliver both notifications before the
	// worker runs; the latest cause of ind is "verdict".
	f.engine.markDirty(store.Change{Key: ind.Key(), Reason: store.ReasonOverride})
	f.engine.markDirty(store.Change{Key: ind.Key(), Reason: store.ReasonVerdict})
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "added/verdict")
	if d, ok := f.decision(ind.Key()); !ok || d.Sovereignty.Rule != sovereignty.RuleForceBlock {
		t.Errorf("decision = %+v, %v", d, ok)
	}
}

// TestEngineForceAllowExpiresOnRefresh: an expired force-allow is noticed
// by the refresh, without waiting for the store's sweep.
func TestEngineForceAllowExpiresOnRefresh(t *testing.T) {
	f := newFixture(t, testPolicy())
	ind := ipv4("198.51.100.19")
	f.put(t, ind, pubA, 1, 3*time.Hour)
	f.put(t, ind, pubB, 1, 3*time.Hour)
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceAllow, ExpiresAt: f.clock.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	wantChanges(t, f.rec.take())
	f.clock.Advance(time.Hour)
	f.engine.refreshExpired()
	wantChanges(t, f.rec.take(), "added/refresh")
}

// TestEngineForceBlockExpires: an expired force-block is removed once the
// store reports the expiry.
func TestEngineForceBlockExpires(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.start(t)
	ind := ipv4("198.51.100.17")
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock, ExpiresAt: f.clock.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "added/override")
	f.clock.Advance(time.Hour)
	if got := f.engine.Decisions(StateBlock); len(got) != 0 {
		t.Errorf("expired force-block still listed: %+v", got)
	}
	if err := f.store.Sweep(f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "removed/expiry")
}

// TestEngineAllowlist: an allow-listed indicator is "allowed" whatever its
// score, and a block that becomes allow-listed on reload is removed.
func TestEngineAllowlist(t *testing.T) {
	f := newFixture(t, testPolicy())
	allowed, other := ipv4("198.51.100.20"), ipv4("198.51.100.21")
	f.put(t, allowed, pubA, 1, time.Hour)
	f.put(t, allowed, pubB, 1, time.Hour)
	f.put(t, other, pubA, 1, time.Hour)
	f.put(t, other, pubB, 1, time.Hour)
	f.engine = New(f.store, testPolicy(), discardLogger(), Options{Now: f.clock.Now, RefreshInterval: time.Hour,
		Allowlist: sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("198.51.100.20/32"), Source: sovereignty.SourceConfig})})
	f.engine.Subscribe(f.rec.record)
	f.start(t)

	changes := f.rec.take()
	wantChanges(t, changes, "added/startup")
	if len(changes) == 1 && changes[0].Key != other.Key() {
		t.Errorf("blocked %s", changes[0].Key)
	}
	d, ok := f.decision(allowed.Key())
	if !ok || d.State != StateAllowed || !d.ExpiresAt.IsZero() || d.Score != 2 ||
		d.Reason != "allow-listed: allowlist.cidrs entry 198.51.100.20/32; verdicts: consensus: score 2 >= threshold 1.8, 2 >= quorum 2" {
		t.Errorf("allowed decision = %+v, %v", d, ok)
	}
	if got := f.engine.Decisions(StateAllowed); len(got) != 1 {
		t.Errorf("Decisions(allowed) = %+v", got)
	}

	// Reload with a range covering both: the block is removed.
	f.engine.Reload(testPolicy(), sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("198.51.100.0/24"), Source: sovereignty.SourceFile}))
	wantChanges(t, f.rec.take(), "removed/reload")
	if _, ok := f.engine.Allowlist().Match(netip.MustParsePrefix("198.51.100.21/32")); !ok {
		t.Errorf("Allowlist() = %+v, want the reloaded one", f.engine.Allowlist().Entries())
	}
	// Reload with a higher threshold and no allow-list: nothing blocks.
	p := testPolicy()
	p.Threshold = 5
	f.engine.Reload(p, nil)
	wantChanges(t, f.rec.take())
	if d, _ := f.decision(allowed.Key()); d.State != StateNone || d.Threshold != 5 {
		t.Errorf("after reload: %+v", d)
	}
	// And back: both block again.
	f.engine.Reload(testPolicy(), nil)
	wantChanges(t, f.rec.take(), "added/reload", "added/reload")
}

// TestEngineForceAllow: a force-allow on a range re-decides the kept
// indicators it overlaps; a force-block does not beat the protected
// allow-list.
func TestEngineForceAllow(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.engine = New(f.store, testPolicy(), discardLogger(), Options{Now: f.clock.Now, RefreshInterval: time.Hour,
		Allowlist: sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("198.51.100.99/32"), Source: sovereignty.SourceSelf})})
	f.engine.Subscribe(f.rec.record)
	inside, outside := ipv4("198.51.100.30"), ipv4("198.51.101.30")
	for _, ind := range []obieproto.Indicator{inside, outside} {
		f.put(t, ind, pubA, 1, time.Hour)
		f.put(t, ind, pubB, 1, time.Hour)
	}
	f.start(t)
	wantChanges(t, f.rec.take(), "added/startup", "added/startup")

	rng := obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "198.51.100.0/24", Scope: "/24"}
	if err := f.store.SetOverride(store.Override{Indicator: rng, Action: store.ForceAllow, Note: "partner"}); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	changes := f.rec.take()
	wantChanges(t, changes, "removed/override")
	if len(changes) == 1 && (changes[0].Key != inside.Key() || changes[0].Decision.State != StateAllowed) {
		t.Errorf("change = %+v", changes[0])
	}
	if d, err := f.engine.Explain(inside); err != nil || d.Sovereignty.Rule != sovereignty.RuleForceAllow || d.Sovereignty.Note != "partner" {
		t.Errorf("Explain = %+v, %v", d.Sovereignty, err)
	}

	// A force-block inside the force-allowed range is overruled while the
	// force-allow lasts, and blocks once it is gone.
	fb := ipv4("198.51.100.31")
	if err := f.store.SetOverride(store.Override{Indicator: fb, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	wantChanges(t, f.rec.take())

	if _, err := f.store.DeleteOverride(rng.Key()); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	changes = f.rec.take()
	wantChanges(t, changes, "added/override", "added/override")
	if len(changes) == 2 && (changes[0].Key != inside.Key() || changes[1].Key != fb.Key()) {
		t.Errorf("changes = %+v", changes)
	}

	// A force-block on this node's own address is overruled.
	own := ipv4("198.51.100.99")
	if err := f.store.SetOverride(store.Override{Indicator: own, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	// Explain reads the overrides from the store: it is current before the
	// worker ran.
	d, err := f.engine.Explain(own)
	if err != nil || d.State != StateAllowed || d.Sovereignty.Source != sovereignty.SourceSelf {
		t.Errorf("Explain(own) = %+v, %v", d, err)
	}
	f.engine.processDirty()
	wantChanges(t, f.rec.take())
}

// flakyOverrides fails Overrides while fail is set.
type flakyOverrides struct {
	*store.DB
	fail atomic.Bool
}

func (s *flakyOverrides) Overrides(now time.Time) ([]store.Override, error) {
	if s.fail.Load() {
		return nil, errFlaky
	}
	return s.DB.Overrides(now)
}

// TestEngineRetriesFailedOverrideReads: an override change is not lost when
// reading the overrides fails.
func TestEngineRetriesFailedOverrideReads(t *testing.T) {
	f := newFixture(t, testPolicy())
	flaky := &flakyOverrides{DB: f.store}
	f.engine = New(flaky, testPolicy(), discardLogger(), Options{Now: f.clock.Now, RefreshInterval: time.Hour})
	f.engine.Subscribe(f.rec.record)
	f.start(t)
	ind := ipv4("198.51.100.40")

	flaky.fail.Store(true)
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	wantChanges(t, f.rec.take())
	if err := f.engine.Ready(); !errors.Is(err, errFlaky) {
		t.Errorf("Ready() = %v", err)
	}
	f.engine.Reload(testPolicy(), nil) // logs and keeps the previous overrides
	wantChanges(t, f.rec.take())

	flaky.fail.Store(false)
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "added/override")
	if err := f.engine.Ready(); err != nil {
		t.Errorf("Ready() = %v", err)
	}
	if err := f.engine.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	flaky.fail.Store(true)
	if err := f.engine.Start(context.Background()); !errors.Is(err, errFlaky) {
		t.Errorf("Start = %v", err)
	}
	if _, err := f.engine.Explain(ind); !errors.Is(err, errFlaky) {
		t.Errorf("Explain = %v", err)
	}
}

// TestEngineStartupForceBlock: force-blocks stored before the start are
// decided at startup.
func TestEngineStartupForceBlock(t *testing.T) {
	f := newFixture(t, testPolicy())
	ind := ipv4("198.51.100.50")
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	wantChanges(t, f.rec.take(), "added/startup")
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

// flakyStore fails ActiveVerdicts while fail is set.
type flakyStore struct {
	*store.DB
	fail atomic.Bool
}

var errFlaky = errors.New("flaky read")

func (s *flakyStore) ActiveVerdicts(key string, now time.Time) ([]*obieproto.Event, error) {
	if s.fail.Load() {
		return nil, errFlaky
	}
	return s.DB.ActiveVerdicts(key, now)
}

// TestEngineRetriesFailedReads checks that a change whose evaluation failed
// is not lost: the decision is kept, Ready reports the failure, and the
// indicator is evaluated again once reads succeed.
func TestEngineRetriesFailedReads(t *testing.T) {
	f := newFixture(t, testPolicy())
	flaky := &flakyStore{DB: f.store}
	f.engine = New(flaky, testPolicy(), discardLogger(), Options{Now: f.clock.Now, RefreshInterval: time.Hour})
	f.engine.Subscribe(f.rec.record)
	ind := ipv4("198.51.100.10")
	v := f.put(t, ind, self, 1, time.Hour)
	f.start(t)
	wantChanges(t, f.rec.take(), "added/startup")

	flaky.fail.Store(true)
	f.revoke(t, v)
	f.engine.processDirty()
	if err := f.engine.Ready(); !errors.Is(err, errFlaky) {
		t.Errorf("Ready() = %v, want the read error", err)
	}
	if got := f.engine.Decisions(StateBlock); len(got) != 1 {
		t.Errorf("decision not kept: %+v", got)
	}
	f.engine.processDirty() // still failing: the change stays pending
	wantChanges(t, f.rec.take())

	flaky.fail.Store(false)
	f.engine.processDirty()
	wantChanges(t, f.rec.take(), "removed/revoke")
	if err := f.engine.Ready(); err != nil {
		t.Errorf("Ready() after recovery = %v", err)
	}
}

// TestEngineSubscribeSnapshot checks that a subscriber registered after
// Start receives the existing blocks, then the changes.
func TestEngineSubscribeSnapshot(t *testing.T) {
	f := newFixture(t, testPolicy())
	blocked := ipv4("198.51.100.14")
	f.put(t, blocked, self, 1, time.Hour)
	f.put(t, ipv4("198.51.100.15"), pubA, 1, time.Hour) // not blocked
	f.start(t)

	late := newRecorder()
	f.engine.Subscribe(late.record)
	changes := late.take()
	wantChanges(t, changes, "added/snapshot")
	if len(changes) == 1 && (changes[0].Key != blocked.Key() || changes[0].Decision.State != StateBlock) {
		t.Errorf("snapshot = %+v", changes[0])
	}
	f.put(t, ipv4("198.51.100.16"), self, 1, time.Hour)
	f.engine.processDirty()
	wantChanges(t, late.take(), "added/verdict")
}

// TestEngineDecisionsHidesExpiredBlocks checks that a block past its expiry
// is not listed as blocked before the refresh re-evaluates it.
func TestEngineDecisionsHidesExpiredBlocks(t *testing.T) {
	p := testPolicy()
	p.MaxTTL = time.Hour
	f := newFixture(t, p)
	f.put(t, ipv4("198.51.100.17"), self, 1, 3*time.Hour)
	f.start(t)
	f.clock.Advance(time.Hour)
	if got := f.engine.Decisions(StateBlock); len(got) != 0 {
		t.Errorf("Decisions(block) lists an expired block: %+v", got)
	}
	if got := f.engine.Decisions(""); len(got) != 1 {
		t.Errorf("Decisions() = %+v", got)
	}
	f.engine.refreshExpired()
	if got := f.engine.Decisions(StateBlock); len(got) != 1 {
		t.Errorf("Decisions(block) after refresh = %+v", got)
	}
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

// TestEngineTransitions: every state change is streamed with its previous
// state, allow-listing included; block updates are not.
func TestEngineTransitions(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.engine = New(f.store, testPolicy(), discardLogger(), Options{Now: f.clock.Now, RefreshInterval: time.Hour,
		Allowlist: sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("198.51.100.20/32"), Source: sovereignty.SourceConfig})})
	var got []string
	unsubscribe := f.engine.SubscribeTransitions(func(tr Transition) {
		got = append(got, tr.Key+":"+string(tr.From)+">"+string(tr.Decision.State)+"/"+tr.Cause)
	})
	blocked, allowed := ipv4("203.0.113.1"), ipv4("198.51.100.20")
	f.put(t, blocked, pubA, 1, time.Hour)
	f.put(t, blocked, pubB, 1, time.Hour)
	f.start(t)

	f.put(t, allowed, pubA, 1, time.Hour)
	f.engine.processDirty()
	f.put(t, blocked, pubC, 1, time.Hour) // a block update: no transition
	f.engine.processDirty()
	f.engine.Reload(testPolicy(), sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("203.0.113.0/24"), Source: sovereignty.SourceFile}))
	want := []string{
		blocked.Key() + ":none>block/startup",
		allowed.Key() + ":none>allowed/verdict",
		allowed.Key() + ":allowed>none/reload",
		blocked.Key() + ":block>allowed/reload",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("transitions = %v\nwant          %v", got, want)
	}

	unsubscribe()
	f.engine.Reload(testPolicy(), nil)
	if len(got) != len(want) {
		t.Errorf("transitions after unsubscribe: %v", got[len(want):])
	}
}

// TestEngineNoTransitionForUnkeptDecisions: a force-allow on an address
// without verdicts decides "allowed", but nothing is kept, so nothing
// transitions.
func TestEngineNoTransitionForUnkeptDecisions(t *testing.T) {
	f := newFixture(t, testPolicy())
	var got []Transition
	f.engine.SubscribeTransitions(func(tr Transition) { got = append(got, tr) })
	f.start(t)
	for range 2 {
		if err := f.store.SetOverride(store.Override{Indicator: ipv4("198.51.100.40"), Action: store.ForceAllow}); err != nil {
			t.Fatal(err)
		}
		f.engine.processDirty()
	}
	if len(got) != 0 {
		t.Errorf("transitions = %+v, want none", got)
	}
}

// TestEngineExplainWith: the decision from edited inputs, as a change
// would leave it, while the stored inputs and the kept decision stay as
// they are (ADR 0026).
func TestEngineExplainWith(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.start(t)
	ind := ipv4("198.51.100.8")
	f.put(t, ind, pubA, 1, time.Hour)
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.engine.Flush()
	if d, ok := f.decision(ind.Key()); !ok || d.State != StateBlock {
		t.Fatalf("kept decision = %+v, %v; want the force-block", d, ok)
	}

	without, err := f.engine.ExplainWith(ind, func(in *Inputs) {
		in.Overrides = slices.DeleteFunc(in.Overrides, func(o store.Override) bool { return o.Indicator.Key() == ind.Key() })
	})
	if err != nil || without.State != StateNone || without.Sovereignty.Rule != "" || without.Score != 1 {
		t.Errorf("without the override = %+v, %v; want none by the verdicts", without, err)
	}
	allowed, err := f.engine.ExplainWith(ind, func(in *Inputs) {
		in.Overrides = append(in.Overrides, store.Override{Indicator: ipv4("198.51.100.8"), Action: store.ForceAllow})
	})
	if err != nil || allowed.State != StateAllowed {
		t.Errorf("with a force-allow = %+v, %v", allowed, err)
	}
	own := verdictOn(ind, self, obieproto.ActionBan, 0.8, f.clock.Now(), time.Hour)
	reported, err := f.engine.ExplainWith(ind, func(in *Inputs) {
		in.Overrides = nil
		in.Verdicts = append(in.Verdicts, own)
	})
	if err != nil || reported.State != StateBlock || reported.Contributors != 2 {
		t.Errorf("with this node's verdict = %+v, %v; want a block of 2 publishers", reported, err)
	}

	// Nothing changed.
	if d, err := f.engine.Explain(ind); err != nil || d.State != StateBlock || len(d.Publishers) != 1 {
		t.Errorf("Explain after ExplainWith = %+v, %v", d, err)
	}
}

// TestEngineFlush: a change is evaluated by Flush before it returns, so
// the kept decision reads it at once; before Start it does nothing.
func TestEngineFlush(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.engine.Flush() // not running: no evaluation, no panic
	f.start(t)
	for i := range 20 {
		ind := ipv4(fmt.Sprintf("198.51.100.%d", 100+i))
		if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
			t.Fatal(err)
		}
		f.engine.Flush()
		if d, ok := f.decision(ind.Key()); !ok || d.State != StateBlock {
			t.Fatalf("decision on %s right after Flush = %+v, %v", ind.Key(), d, ok)
		}
	}
}
