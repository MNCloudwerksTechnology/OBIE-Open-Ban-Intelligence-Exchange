package decision

import (
	"maps"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// wantPublisherCounts checks Engine.PublisherCounts, and that they add up
// to the verdicts of Engine.Counts.
func wantPublisherCounts(t *testing.T, e *Engine, want map[string]PublisherCount) {
	t.Helper()
	got := e.PublisherCounts()
	if !maps.Equal(got, want) {
		t.Errorf("PublisherCounts() = %v, want %v", got, want)
	}
	sum := 0
	for _, c := range got {
		sum += c.Verdicts
	}
	if n := e.Counts().Verdicts; sum != n {
		t.Errorf("the publishers' verdicts add up to %d, Counts().Verdicts = %d", sum, n)
	}
}

// TestEnginePublisherCounts: the engine counts the active verdicts of
// every publisher, and those that count in the decisions, as verdicts
// arrive, are replaced, revoked and expire, and as a reload changes the
// weights (ADR 0021).
func TestEnginePublisherCounts(t *testing.T) {
	f := newFixture(t, testPolicy())
	one, two := ipv4("203.0.113.1"), ipv4("203.0.113.2")
	f.put(t, one, pubA, 1, 2*time.Hour)
	b := f.put(t, one, pubB, 1, 2*time.Hour)
	f.put(t, one, pubD, 1, 2*time.Hour) // weight 0: held, not counting
	f.putEvent(t, verdictOn(two, pubA, obieproto.ActionWatch, 1, f.clock.Now(), time.Hour))
	f.put(t, two, pubB, 1, time.Hour)
	wantPublisherCounts(t, f.engine, map[string]PublisherCount{})
	f.start(t)
	wantPublisherCounts(t, f.engine, map[string]PublisherCount{
		pubA: {Verdicts: 2, Counting: 1}, pubB: {Verdicts: 2, Counting: 2}, pubD: {Verdicts: 1}})

	// A newer verdict replaces the watch verdict.
	f.clock.Advance(time.Second)
	f.put(t, two, pubA, 1, time.Hour)
	f.engine.processDirty()
	wantPublisherCounts(t, f.engine, map[string]PublisherCount{
		pubA: {Verdicts: 2, Counting: 2}, pubB: {Verdicts: 2, Counting: 2}, pubD: {Verdicts: 1}})

	f.revoke(t, b)
	f.engine.processDirty()
	wantPublisherCounts(t, f.engine, map[string]PublisherCount{
		pubA: {Verdicts: 2, Counting: 2}, pubB: {Verdicts: 1, Counting: 1}, pubD: {Verdicts: 1}})

	// A reload re-decides with the new weights.
	p := testPolicy()
	p.Weights[pubA], p.Weights[pubD] = 0, 0.5
	f.engine.Reload(p, f.engine.Allowlist())
	wantPublisherCounts(t, f.engine, map[string]PublisherCount{
		pubA: {Verdicts: 2}, pubB: {Verdicts: 1, Counting: 1}, pubD: {Verdicts: 1, Counting: 1}})

	// The verdicts on two expire: pubB holds none any more.
	f.clock.Advance(time.Hour)
	if err := f.store.Sweep(f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	wantPublisherCounts(t, f.engine, map[string]PublisherCount{pubA: {Verdicts: 1}, pubD: {Verdicts: 1, Counting: 1}})

	// The counts are a copy.
	f.engine.PublisherCounts()[pubA] = PublisherCount{Verdicts: 99}
	wantPublisherCounts(t, f.engine, map[string]PublisherCount{pubA: {Verdicts: 1}, pubD: {Verdicts: 1, Counting: 1}})
}

// TestEnginePublisherCountsWithoutVerdicts: a force-block without verdicts
// is kept, but counts for no publisher.
func TestEnginePublisherCountsWithoutVerdicts(t *testing.T) {
	f := newFixture(t, testPolicy())
	ind := ipv4("203.0.113.3")
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	if d, ok := f.decision(ind.Key()); !ok || d.State != StateBlock {
		t.Fatalf("force-block not kept: %+v, %v", d, ok)
	}
	wantPublisherCounts(t, f.engine, map[string]PublisherCount{})
}
