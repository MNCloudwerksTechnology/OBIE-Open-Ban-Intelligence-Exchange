package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// ended returns the ended verdicts f selects, all on one page.
func ended(t *testing.T, db *DB, f EndedFilter) EndedPage {
	t.Helper()
	page, err := db.EndedVerdicts(f, Page{Limit: MaxPageLimit})
	if err != nil {
		t.Fatalf("EndedVerdicts(%+v): %v", f, err)
	}
	return page
}

// endedIDs returns the event IDs of the ended verdicts f selects.
func endedIDs(t *testing.T, db *DB, f EndedFilter) []string {
	t.Helper()
	var ids []string
	for _, v := range ended(t, db, f).Verdicts {
		ids = append(ids, v.Event.ID)
	}
	return ids
}

func sweep(t *testing.T, db *DB, now time.Time) {
	t.Helper()
	if err := db.Sweep(now); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
}

// ttlOf returns the Badger expiry of the entry with key.
func ttlOf(t *testing.T, db *DB, key []byte) time.Time {
	t.Helper()
	var at uint64
	err := db.view(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		at = item.ExpiresAt()
		return nil
	})
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	return time.Unix(int64(at), 0) // #nosec G115 -- a Unix time the store wrote.
}

func TestRevokedVerdictIsKeptWithItsRevocation(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	v := verdict(pubA, ind, clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	if got := ended(t, db, EndedFilter{State: EndedRevoked}); len(got.Verdicts) != 0 || got.Total != 0 {
		t.Fatalf("ended before the revocation = %+v", got)
	}

	clk.Advance(time.Minute)
	r := revoke(pubA, v, clk.Now())
	mustPut(t, db, r, true)
	page := ended(t, db, EndedFilter{State: EndedRevoked})
	if len(page.Verdicts) != 1 {
		t.Fatalf("revoked = %+v, want the verdict", page)
	}
	got := page.Verdicts[0]
	want := Revocation{ID: r.ID, Reason: "false_positive", At: r.IssuedAt.Time}
	if got.Event.ID != v.ID || got.State != EndedRevoked || got.Revocation == nil || !reflect.DeepEqual(*got.Revocation, want) {
		t.Errorf("revoked verdict = %+v (revocation %+v), want %s with %+v", got, got.Revocation, v.ID, want)
	}
	if got.Cursor != "ipv4:11.0.0.1,"+pubA+",password_bruteforce/ssh" {
		t.Errorf("cursor = %q", got.Cursor)
	}
	if !reflect.DeepEqual(page.States, map[EndedState]int{EndedRevoked: 1}) || page.Total != 1 || page.Offset != 0 {
		t.Errorf("counts = %v, total %d, offset %d", page.States, page.Total, page.Offset)
	}
	if got := activeIDs(t, db, ind.Key(), clk.Now()); len(got) != 0 {
		t.Errorf("active = %v, want none", got)
	}

	// At its expiry the sweep drops the record; the verdict stays revoked,
	// not expired, until the retention after its expiry ends.
	clk.Advance(time.Hour)
	sweep(t, db, clk.Now())
	checkVerdicts(t, db, 0)
	if got := endedIDs(t, db, EndedFilter{State: EndedRevoked}); !slices.Equal(got, []string{v.ID}) {
		t.Errorf("revoked after expiry = %v", got)
	}
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); len(got) != 0 {
		t.Errorf("expired = %v, want none: it was revoked", got)
	}
	clk.Advance(DefaultEndedRetention - time.Minute - time.Second)
	if got := endedIDs(t, db, EndedFilter{State: EndedRevoked}); len(got) != 1 {
		t.Errorf("revoked a second before its retention ended = %v", got)
	}
	clk.Advance(time.Second)
	if got := ended(t, db, EndedFilter{State: EndedRevoked}); len(got.Verdicts) != 0 || got.Total != 0 {
		t.Errorf("revoked after its retention = %+v, want none", got)
	}
}

