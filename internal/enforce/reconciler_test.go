package enforce

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// fakeEnforcer is an in-memory backend that records its calls and fails
// on demand. Like the kernel it lists no expired entries, by the clock
// now if set.
type fakeEnforcer struct {
	now       func() time.Time
	mu        sync.Mutex
	entries   map[netip.Prefix]time.Time
	calls     []string
	applies   [][2][]Entry
	failApply int // the next failApply applies fail
	// failTeardown makes the next teardowns fail.
	failTeardown int
}

func newFake(entries ...Entry) *fakeEnforcer {
	f := &fakeEnforcer{entries: map[netip.Prefix]time.Time{}}
	for _, e := range entries {
		f.entries[e.Prefix] = e.Expires
	}
	return f
}

func (f *fakeEnforcer) record(call string) {
	f.calls = append(f.calls, call)
}

func (f *fakeEnforcer) Setup(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("setup")
	return nil
}

func (f *fakeEnforcer) List(context.Context) ([]Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("list")
	return f.list(), nil
}

func (f *fakeEnforcer) list() []Entry {
	out := make([]Entry, 0, len(f.entries))
	for p, exp := range f.entries {
		if f.now == nil || f.now().Before(exp) {
			out = append(out, Entry{Prefix: p, Expires: exp})
		}
	}
	sortEntries(out)
	return out
}

func (f *fakeEnforcer) Apply(_ context.Context, add, remove []Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("apply")
	if f.failApply > 0 {
		f.failApply--
		return errors.New("netlink: operation not permitted")
	}
	f.applies = append(f.applies, [2][]Entry{add, remove})
	for _, e := range remove {
		delete(f.entries, e.Prefix)
	}
	for _, e := range add {
		f.entries[e.Prefix] = e.Expires
	}
	return nil
}

func (f *fakeEnforcer) Teardown(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("teardown")
	if f.failTeardown > 0 {
		f.failTeardown--
		return errors.New("netlink: operation not permitted")
	}
	clear(f.entries)
	return nil
}

// state returns the applied entries as "prefix@expiry-offset" strings.
func (f *fakeEnforcer) state() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return entriesString(f.list())
}

func (f *fakeEnforcer) callsString() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, " ")
}

func (f *fakeEnforcer) count(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

func entriesString(entries []Entry) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.Prefix.String() + "@" + e.Expires.Sub(t0).String()
	}
	return strings.Join(parts, " ")
}

type fixture struct {
	gate *Gate
	enf  *fakeEnforcer
	rec  *Reconciler
	logs *syncBuffer
	now  time.Time
}

// syncBuffer is a bytes.Buffer safe for the reconciler goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newFixture(t *testing.T, mode config.Mode, enf *fakeEnforcer, opts Options) *fixture {
	t.Helper()
	f := &fixture{enf: enf, logs: &syncBuffer{}, now: t0}
	enf.now = func() time.Time { return f.now }
	log := slog.New(slog.NewJSONHandler(f.logs, nil))
	var rec *Reconciler
	f.gate = NewGate(mode, func() { rec.Trigger() }, log)
	if opts.MaxEntries == 0 {
		opts.MaxEntries = 100
	}
	if opts.Interval == 0 {
		opts.Interval = time.Hour
	}
	opts.Backend = "fake"
	opts.Now = func() time.Time { return f.now }
	rec = NewReconciler(f.gate, enf, opts, log)
	f.rec = rec
	return f
}

func (f *fixture) reconcile(t *testing.T) {
	t.Helper()
	if err := f.rec.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) add(value string, expires time.Duration, score float64) {
	c := block(value, decision.ChangeAdded, t0.Add(expires))
	c.Decision.Score = score
	f.gate.Handle(c)
}

func wantState(t *testing.T, enf *fakeEnforcer, want string) {
	t.Helper()
	if got := enf.state(); got != want {
		t.Errorf("applied = %q\nwant      %q", got, want)
	}
}

