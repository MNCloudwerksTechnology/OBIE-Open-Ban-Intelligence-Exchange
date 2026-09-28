package enforce

import (
	"context"
	"fmt"
	"math/rand/v2"
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

// TestLookupInheritsOnlyTheCap: a range inside a wider block the cap
// left out is left out with it, but not one inside a block the
// allow-list refused: the allow-list refuses ranges one by one.
func TestLookupInheritsOnlyTheCap(t *testing.T) {
	allow := sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("203.0.113.9/32"), Source: sovereignty.SourceConfig})
	f := newFixture(t, config.ModeEnforce, newFake(), Options{Allowlist: func() *sovereignty.Allowlist { return allow }})
	f.add("203.0.113.0/24", time.Hour, 1)
	f.reconcile(t)
	s := f.rec.Snapshot()
	if got := s.Lookup(netip.MustParsePrefix("203.0.113.0/24")); got != (Coverage{Skipped: SkipAllowlist, Within: netip.MustParsePrefix("203.0.113.0/24")}) {
		t.Errorf("Lookup(the refused block) = %+v", got)
	}
	if got := s.Lookup(netip.MustParsePrefix("203.0.113.1/32")); got != (Coverage{}) {
		t.Errorf("Lookup(an address inside the refused block) = %+v, want none", got)
	}
}

// TestEntryIndex: an index of entries in any order, as a backend lists
// them, finds the entry holding a range.
func TestEntryIndex(t *testing.T) {
	x := NewEntryIndex([]Entry{entry("2001:db8::/48", t0), entry("198.51.100.0/24", t0), entry("192.0.2.1/32", t0)})
	for _, tc := range []struct{ p, want string }{
		{"198.51.100.7/32", "198.51.100.0/24"},
		{"192.0.2.1/32", "192.0.2.1/32"},
		{"2001:db8::1/128", "2001:db8::/48"},
		{"192.0.2.2/32", ""},
		{"198.51.0.0/16", ""},
	} {
		p := netip.MustParsePrefix(tc.p)
		e, ok := x.Holder(p)
		got := ""
		if ok {
			got = e.Prefix.String()
		}
		if got != tc.want || x.Holds(p) != ok {
			t.Errorf("Holder(%s) = %q, want %q", tc.p, got, tc.want)
		}
	}
	var none *EntryIndex
	if none.Holds(netip.MustParsePrefix("192.0.2.1/32")) {
		t.Error("a nil index holds something")
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

// BenchmarkSnapshotApplies asks a snapshot of 100,000 entries (the
// default enforce.max_entries) whether it applies an address: what the
// decisions list's firewall filter asks of every decision
// (performance.md).
func BenchmarkSnapshotApplies(b *testing.B) {
	s := &Snapshot{Entries: make([]Entry, 100_000)}
	for i := range s.Entries {
		s.Entries[i] = Entry{Prefix: netip.PrefixFrom(netip.AddrFrom4([4]byte{11, byte(i >> 16), byte(i >> 8), byte(i)}), 32)}
	}
	s.index()
	addrs := make([]netip.Prefix, 1024)
	for i := range addrs {
		addrs[i] = netip.PrefixFrom(netip.AddrFrom4([4]byte{11, byte(i >> 4), byte(i), byte(i * 7)}), 32)
	}
	i := 0
	for b.Loop() {
		s.Applies(addrs[i%len(addrs)])
		i++
	}
}

func TestSpans(t *testing.T) {
	for _, tc := range []struct {
		p           string
		first, last uint32
	}{
		{"0.0.0.0/0", 0, 0xffffffff},
		{"198.51.100.0/24", 0xc6336400, 0xc63364ff},
		{"198.51.100.7/32", 0xc6336407, 0xc6336407},
	} {
		if got := span4(netip.MustParsePrefix(tc.p)); got != (span[uint32]{tc.first, tc.last}) {
			t.Errorf("span4(%s) = %x", tc.p, got)
		}
	}
	for _, tc := range []struct {
		p           string
		first, last u128
	}{
		{"::/0", u128{}, u128{^uint64(0), ^uint64(0)}},
		{"2001:db8::/32", u128{0x20010db800000000, 0}, u128{0x20010db8ffffffff, ^uint64(0)}},
		{"2001:db8::/64", u128{0x20010db800000000, 0}, u128{0x20010db800000000, ^uint64(0)}},
		{"2001:db8::8000:0:0:0/65", u128{0x20010db800000000, 0x8000000000000000}, u128{0x20010db800000000, ^uint64(0)}},
		{"2001:db8::1/128", u128{0x20010db800000000, 1}, u128{0x20010db800000000, 1}},
	} {
		if got := span6(netip.MustParsePrefix(tc.p)); got != (span[u128]{tc.first, tc.last}) {
			t.Errorf("span6(%s) = %x", tc.p, got)
		}
	}
}

// TestHolderMatchesContainment: the snapshot finds the entry holding a
// range exactly when one of its disjoint entries contains it, for IPv4 and
// IPv6 ranges of every length.
func TestHolderMatchesContainment(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4)) // #nosec G404 -- reproducible test data.
	random := func() netip.Prefix {
		if rng.IntN(3) == 0 {
			a := netip.MustParseAddr(fmt.Sprintf("2001:%x::%x", rng.IntN(4), rng.IntN(256)))
			return netip.PrefixFrom(a, 16+rng.IntN(113)).Masked()
		}
		a := netip.MustParseAddr(fmt.Sprintf("198.51.%d.%d", rng.IntN(4), rng.IntN(256)))
		return netip.PrefixFrom(a, 12+rng.IntN(21)).Masked()
	}
	var entries []Entry
	for range 200 {
		p := random()
		if !slices.ContainsFunc(entries, func(e Entry) bool { return e.Prefix.Overlaps(p) }) {
			entries = append(entries, Entry{Prefix: p})
		}
	}
	sortEntries(entries)
	s := &Snapshot{Entries: entries}
	s.index()
	for range 5000 {
		q := random()
		want := slices.IndexFunc(entries, func(e Entry) bool { return e.Prefix.Bits() <= q.Bits() && e.Prefix.Contains(q.Addr()) })
		got, ok := s.holder(q)
		if ok != (want >= 0) || (ok && got != entries[want]) {
			t.Fatalf("holder(%s) = %v, %v; want entry %d of %v", q, got, ok, want, entries)
		}
	}
}