func TestExpiredVerdictIsKept(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	v := verdict(pubA, ind, clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	// The record outlives its verdict, so the sweep can still keep it after
	// a downtime.
	if got, want := ttlOf(t, db, verdictKey(ind.Key(), pubA)), v.ExpiresAt().Add(DefaultEndedRetention); !got.Equal(want) {
		t.Errorf("record TTL = %v, want %v", got, want)
	}

	clk.Advance(time.Hour)
	sweep(t, db, clk.Now())
	checkVerdicts(t, db, 0)
	page := ended(t, db, EndedFilter{State: EndedExpired})
	if len(page.Verdicts) != 1 || page.Verdicts[0].Event.ID != v.ID || page.Verdicts[0].State != EndedExpired ||
		page.Verdicts[0].Revocation != nil {
		t.Fatalf("expired = %+v, want the verdict", page)
	}
	if !reflect.DeepEqual(page.States, map[EndedState]int{EndedExpired: 1}) {
		t.Errorf("counts = %v", page.States)
	}
	key := endedKey(EndedExpired, ind.Key(), pubA, "password_bruteforce/ssh")
	if got, want := ttlOf(t, db, key), v.ExpiresAt().Add(DefaultEndedRetention); !got.Equal(want) {
		t.Errorf("ended verdict TTL = %v, want %v", got, want)
	}
	if got := tally(t, db).ByPublisher; !reflect.DeepEqual(got, map[string]EndedCount{pubA: {Expired: 1}}) {
		t.Errorf("EndedCounts = %v", got)
	}
	clk.Advance(DefaultEndedRetention)
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); len(got) != 0 {
		t.Errorf("expired after its retention = %v", got)
	}
	sweep(t, db, clk.Now())
	if got := tally(t, db).ByPublisher; len(got) != 0 {
		t.Errorf("EndedCounts after the retention and a sweep = %v", got)
	}
}

func TestSupersededVerdictIsNotKept(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	mustPut(t, db, verdict(pubA, ind, clk.Now(), time.Hour), true)
	cur := verdict(pubA, ind, clk.Now().Add(time.Second), 3*time.Hour)
	mustPut(t, db, cur, true)
	clk.Advance(2 * time.Hour)
	sweep(t, db, clk.Now())
	for _, state := range EndedStates {
		if got := endedIDs(t, db, EndedFilter{State: state}); len(got) != 0 {
			t.Errorf("%s = %v, want none: the verdict was refreshed, not ended", state, got)
		}
	}
}

func TestEarlyRevocationKeepsItsReason(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	v := verdict(pubA, ipv4("11.0.0.1"), clk.Now(), time.Hour)
	r := revoke(pubA, v, clk.Now())
	r.Reason = "test_traffic"
	mustPut(t, db, revoke(pubB, v, clk.Now()), true) // another publisher's: ignored
	mustPut(t, db, r, true)
	mustPut(t, db, v, true)

	page := ended(t, db, EndedFilter{State: EndedRevoked})
	if len(page.Verdicts) != 1 || page.Verdicts[0].Event.ID != v.ID {
		t.Fatalf("revoked = %+v, want the verdict", page)
	}
	want := Revocation{ID: r.ID, Reason: "test_traffic", At: r.IssuedAt.Time}
	if rev := page.Verdicts[0].Revocation; rev == nil || !reflect.DeepEqual(*rev, want) {
		t.Errorf("revocation = %+v, want %+v", rev, want)
	}

	// A marker written by an older version, without the revocation kept
	// apart: revoked, reason unknown.
	w := verdict(pubA, ipv4("11.0.0.2"), clk.Now(), time.Hour)
	err := db.db.Update(func(txn *badger.Txn) error {
		return txn.Set(revokeKey(w.ID, pubA), []byte(w.Key()))
	})
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, db, w, true)
	page = ended(t, db, EndedFilter{State: EndedRevoked, Key: w.Key()})
	if len(page.Verdicts) != 1 || page.Verdicts[0].Revocation != nil {
		t.Errorf("revoked by an old marker = %+v, want it without revocation", page)
	}
}