// TestCoveredPrefixesAreLeftOut: an interval set cannot hold overlapping
// ranges, so a prefix inside a wider applied one is left out until the
// wider one ends.
func TestCoveredPrefixesAreLeftOut(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(), Options{})
	f.add("198.51.100.7", 3*time.Hour, 1)
	f.add("198.51.100.0/24", time.Hour, 1)
	f.add("198.51.100.0/25", 2*time.Hour, 1)
	f.add("198.51.101.1", time.Hour, 1)
	f.add("2001:db8::1", time.Hour, 1)
	f.add("2001:db8::/32", time.Hour, 1)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.0/24@1h0m0s 198.51.101.1/32@1h0m0s 2001:db8::/32@1h0m0s")
	if got := f.rec.Detail(); got != "enforcing via fake: 3 entries" {
		t.Errorf("Detail = %q", got)
	}
	wantCounts(t, f.rec, 6, 3, 3, 0, 0)

	// When the /24 ends, the /25 and the address outside it come back.
	f.gate.Handle(block("198.51.100.0/24", decision.ChangeRemoved, time.Time{}))
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.0/25@2h0m0s 198.51.101.1/32@1h0m0s 2001:db8::/32@1h0m0s")
}

// TestCoveredPrefixesAndTheCap: covered blocks take no slot of
// enforce.max_entries, and the wider block weighs as much as the blocks
// it covers.
func TestCoveredPrefixesAndTheCap(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(), Options{MaxEntries: 2})
	f.add("198.51.100.0/24", time.Hour, 9)
	f.add("198.51.100.7", time.Hour, 8)
	f.add("192.0.2.1", time.Hour, 1)
	f.reconcile(t)
	wantState(t, f.enf, "192.0.2.1/32@1h0m0s 198.51.100.0/24@1h0m0s")
	if got := f.rec.Detail(); got != "enforcing via fake: 2 entries" {
		t.Errorf("Detail = %q", got)
	}
	wantCounts(t, f.rec, 3, 2, 1, 0, 0)

	g := newFixture(t, config.ModeEnforce, newFake(), Options{MaxEntries: 1})
	g.add("198.51.100.0/24", time.Hour, 1)
	g.add("198.51.100.7", time.Hour, 9)
	g.add("192.0.2.1", time.Hour, 5)
	g.reconcile(t)
	wantState(t, g.enf, "198.51.100.0/24@1h0m0s")
	wantCounts(t, g.rec, 3, 1, 1, 0, 1)

	// A block inside a wider range left out over the cap is left out with
	// it, not counted as sharing an applied entry.
	h := newFixture(t, config.ModeEnforce, newFake(), Options{MaxEntries: 1})
	h.add("198.51.100.0/24", time.Hour, 1)
	h.add("198.51.100.7", time.Hour, 1)
	h.add("192.0.2.1", time.Hour, 5)
	h.reconcile(t)
	wantState(t, h.enf, "192.0.2.1/32@1h0m0s")
	wantCounts(t, h.rec, 3, 1, 0, 0, 2)
	if s := h.rec.Status(); s.Skipped[SkipMaxEntries] != 1 {
		t.Errorf("Skipped = %v, want the one range", s.Skipped)
	}
}

// wantCounts checks how the last pass accounted for the decided blocks.
func wantCounts(t *testing.T, rec *Reconciler, blocks, applied, covered, refused, capped int) {
	t.Helper()
	s := rec.Status()
	if s.Blocks != blocks || s.Applied != applied || s.Covered != covered ||
		s.SkippedBlocks[SkipAllowlist] != refused || s.SkippedBlocks[SkipMaxEntries] != capped {
		t.Errorf("Status = %+v, want blocks=%d applied=%d covered=%d refused=%d capped=%d",
			s, blocks, applied, covered, refused, capped)
	}
	if s.Blocks != s.Applied+s.Covered+s.SkippedBlocks[SkipAllowlist]+s.SkippedBlocks[SkipMaxEntries] {
		t.Errorf("Status = %+v: the blocks do not add up", s)
	}
}

// TestDeferNearExpiry: an entry about to expire is left to expire, not
// removed, and an addition overlapping it waits until it is gone.
func TestDeferNearExpiry(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(), Options{})
	f.add("198.51.100.0/24", time.Hour, 1)
	f.add("198.51.100.0/25", 2*time.Hour, 1)
	f.add("192.0.2.1", time.Hour, 1)
	f.reconcile(t)
	wantState(t, f.enf, "192.0.2.1/32@1h0m0s 198.51.100.0/24@1h0m0s")

	f.now = t0.Add(time.Hour - 800*time.Millisecond)
	f.gate.Handle(block("192.0.2.1", decision.ChangeRemoved, time.Time{}))
	f.reconcile(t)
	wantState(t, f.enf, "192.0.2.1/32@1h0m0s 198.51.100.0/24@1h0m0s") // neither removed nor the /25 added
	if !f.rec.deferredAdditions() {
		t.Error("the /25 is not reported deferred")
	}

	// Renewing the dying 192.0.2.1 re-adds it at once, without a removal.
	f.add("192.0.2.1", 3*time.Hour, 1)
	f.reconcile(t)
	wantState(t, f.enf, "192.0.2.1/32@3h0m0s 198.51.100.0/24@1h0m0s")
	f.gate.Handle(block("192.0.2.1", decision.ChangeRemoved, time.Time{}))

	f.now = t0.Add(time.Hour)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.0/25@2h0m0s")
	if f.rec.deferredAdditions() {
		t.Error("still deferring")
	}
}

