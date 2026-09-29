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

// wantCategories checks Engine.Categories.
func wantCategories(t *testing.T, e *Engine, want map[string]int) {
	t.Helper()
	if got := e.Categories(); !maps.Equal(got, want) {
		t.Errorf("Categories() = %v, want %v", got, want)
	}
}

// verdictAbout is a ban verdict by publisher on ind about reason and
// protocol, issued at issued.
func verdictAbout(ind obieproto.Indicator, publisher, reason, protocol string, issued time.Time) *obieproto.Event {
	v := verdictOn(ind, publisher, obieproto.ActionBan, 1, issued, time.Hour)
	v.Evidence.Reason, v.Protocol = reason, protocol
	return v
}

// TestEngineCategories: the engine counts the decisions holding a verdict
// of each category once per decision, as verdicts arrive, are replaced and
// are revoked (ADR 0022).
func TestEngineCategories(t *testing.T) {
	f := newFixture(t, testPolicy())
	one, two := ipv4("203.0.113.1"), ipv4("203.0.113.2")
	f.putEvent(t, verdictAbout(one, pubA, "password_bruteforce", "ssh", f.clock.Now()))
	b := verdictAbout(one, pubB, "password_bruteforce", "ssh", f.clock.Now())
	f.putEvent(t, b)
	f.putEvent(t, verdictAbout(two, pubA, "port_scan", "tcp", f.clock.Now()))
	wantCategories(t, f.engine, map[string]int{})
	f.start(t)
	wantCategories(t, f.engine, map[string]int{"password_bruteforce/ssh": 1, "port_scan/tcp": 1})

	// Another category on one; a newer verdict of A on two replaces its
	// category.
	f.clock.Advance(time.Second)
	f.putEvent(t, verdictAbout(one, pubC, "port_scan", "tcp", f.clock.Now()))
	f.putEvent(t, verdictAbout(two, pubA, "password_bruteforce", "ssh", f.clock.Now()))
	f.engine.processDirty()
	wantCategories(t, f.engine, map[string]int{"password_bruteforce/ssh": 2, "port_scan/tcp": 1})

	f.revoke(t, b)
	f.engine.processDirty()
	wantCategories(t, f.engine, map[string]int{"password_bruteforce/ssh": 2, "port_scan/tcp": 1})

	f.clock.Advance(time.Hour)
	if err := f.store.Sweep(f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	wantCategories(t, f.engine, map[string]int{})
}

func TestCategory(t *testing.T) {
	for _, tc := range []struct{ reason, protocol, category string }{
		{"password_bruteforce", "ssh", "password_bruteforce/ssh"},
		{"port_scan", "", "port_scan"},
	} {
		if got := Category(tc.reason, tc.protocol); got != tc.category {
			t.Errorf("Category(%q, %q) = %q, want %q", tc.reason, tc.protocol, got, tc.category)
		}
	}
}

// TestEngineLookups: a kept decision is read by key, the policy in effect
// follows reloads, contributions carry the verdict's reason and protocol,
// and the generation moves with every evaluation that keeps or drops a
// decision, and only then.
func TestEngineLookups(t *testing.T) {
	f := newFixture(t, testPolicy())
	ind := ipv4("203.0.113.4")
	f.putEvent(t, verdictAbout(ind, pubA, "port_scan", "tcp", f.clock.Now()))
	f.start(t)
	d, ok := f.engine.Decision(ind.Key())
	if !ok || d.State != StateNone || d.Indicator != ind || d.Publishers != nil {
		t.Errorf("Decision(%s) = %+v, %v", ind.Key(), d, ok)
	}
	if _, ok := f.engine.Decision("ipv4:203.0.113.99"); ok {
		t.Error("Decision of an unknown indicator found")
	}
	x, err := f.engine.Explain(ind)
	if err != nil || len(x.Publishers) != 1 || x.Publishers[0].Reason != "port_scan" || x.Publishers[0].Protocol != "tcp" {
		t.Errorf("Explain(%s) = %+v, %v", ind.Key(), x.Publishers, err)
	}

	gen := f.engine.Generation()
	f.engine.processDirty() // nothing dirty
	f.engine.reevaluateAll([]string{"ipv4:203.0.113.99"}, func(string) string { return CauseRefresh })
	if g := f.engine.Generation(); g != gen {
		t.Errorf("Generation moved from %d to %d without a kept decision", gen, g)
	}
	f.put(t, ind, pubB, 1, time.Hour)
	f.engine.processDirty()
	if g := f.engine.Generation(); g != gen+1 {
		t.Errorf("Generation = %d after one evaluation, want %d", g, gen+1)
	}

	p := testPolicy()
	p.Threshold = 3
	f.engine.Reload(p, f.engine.Allowlist())
	if got := f.engine.Policy(); got.Threshold != 3 || got.Names[pubA] != "alpha" {
		t.Errorf("Policy() = %+v after a reload", got)
	}
}