// TestExpiredRecordIsAbsentForPut: a record kept past its expiry changes
// nothing about which verdicts Put accepts, and is kept as expired when a
// verdict replaces it before the sweep.
func TestExpiredRecordIsAbsentForPut(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	start := clk.Now()
	old := verdict(pubA, ind, start, time.Hour)
	mustPut(t, db, old, true)
	clk.Advance(time.Hour) // old expired; no sweep yet
	rec.take()

	// Issued before old, but still active: accepted, as when Badger hid old.
	earlier := verdict(pubA, ind, start.Add(-time.Minute), 3*time.Hour)
	mustPut(t, db, earlier, true)
	if got := activeIDs(t, db, ind.Key(), clk.Now()); !slices.Equal(got, []string{earlier.ID}) {
		t.Errorf("active = %v, want [%s]", got, earlier.ID)
	}
	if got := rec.take(); !slices.Equal(got, []Change{{ind.Key(), ReasonVerdict}}) {
		t.Errorf("changes = %v", got)
	}
	checkVerdicts(t, db, 1)
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); !slices.Equal(got, []string{old.ID}) {
		t.Errorf("expired = %v, want [%s]", got, old.ID)
	}
	// The sweep finds old's expiry entry gone and changes nothing more.
	sweep(t, db, clk.Now())
	if got := rec.take(); len(got) != 0 {
		t.Errorf("sweep notified %v", got)
	}
	checkVerdicts(t, db, 1)
}

func TestEvictedExpiredVerdictIsKept(t *testing.T) {
	clk := newClock()
	db := newCappedDB(t, clk, 2, "")
	now := clk.Now()
	short := verdict(pubA, ipv4("85.0.0.1"), now, time.Hour)
	mustPut(t, db, short, true)
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.2"), now, 3*time.Hour), true)
	clk.Advance(time.Hour) // short expired; no sweep yet

	mustPut(t, db, verdict(pubB, ipv4("85.0.0.3"), clk.Now(), time.Hour), true)
	checkVerdicts(t, db, 2)
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); !slices.Equal(got, []string{short.ID}) {
		t.Errorf("expired = %v, want the evicted, expired verdict", got)
	}

	// An evicted active verdict did not end: it is not kept.
	mustPut(t, db, verdict(pubB, ipv4("85.0.0.4"), clk.Now(), 4*time.Hour), true)
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); len(got) != 1 {
		t.Errorf("expired = %v, want only the first", got)
	}
}

func TestEndedVerdictsFiltersPagesAndCounts(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	now := clk.Now()
	portScan := func(ev *obieproto.Event) *obieproto.Event {
		ev.Protocol, ev.Evidence.Reason = "tcp", "port_scan"
		return ev
	}
	var expired []*obieproto.Event
	for i := range 5 {
		ind := ipv4(ipv4Value(i))
		expired = append(expired, verdict(pubA, ind, now, time.Hour), verdict(pubB, ind, now, time.Hour))
	}
	scan := portScan(verdict(pubC, ipv4(ipv4Value(2)), now, time.Hour))
	expired = append(expired, scan)
	for _, ev := range expired {
		mustPut(t, db, ev, true)
	}
	revoked := verdict(pubA, ipv4(ipv4Value(9)), now, 2*time.Hour)
	mustPut(t, db, revoked, true)
	mustPut(t, db, revoke(pubA, revoked, now), true)
	clk.Advance(time.Hour)
	sweep(t, db, clk.Now())

	cases := []struct {
		name   string
		f      EndedFilter
		want   int
		states map[EndedState]int
	}{
		{"all expired", EndedFilter{State: EndedExpired}, 11, map[EndedState]int{EndedExpired: 11, EndedRevoked: 1}},
		{"publisher", EndedFilter{State: EndedExpired, Publisher: pubA}, 5, map[EndedState]int{EndedExpired: 5, EndedRevoked: 1}},
		{"except", EndedFilter{State: EndedExpired, Except: pubA}, 6, map[EndedState]int{EndedExpired: 6}},
		{"category", EndedFilter{State: EndedExpired, Category: "port_scan/tcp"}, 1, map[EndedState]int{EndedExpired: 1}},
		{"key", EndedFilter{State: EndedExpired, Key: "ipv4:" + ipv4Value(2)}, 3, map[EndedState]int{EndedExpired: 3}},
		{"key and publisher", EndedFilter{State: EndedRevoked, Key: "ipv4:" + ipv4Value(9), Publisher: pubA}, 1,
			map[EndedState]int{EndedRevoked: 1}},
		{"no match", EndedFilter{State: EndedRevoked, Publisher: pubC}, 0, map[EndedState]int{EndedExpired: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page := ended(t, db, c.f)
			if len(page.Verdicts) != c.want || page.Total != c.want || page.Next != "" {
				t.Errorf("got %d verdicts, total %d, next %q; want %d", len(page.Verdicts), page.Total, page.Next, c.want)
			}
			if !reflect.DeepEqual(page.States, c.states) {
				t.Errorf("states = %v, want %v", page.States, c.states)
			}
		})
	}

	// Pages of 4 walk every expired verdict once, in key order.
	var got []string
	var offsets []int
	p := Page{Limit: 4}
	for range 10 {
		page, err := db.EndedVerdicts(EndedFilter{State: EndedExpired}, p)
		if err != nil {
			t.Fatal(err)
		}
		offsets = append(offsets, page.Offset)
		for _, v := range page.Verdicts {
			got = append(got, v.Cursor)
		}
		if page.Total != 11 {
			t.Errorf("total = %d, want 11", page.Total)
		}
		if page.Next == "" {
			break
		}
		p.After = page.Next
	}
	if len(got) != 11 || !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Errorf("pages = %v, want 11 cursors in order", got)
	}
	if !slices.Equal(offsets, []int{0, 4, 8}) {
		t.Errorf("offsets = %v, want [0 4 8]", offsets)
	}

	want := map[string]EndedCount{pubA: {Revoked: 1, Expired: 5}, pubB: {Expired: 5}, pubC: {Expired: 1}}
	if got := tally(t, db).ByPublisher; !reflect.DeepEqual(got, want) {
		t.Errorf("EndedCounts = %v, want %v", got, want)
	}
}

