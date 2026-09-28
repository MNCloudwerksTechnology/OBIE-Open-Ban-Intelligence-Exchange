package enforce

import (
	"context"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// TestSnapshotLookup: the snapshot of a pass tells for any range whether
// its own or a wider entry applies it, why it was skipped, or that it
// waits for an entry to expire.
func TestSnapshotLookup(t *testing.T) {
	allow := sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("192.0.2.9/32"), Source: sovereignty.SourceConfig})
	f := newFixture(t, config.ModeEnforce, newFake(entry("203.0.113.0/24", t0.Add(time.Second))),
		Options{MaxEntries: 4, Allowlist: func() *sovereignty.Allowlist { return allow }})
	f.add("198.51.100.0/24", time.Hour, 5)
	f.add("198.51.100.7", time.Hour, 5)
	f.add("192.0.2.1", time.Hour, 4)
	f.add("192.0.2.9", time.Hour, 9)          // refused by the allow-list
	f.add("2001:db8::/48", time.Hour, 3)      // IPv6
	f.add("203.0.113.128/25", 2*time.Hour, 3) // inside a dying entry: deferred
	f.add("198.18.0.0/16", time.Hour, 1)      // over the cap
	f.add("198.18.0.0/24", time.Hour, 1)      // inside the capped /16
	if s := f.rec.Snapshot(); s != nil {
		t.Fatalf("snapshot before the first pass: %+v", s)
	}
	f.reconcile(t)
	s := f.rec.Snapshot()
	if s == nil || s.Mode != config.ModeEnforce || !s.At.Equal(t0) || s.Seq != 1 {
		t.Fatalf("snapshot = %+v", s)
	}
	if got, want := entriesString(s.Entries), f.enf.state(); got != want {
		t.Errorf("snapshot entries = %q, backend holds %q", got, want)
	}
	p := netip.MustParsePrefix
	for _, tc := range []struct {
		prefix string
		want   Coverage
	}{
		{"192.0.2.1/32", Coverage{Applied: true, Entry: Entry{Prefix: p("192.0.2.1/32"), Expires: t0.Add(time.Hour)}}},
		{"198.51.100.7/32", Coverage{Applied: true, Entry: Entry{Prefix: p("198.51.100.0/24"), Expires: t0.Add(time.Hour)}}},
		{"198.51.100.0/25", Coverage{Applied: true, Entry: Entry{Prefix: p("198.51.100.0/24"), Expires: t0.Add(time.Hour)}}},
		{"2001:db8::1/128", Coverage{Applied: true, Entry: Entry{Prefix: p("2001:db8::/48"), Expires: t0.Add(time.Hour)}}},
		{"203.0.113.200/32", Coverage{Applied: true, Entry: Entry{Prefix: p("203.0.113.0/24"), Expires: t0.Add(time.Second)}}},
		{"192.0.2.9/32", Coverage{Skipped: SkipAllowlist, Within: p("192.0.2.9/32")}},
		{"198.18.0.0/16", Coverage{Skipped: SkipMaxEntries, Within: p("198.18.0.0/16")}},
		{"198.18.0.0/24", Coverage{Skipped: SkipMaxEntries, Within: p("198.18.0.0/16")}},
		{"198.18.0.5/32", Coverage{Skipped: SkipMaxEntries, Within: p("198.18.0.0/16")}},
		{"198.51.0.0/16", Coverage{}},   // around an entry: not applied
		{"192.0.2.2/32", Coverage{}},    // next to one
		{"2001:db9::1/128", Coverage{}}, // another IPv6 range
		{"::ffff:192.0.2.1/128", Coverage{}},
	} {
		if got := s.Lookup(p(tc.prefix)); got != tc.want {
			t.Errorf("Lookup(%s) = %+v, want %+v", tc.prefix, got, tc.want)
		}
		if got := s.Applies(p(tc.prefix)); got != tc.want.Applied {
			t.Errorf("Applies(%s) = %v", tc.prefix, got)
		}
	}

	// The dying entry holds the /25 whose addition waits for it; once it
	// is gone, the /25 is added.
	if got := s.Lookup(p("203.0.113.128/25")); !got.Applied || got.Entry.Prefix != p("203.0.113.0/24") {
		t.Errorf("Lookup(deferred under a dying entry) = %+v", got)
	}
	if !slices.Contains(s.Deferred, p("203.0.113.128/25")) {
		t.Errorf("Deferred = %v", s.Deferred)
	}
	f.now = t0.Add(time.Second)
	f.reconcile(t)
	if got := f.rec.Snapshot().Lookup(p("203.0.113.128/25")); !got.Applied || got.Entry.Prefix != p("203.0.113.128/25") {
		t.Errorf("Lookup(the /25 after the /24 expired) = %+v", got)
	}

	var none *Snapshot
	if got := none.Lookup(p("192.0.2.1/32")); got != (Coverage{}) || none.Applies(p("192.0.2.1/32")) {
		t.Errorf("nil snapshot Lookup = %+v", got)
	}
	if got := s.Lookup(netip.Prefix{}); got != (Coverage{}) {
		t.Errorf("Lookup(invalid) = %+v", got)
	}
}