func TestSettle(t *testing.T) {
	e := func(p string, left time.Duration) Entry {
		return Entry{Prefix: netip.MustParsePrefix(p), Expires: t0.Add(left)}
	}
	have := []Entry{e("192.0.2.1/32", time.Second), e("198.51.100.0/24", 3*time.Second), e("203.0.113.0/24", time.Hour),
		e("2001:db8::/32", time.Hour)}
	add := []Entry{
		e("192.0.2.1/32", time.Hour),      // renews a dying entry: added, not removed
		e("198.51.100.128/25", time.Hour), // inside a dying entry: deferred
		e("203.0.113.0/25", time.Hour),    // inside a replaced entry: added
		e("2001:db8:1::/48", time.Hour),   // inside a staying entry: deferred
		e("2001::/16", time.Hour),         // around a staying entry: deferred
		e("198.51.101.0/24", time.Hour),   // adjacent: added
	}
	remove := []Entry{have[0], have[1], have[2]}
	gotAdd, gotRemove, deferred := settle(add, remove, have, t0)
	if got := entriesString(gotAdd); got != "192.0.2.1/32@1h0m0s 203.0.113.0/25@1h0m0s 198.51.101.0/24@1h0m0s" {
		t.Errorf("add = %s", got)
	}
	if got := entriesString(gotRemove); got != "203.0.113.0/24@1h0m0s" {
		t.Errorf("remove = %s", got)
	}
	if got := fmt.Sprint(deferred); got != "[198.51.100.128/25 2001::/16 2001:db8:1::/48]" {
		t.Errorf("deferred = %s", got)
	}
}

func TestConvergeFromEmpty(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(), Options{})
	f.add("192.0.2.7", time.Hour, 1)
	f.add("198.51.100.0/24", 2*time.Hour, 1)
	f.reconcile(t)
	wantState(t, f.enf, "192.0.2.7/32@1h0m0s 198.51.100.0/24@2h0m0s")
	if got := f.enf.callsString(); got != "setup list apply" {
		t.Errorf("calls = %q", got)
	}
	// Idempotent: a second pass changes nothing.
	f.reconcile(t)
	if got := f.enf.callsString(); got != "setup list apply list" {
		t.Errorf("calls = %q", got)
	}
	if got := f.rec.Detail(); got != "enforcing via fake: 2 entries" {
		t.Errorf("Detail = %q", got)
	}
	if f.rec.Ready() != nil {
		t.Errorf("Ready = %v", f.rec.Ready())
	}
}

// TestConvergeFromDrift: entries that are not decided are removed, missing
// ones added, and one with a drifted expiry replaced; one within the
// tolerance is kept.
func TestConvergeFromDrift(t *testing.T) {
	enf := newFake(
		entry("192.0.2.99/32", t0.Add(time.Hour)),                   // extra
		entry("198.51.100.1/32", t0.Add(time.Hour)),                 // drifted
		entry("198.51.100.2/32", t0.Add(time.Hour+ExpiryTolerance)), // within tolerance
	)
	f := newFixture(t, config.ModeEnforce, enf, Options{})
	f.add("198.51.100.1", 3*time.Hour, 1)
	f.add("198.51.100.2", time.Hour, 1)
	f.add("198.51.100.3", time.Hour, 1) // missing
	f.reconcile(t)
	wantState(t, enf, "198.51.100.1/32@3h0m0s 198.51.100.2/32@1h0m5s 198.51.100.3/32@1h0m0s")
	if len(enf.applies) != 1 {
		t.Fatalf("applies = %+v", enf.applies)
	}
	add, remove := enf.applies[0][0], enf.applies[0][1]
	if got := entriesString(add); got != "198.51.100.1/32@3h0m0s 198.51.100.3/32@1h0m0s" {
		t.Errorf("added %q", got)
	}
	if got := entriesString(remove); got != "192.0.2.99/32@1h0m0s 198.51.100.1/32@1h0m0s" {
		t.Errorf("removed %q", got)
	}
}