func TestEndedVerdictsWhileClosed(t *testing.T) {
	db := NewMemory(discardLogger(), Options{})
	if _, err := db.EndedVerdicts(EndedFilter{State: EndedExpired}, Page{}); !errors.Is(err, ErrClosed) {
		t.Errorf("EndedVerdicts = %v, want ErrClosed", err)
	}
	if _, err := db.EndedCounts(); !errors.Is(err, ErrClosed) {
		t.Errorf("EndedCounts = %v, want ErrClosed", err)
	}
}

// TestSweepArchivesLargestEvents: a sweep batch of events of the maximum
// size, each kept as expired, fits into Badger's transactions.
func TestSweepArchivesLargestEvents(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	const n = sweepBatchSize + 3
	for i := range n {
		ev := verdict(pubA, ipv4(ipv4Value(i)), clk.Now(), time.Hour)
		for len(ev.MITRE)*6 < obieproto.MaxEventSize {
			ev.MITRE = append(ev.MITRE, "T1110")
		}
		mustPut(t, db, ev, true)
	}
	clk.Advance(time.Hour)
	sweep(t, db, clk.Now())
	checkVerdicts(t, db, 0)
	if page := ended(t, db, EndedFilter{State: EndedExpired}); page.Total != n {
		t.Errorf("expired = %d, want %d", page.Total, n)
	}
}

// tally returns the ended verdicts the store counts.
func tally(t *testing.T, db *DB) EndedTally {
	t.Helper()
	c, err := db.EndedCounts()
	if err != nil {
		t.Fatalf("EndedCounts: %v", err)
	}
	return c
}