// TestSnapshotDeferredLookup: a range whose addition waits for an entry
// it overlaps, which does not hold it, is reported deferred.
func TestSnapshotDeferredLookup(t *testing.T) {
	f := newFixture(t, config.ModeEnforce, newFake(entry("203.0.113.7/32", t0.Add(time.Second))), Options{})
	f.add("203.0.113.0/24", time.Hour, 1)
	f.reconcile(t)
	if got := f.rec.Snapshot().Lookup(netip.MustParsePrefix("203.0.113.0/24")); got != (Coverage{Deferred: true}) {
		t.Errorf("Lookup = %+v, want deferred", got)
	}
}

// TestSnapshotSeq: the sequence moves with what the backend holds, the
// skipped ranges and the mode, not with an unchanged pass or a failed one.
func TestSnapshotSeq(t *testing.T) {
	enf := newFake()
	f := newFixture(t, config.ModeEnforce, enf, Options{})
	f.add("192.0.2.1", time.Hour, 1)
	f.reconcile(t)
	seq := func() uint64 { return f.rec.Snapshot().Seq }
	if seq() != 1 {
		t.Fatalf("Seq = %d after the first pass", seq())
	}
	f.now = t0.Add(time.Minute)
	f.reconcile(t)
	if seq() != 1 {
		t.Errorf("Seq = %d after an unchanged pass", seq())
	}
	if !f.rec.Snapshot().At.Equal(t0.Add(time.Minute)) {
		t.Errorf("At = %v, want the last pass", f.rec.Snapshot().At)
	}
	f.add("192.0.2.1", 2*time.Hour, 1) // the expiry moves
	f.reconcile(t)
	if seq() != 2 {
		t.Errorf("Seq = %d after a renewal", seq())
	}

	enf.failApply = 1
	f.add("192.0.2.2", time.Hour, 1)
	if err := f.rec.Reconcile(context.Background()); err == nil {
		t.Fatal("the pass did not fail")
	}
	if s := f.rec.Snapshot(); s.Seq != 2 || len(s.Entries) != 1 {
		t.Errorf("snapshot after a failed pass = %+v, want the last successful one", s)
	}
	f.reconcile(t)
	if seq() != 3 {
		t.Errorf("Seq = %d after an addition", seq())
	}

	f.gate.SetMode(config.ModeObserve)
	f.reconcile(t)
	if s := f.rec.Snapshot(); s.Seq != 4 || s.Mode != config.ModeObserve || len(s.Entries) != 0 || len(s.Skipped) != 0 {
		t.Errorf("snapshot in observe mode = %+v", s)
	}
	if f.rec.Snapshot().Applies(netip.MustParsePrefix("192.0.2.1/32")) {
		t.Error("observe mode applies 192.0.2.1")
	}
	f.reconcile(t)
	if seq() != 4 {
		t.Errorf("Seq = %d after another observe pass", seq())
	}
}