// TestExpiryAndRemoval: a block that reached its expiry and a removed block
// are withdrawn.
func TestExpiryAndRemoval(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(), Options{})
	f.add("198.51.100.1", time.Minute, 1)
	f.add("198.51.100.2", time.Hour, 1)
	f.add("198.51.100.3", time.Hour, 1)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.1/32@1m0s 198.51.100.2/32@1h0m0s 198.51.100.3/32@1h0m0s")

	f.now = t0.Add(time.Minute) // 198.51.100.1 expires; the engine has not refreshed yet
	f.gate.Handle(block("198.51.100.3", decision.ChangeRemoved, time.Time{}))
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.2/32@1h0m0s")

	// A block with less than MinTimeout left is not added.
	f.add("198.51.100.4", time.Minute+MinTimeout/2, 1)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.2/32@1h0m0s")
}

// TestAllowlistDefence: right before apply, blocks overlapping the
// allow-list are refused — a protected entry always, an operator entry
// unless the operator force-blocked the indicator — and withdrawn if they
// were applied before the allow-list changed.
func TestAllowlistDefence(t *testing.T) {
	allow := sovereignty.NewAllowlist()
	f := newFixture(t, config.ModeEnforce, newFake(), Options{Allowlist: func() *sovereignty.Allowlist { return allow }})
	f.add("198.51.100.7", time.Hour, 1)
	f.add("192.0.2.0/23", time.Hour, 1)
	f.add("203.0.113.5", time.Hour, 1)
	forced := block("203.0.113.9", decision.ChangeAdded, t0.Add(time.Hour))
	forced.Decision.Sovereignty = sovereignty.Ruling{Effect: sovereignty.EffectBlock, Rule: sovereignty.RuleForceBlock}
	f.gate.Handle(forced)
	f.reconcile(t)
	wantState(t, f.enf, "192.0.2.0/23@1h0m0s 198.51.100.7/32@1h0m0s 203.0.113.5/32@1h0m0s 203.0.113.9/32@1h0m0s")

	// The allow-list changes, e.g. on reload, before the engine re-decides.
	allow = sovereignty.NewAllowlist(
		sovereignty.Entry{Prefix: netip.MustParsePrefix("192.0.2.1/32"), Source: sovereignty.SourceSelf},
		sovereignty.Entry{Prefix: netip.MustParsePrefix("203.0.113.0/24"), Source: sovereignty.SourceConfig},
	)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.7/32@1h0m0s 203.0.113.9/32@1h0m0s")
	if got := f.rec.Detail(); got != "enforcing via fake: 2 entries, 2 refused by the allow-list" {
		t.Errorf("Detail = %q", got)
	}
	wantCounts(t, f.rec, 4, 2, 0, 2, 0)
	if n := strings.Count(f.logs.String(), "refused by the allow-list right before apply"); n != 2 {
		t.Errorf("logged %d refusals:\n%s", n, f.logs)
	}
	f.reconcile(t) // known refusals are not logged again
	if n := strings.Count(f.logs.String(), "refused by the allow-list right before apply"); n != 2 {
		t.Errorf("logged %d refusals after the second pass", n)
	}

	// The same prefix through another indicator that is force-blocked is
	// applied, and not counted as refused.
	same := block("203.0.113.5/32", decision.ChangeAdded, t0.Add(time.Hour))
	same.Decision.Sovereignty = sovereignty.Ruling{Effect: sovereignty.EffectBlock, Rule: sovereignty.RuleForceBlock}
	f.gate.Handle(same)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.7/32@1h0m0s 203.0.113.5/32@1h0m0s 203.0.113.9/32@1h0m0s")
	if got := f.rec.Detail(); got != "enforcing via fake: 3 entries, 1 refused by the allow-list" {
		t.Errorf("Detail = %q", got)
	}
	// The refused 203.0.113.5 shares the entry of the force-blocked /32.
	wantCounts(t, f.rec, 5, 3, 1, 1, 0)
	f.gate.Handle(block("203.0.113.5/32", decision.ChangeRemoved, time.Time{}))

	// A protected entry wins over a force-block.
	allow = sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("203.0.113.9/32"), Source: sovereignty.SourceBootstrap})
	f.reconcile(t)
	wantState(t, f.enf, "192.0.2.0/23@1h0m0s 198.51.100.7/32@1h0m0s 203.0.113.5/32@1h0m0s")
}