// revoked stores a verdict of publisher on ind and its revocation.
func revoked(t *testing.T, db *DB, clk *clock, publisher string, ind obieproto.Indicator) *obieproto.Event {
	t.Helper()
	v := verdict(publisher, ind, clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	mustPut(t, db, revoke(publisher, v, clk.Now()), true)
	return v
}

// TestEndedVerdictsAreCapped: beyond Options.MaxEnded of other publishers'
// verdicts that ended in a state, the store keeps no more of them, so a
// flood of short-lived verdicts cannot fill the disk; this node's own are
// always kept, a flood of expiries does not crowd out revocations, and the
// sweep recounts them as they are forgotten.
func TestEndedVerdictsAreCapped(t *testing.T) {
	clk := newClock()
	db := startDB(t, NewMemory(discardLogger(), Options{Now: clk.Now, MaxEnded: 3, Self: pubC}))
	for i := range 6 {
		mustPut(t, db, verdict(pubA, ipv4(ipv4Value(i)), clk.Now(), time.Hour), true)
	}
	own := verdict(pubC, ipv4(ipv4Value(9)), clk.Now(), time.Hour)
	mustPut(t, db, own, true)
	clk.Advance(time.Hour)
	sweep(t, db, clk.Now())
	checkVerdicts(t, db, 0)
	if page := ended(t, db, EndedFilter{State: EndedExpired}); page.Total != 4 {
		t.Errorf("kept %d expired verdicts, want 3 of the flood and this node's own", page.Total)
	}
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired, Publisher: pubC}); !slices.Equal(got, []string{own.ID}) {
		t.Errorf("this node's own expired verdict = %v, want it kept beyond the cap", got)
	}
	c := tally(t, db)
	if want := map[string]EndedCount{pubA: {Expired: 3}, pubC: {Expired: 1}}; !reflect.DeepEqual(c.ByPublisher, want) || c.Max != 3 ||
		!reflect.DeepEqual(c.Full, map[EndedState]bool{EndedExpired: true, EndedRevoked: false}) {
		t.Errorf("tally = %+v", c)
	}

	// The expiries leave the revocations their own room.
	first := revoked(t, db, clk, pubB, ipv4("11.9.9.1"))
	if got := endedIDs(t, db, EndedFilter{State: EndedRevoked}); !slices.Equal(got, []string{first.ID}) {
		t.Errorf("revoked = %v, want [%s]", got, first.ID)
	}
	// A later verdict revoked on the same indicator replaces it and does
	// not count again.
	clk.Advance(time.Second)
	again := revoked(t, db, clk, pubB, ipv4("11.9.9.1"))
	if got := endedIDs(t, db, EndedFilter{State: EndedRevoked}); !slices.Equal(got, []string{again.ID}) {
		t.Errorf("revoked after a replacement = %v, want [%s]", got, again.ID)
	}
	if got := tally(t, db).ByPublisher[pubB]; got != (EndedCount{Revoked: 1}) {
		t.Errorf("pubB's count after a replacement = %+v, want 1 revoked", got)
	}
	revoked(t, db, clk, pubB, ipv4("11.9.9.2"))
	revoked(t, db, clk, pubA, ipv4("11.9.9.3"))
	beyond := revoked(t, db, clk, pubB, ipv4("11.9.9.4"))
	if got := endedIDs(t, db, EndedFilter{State: EndedRevoked, Key: beyond.Key()}); len(got) != 0 {
		t.Errorf("revoked beyond the cap = %v, want none kept", got)
	}
	if got := activeIDs(t, db, beyond.Key(), clk.Now()); len(got) != 0 {
		t.Errorf("active after the revocation = %v: the cap must not keep a verdict active", got)
	}
	mine := revoked(t, db, clk, pubC, ipv4("11.9.9.5"))
	if got := endedIDs(t, db, EndedFilter{State: EndedRevoked, Key: mine.Key()}); !slices.Equal(got, []string{mine.ID}) {
		t.Errorf("this node's own revocation beyond the cap = %v, want it kept", got)
	}
	if c := tally(t, db); !c.Full[EndedRevoked] || c.ByPublisher[pubC] != (EndedCount{Revoked: 1, Expired: 1}) {
		t.Errorf("tally = %+v", c)
	}

	// Once the retention ends, the sweep recounts and verdicts are kept
	// again.
	clk.Advance(DefaultEndedRetention + time.Hour)
	sweep(t, db, clk.Now())
	if c := tally(t, db); len(c.ByPublisher) != 0 || c.Full[EndedRevoked] || c.Full[EndedExpired] {
		t.Errorf("tally after the retention = %+v", c)
	}
	w := revoked(t, db, clk, pubB, ipv4("11.9.9.8"))
	if got := endedIDs(t, db, EndedFilter{State: EndedRevoked}); !slices.Equal(got, []string{w.ID}) {
		t.Errorf("revoked after the recount = %v, want [%s]", got, w.ID)
	}
}