// TestMaxEntries: beyond enforce.max_entries the lowest-score blocks are
// skipped, logged and counted.
func TestMaxEntries(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(), Options{MaxEntries: 2})
	f.add("198.51.100.1", time.Hour, 0.5)
	f.add("198.51.100.2", time.Hour, 2)
	f.add("198.51.100.3", time.Hour, 1)
	f.add("198.51.100.4", time.Hour, 0.5)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.2/32@1h0m0s 198.51.100.3/32@1h0m0s")
	wantCounts(t, f.rec, 4, 2, 0, 0, 2)
	if got := f.rec.Detail(); got != "enforcing via fake: 2 entries, 2 skipped over enforce.max_entries" {
		t.Errorf("Detail = %q", got)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "enforce.max_entries reached") || !strings.Contains(logs, `"skipped":2`) ||
		!strings.Contains(logs, `"newly_skipped_sample":["198.51.100.1/32","198.51.100.4/32"]`) {
		t.Errorf("logs lack the skip:\n%s", logs)
	}

	// A higher score displaces the lowest applied one.
	f.add("198.51.100.1", time.Hour, 3)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.1/32@1h0m0s 198.51.100.2/32@1h0m0s")

	// The operator's force-block is kept first, whatever its score.
	forced := block("198.51.100.5", decision.ChangeAdded, t0.Add(time.Hour))
	forced.Decision.Score = 0
	forced.Decision.Sovereignty = sovereignty.Ruling{Effect: sovereignty.EffectBlock, Rule: sovereignty.RuleForceBlock}
	f.gate.Handle(forced)
	f.reconcile(t)
	wantState(t, f.enf, "198.51.100.1/32@1h0m0s 198.51.100.5/32@1h0m0s")
}

// TestObserveNeverApplies: in observe mode the reconciler neither sets up
// nor lists nor applies; it withdraws what an earlier run left behind
// once, and reports "observing".
func TestObserveNeverApplies(t *testing.T) {
	enf := newFake(entry("198.51.100.9/32", t0.Add(time.Hour))) // left behind by an earlier enforce run
	f := newFixture(t, config.ModeObserve, enf, Options{Debounce: time.Millisecond, Interval: 5 * time.Millisecond})
	f.add("198.51.100.1", time.Hour, 1)
	if err := f.rec.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.rec.Stop(context.Background()) })
	f.add("198.51.100.2", time.Hour, 1)
	f.rec.Trigger()
	waitFor(t, "observing", func() bool { return f.rec.Detail() == "observing" })
	time.Sleep(30 * time.Millisecond) // several intervals
	if got := enf.callsString(); got != "teardown" {
		t.Errorf("calls = %q, want only one teardown", got)
	}
	wantState(t, enf, "")
	if f.rec.Ready() != nil {
		t.Errorf("Ready = %v", f.rec.Ready())
	}
	wantCounts(t, f.rec, 0, 0, 0, 0, 0) // observe mode considers no block
}

// TestModeSwitch: switching to enforce applies the blocks, switching back
// withdraws them.
func TestModeSwitch(t *testing.T) {
	f := newFixture(t, config.ModeObserve, newFake(), Options{Debounce: time.Millisecond})
	f.add("198.51.100.1", time.Hour, 1)
	if err := f.rec.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.rec.Stop(context.Background()) })
	waitFor(t, "observing", func() bool { return f.rec.Detail() == "observing" })

	f.gate.SetMode(config.ModeEnforce)
	waitFor(t, "the block applied", func() bool { return f.enf.state() == "198.51.100.1/32@1h0m0s" })
	f.gate.SetMode(config.ModeObserve)
	waitFor(t, "the block withdrawn", func() bool { return f.rec.Detail() == "observing" && f.enf.state() == "" })
	if got := f.enf.callsString(); got != "teardown setup list apply teardown" {
		t.Errorf("calls = %q", got)
	}
	if !strings.Contains(f.logs.String(), "observe mode: every applied block was withdrawn") {
		t.Errorf("logs lack the withdrawal:\n%s", f.logs)
	}
}

// TestDebounce: changes that arrive together are applied in one pass,
// without waiting for the reconcile interval.
func TestDebounce(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(), Options{Debounce: 200 * time.Millisecond})
	if err := f.rec.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.rec.Stop(context.Background()) })
	waitFor(t, "the first pass", func() bool { return f.rec.Status().Mode == config.ModeEnforce })
	for _, v := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"} {
		f.add(v, time.Hour, 1)
	}
	waitFor(t, "the blocks applied", func() bool { return f.enf.count("apply") > 0 })
	time.Sleep(100 * time.Millisecond)
	if n := f.enf.count("apply"); n != 1 {
		t.Errorf("applied %d times, want 1", n)
	}
	wantState(t, f.enf, "198.51.100.1/32@1h0m0s 198.51.100.2/32@1h0m0s 198.51.100.3/32@1h0m0s")
}

// TestFailureRetry: a failed apply is retried with backoff, surfaced
// through Ready meanwhile, and recovers. Every retry sets the backend up
// again, restoring e.g. a table deleted by hand.
func TestFailureRetry(t *testing.T) {
	enf := newFake()
	enf.failApply = 3
	f := newFixture(t, config.ModeEnforce, enf, Options{MinBackoff: 10 * time.Millisecond, Interval: time.Hour})
	f.add("198.51.100.1", time.Hour, 1)
	if err := f.rec.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.rec.Stop(context.Background()) })
	waitFor(t, "a failure", func() bool { return f.rec.Ready() != nil })
	if err := f.rec.Ready(); !strings.Contains(err.Error(), "operation not permitted") || !strings.Contains(err.Error(), "retrying in") {
		t.Errorf("Ready = %v", err)
	}
	waitFor(t, "recovery", func() bool { return f.rec.Ready() == nil && enf.state() == "198.51.100.1/32@1h0m0s" })
	if n := enf.count("apply"); n != 4 {
		t.Errorf("applied %d times, want 4", n)
	}
	if n := enf.count("setup"); n != 4 {
		t.Errorf("set up %d times, want 4", n)
	}
	if n := strings.Count(f.logs.String(), "enforcement failed; retrying"); n != 3 {
		t.Errorf("logged %d failures", n)
	}
}

func TestBackoff(t *testing.T) {
	r := NewReconciler(nil, nil, Options{Interval: 10 * time.Second}, slog.New(slog.DiscardHandler))
	var got []string
	for n := 1; n <= 6; n++ {
		got = append(got, r.backoff(n).String())
	}
	if s := strings.Join(got, " "); s != "1s 2s 4s 8s 10s 10s" {
		t.Errorf("backoff = %s", s)
	}
}

func TestStartStop(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(), Options{})
	if got := f.rec.Detail(); got != "starting" {
		t.Errorf("Detail before the first pass = %q", got)
	}
	if err := f.rec.Stop(context.Background()); err != nil {
		t.Errorf("Stop before Start = %v", err)
	}
	if err := f.rec.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.rec.Start(context.Background()); err == nil {
		t.Error("second Start succeeded")
	}
	if err := f.rec.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.rec.Name() != Name {
		t.Errorf("Name = %q", f.rec.Name())
	}
	f.add("198.51.100.1", time.Hour, 1)
	f.reconcile(t)
	entries, err := f.rec.Entries(context.Background())
	if err != nil || entriesString(entries) != "198.51.100.1/32@1h0m0s" {
		t.Errorf("Entries = %v, %v", entries, err)
	}
}

// TestEntriesAfterTeardown: once observe mode tore the backend down,
// nothing is applied and the backend (e.g. a deleted nftables table) is
// not asked.
func TestEntriesAfterTeardown(t *testing.T) {
	f := newFixture(t, config.ModeObserve, newFake(), Options{})
	f.reconcile(t)
	entries, err := f.rec.Entries(context.Background())
	if err != nil || len(entries) != 0 {
		t.Errorf("Entries = %v, %v", entries, err)
	}
	if got := f.enf.callsString(); got != "teardown" {
		t.Errorf("calls = %q", got)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestObserveWithdrawalFailure: a failed teardown in observe mode is
// retried and reported.
func TestObserveWithdrawalFailure(t *testing.T) {
	enf := newFake(entry("198.51.100.9/32", t0.Add(time.Hour)))
	enf.failTeardown = 1
	f := newFixture(t, config.ModeObserve, enf, Options{MinBackoff: time.Hour, Interval: time.Hour})
	if err := f.rec.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.rec.Stop(context.Background()) })
	waitFor(t, "the failure", func() bool { return f.rec.Ready() != nil })
	if got := f.rec.Detail(); got != "observing (withdrawing the applied blocks failed)" {
		t.Errorf("Detail = %q", got)
	}
	if got := enf.callsString(); got != "teardown" {
		t.Errorf("calls = %q", got)
	}
}