// TestRevokedRecordReplacedIsNotExpired: a verdict that was revoked and is
// replaced after its expiry stays revoked; it is not kept as expired too.
func TestRevokedRecordReplacedIsNotExpired(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	v := revoked(t, db, clk, pubA, ind)
	clk.Advance(time.Hour) // v expired; no sweep yet
	mustPut(t, db, verdict(pubA, ind, clk.Now(), time.Hour), true)
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); len(got) != 0 {
		t.Errorf("expired = %v, want none", got)
	}
	if got := endedIDs(t, db, EndedFilter{State: EndedRevoked}); !slices.Equal(got, []string{v.ID}) {
		t.Errorf("revoked = %v, want [%s]", got, v.ID)
	}
}

func TestEndedDefaults(t *testing.T) {
	for _, tc := range []struct {
		opts Options
		want int
	}{
		{Options{}, DefaultMaxIndicators / 10},
		{Options{MaxIndicators: 20}, minMaxEnded},
		{Options{MaxIndicators: 50_000_000}, 5_000_000},
		{Options{MaxEnded: 7}, 7},
	} {
		if got := tc.opts.withDefaults().MaxEnded; got != tc.want {
			t.Errorf("MaxEnded of %+v = %d, want %d", tc.opts, got, tc.want)
		}
	}
}

// TestEndedAfterRestart: a verdict that expired while obied was down is
// kept as expired by the first sweep after the restart, and Start counts
// the ended verdicts kept.
func TestEndedAfterRestart(t *testing.T) {
	clk := newClock()
	dir := filepath.Join(t.TempDir(), "db")
	db := New(dir, discardLogger(), Options{Now: clk.Now})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	v := verdict(pubA, ipv4("11.0.0.1"), clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	revoked(t, db, clk, pubB, ipv4("11.0.0.2"))
	if err := db.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(3 * time.Hour) // down across the expiry
	db = startDB(t, New(dir, discardLogger(), Options{Now: clk.Now}))
	if got := tally(t, db).ByPublisher; !reflect.DeepEqual(got, map[string]EndedCount{pubB: {Revoked: 1}}) {
		t.Errorf("counts after the restart = %v", got)
	}
	sweep(t, db, clk.Now())
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); !slices.Equal(got, []string{v.ID}) {
		t.Errorf("expired after the first sweep = %v, want [%s]", got, v.ID)
	}
	if got := tally(t, db).ByPublisher; !reflect.DeepEqual(got, map[string]EndedCount{pubA: {Expired: 1}, pubB: {Revoked: 1}}) {
		t.Errorf("counts after the first sweep = %v", got)
	}
}

// TestEndedRetentionIsConfigurable: store.ended_retention sets how long a
// verdict record and an ended verdict outlive the verdict's expiry; zero
// takes the default of 30 days (ADR 0032).
func TestEndedRetentionIsConfigurable(t *testing.T) {
	if got := newMemDB(t, newClock()).EndedRetention(); got != 30*24*time.Hour {
		t.Errorf("default retention = %v, want 30 days", got)
	}
	clk := newClock()
	const retention = 2 * time.Hour
	db := startDB(t, NewMemory(discardLogger(), Options{Now: clk.Now, EndedRetention: retention}))
	if got := db.EndedRetention(); got != retention {
		t.Errorf("EndedRetention() = %v, want %v", got, retention)
	}
	ind := ipv4("11.0.0.9")
	v := verdict(pubA, ind, clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	if got, want := ttlOf(t, db, verdictKey(ind.Key(), pubA)), v.ExpiresAt().Add(retention); !got.Equal(want) {
		t.Errorf("record TTL = %v, want %v", got, want)
	}

	clk.Advance(time.Hour)
	sweep(t, db, clk.Now())
	key := endedKey(EndedExpired, ind.Key(), pubA, "password_bruteforce/ssh")
	if got, want := ttlOf(t, db, key), v.ExpiresAt().Add(retention); !got.Equal(want) {
		t.Errorf("ended verdict TTL = %v, want %v", got, want)
	}
	clk.Advance(retention - time.Second)
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); !slices.Equal(got, []string{v.ID}) {
		t.Errorf("expired a second before the retention ended = %v, want [%s]", got, v.ID)
	}
	clk.Advance(time.Second)
	if got := endedIDs(t, db, EndedFilter{State: EndedExpired}); len(got) != 0 {
		t.Errorf("expired after the retention = %v, want none", got)
	}
}
