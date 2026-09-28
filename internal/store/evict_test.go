package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// newCappedDB returns a started in-memory store on clk holding at most max
// verdicts, with self as this node's peer ID.
func newCappedDB(t testing.TB, clk *clock, maxIndicators int, self string) *DB {
	t.Helper()
	return startDB(t, NewMemory(discardLogger(), Options{Now: clk.Now, MaxIndicators: maxIndicators, Self: self}))
}

// keys counts the keys with prefix, e.g. the stored events.
func keys(t testing.TB, db *DB, prefix []byte) int {
	t.Helper()
	n := 0
	err := db.view(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.IteratorOptions{Prefix: prefix})
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func checkVerdicts(t *testing.T, db *DB, want int64) {
	t.Helper()
	if got := db.Verdicts(); got != want {
		t.Errorf("Verdicts() = %d, want %d", got, want)
	}
	if got := keys(t, db, prefixVerdict); int64(got) != want {
		t.Errorf("%d verdict records stored, want %d", got, want)
	}
}

func TestCapEvictsVerdictExpiringFirst(t *testing.T) {
	clk := newClock()
	db := newCappedDB(t, clk, 3, "")
	rec := watch(db)
	evictionsBefore := testutil.ToFloat64(evictionsTotal)
	now := clk.Now()
	first := verdict(pubA, ipv4("85.0.0.1"), now, time.Hour)
	for _, ev := range []*obieproto.Event{
		verdict(pubA, ipv4("85.0.0.2"), now, 3*time.Hour),
		first,
		verdict(pubB, ipv4("85.0.0.2"), now, 2*time.Hour),
	} {
		mustPut(t, db, ev, true)
	}
	checkVerdicts(t, db, 3)
	rec.take()

	mustPut(t, db, verdict(pubA, ipv4("85.0.0.4"), now, 4*time.Hour), true)
	checkVerdicts(t, db, 3)
	if ids := activeIDs(t, db, "ipv4:85.0.0.1", now); len(ids) != 0 {
		t.Errorf("verdict expiring first still active: %v", ids)
	}
	if got := rec.take(); !slices.Contains(got, Change{Key: "ipv4:85.0.0.1", Reason: ReasonEvict}) {
		t.Errorf("changes = %v, want an eviction of ipv4:85.0.0.1", got)
	}
	if _, err := db.Get(first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(evicted event) error = %v, want ErrNotFound", err)
	}
	if st := db.Stats(); st.Evicted != 1 || st.Full != 0 {
		t.Errorf("stats = %+v, want 1 evicted", st)
	}
	if got := testutil.ToFloat64(evictionsTotal) - evictionsBefore; got != 1 {
		t.Errorf("obie_store_evictions_total rose by %v, want 1", got)
	}
	if got := testutil.ToFloat64(verdictsGauge); got != 3 {
		t.Errorf("obie_store_verdicts = %v, want 3", got)
	}

	// A replay of the evicted event is a duplicate, not a new verdict.
	mustPut(t, db, first, false)
	if st := db.Stats(); st.Duplicate != 1 || st.Evicted != 1 {
		t.Errorf("stats after replay = %+v, want 1 duplicate and still 1 evicted", st)
	}
	checkVerdicts(t, db, 3)
}

func TestCapRefusesVerdictExpiringFirst(t *testing.T) {
	clk := newClock()
	db := newCappedDB(t, clk, 2, "")
	now := clk.Now()
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.1"), now, time.Hour), true)
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.2"), now, time.Hour), true)

	short := verdict(pubB, ipv4("85.0.0.3"), now, 30*time.Minute)
	mustPut(t, db, short, false)
	same := verdict(pubB, ipv4("85.0.0.4"), now, time.Hour)
	mustPut(t, db, same, false)
	if st := db.Stats(); st.Full != 2 || st.Evicted != 0 {
		t.Errorf("stats = %+v, want 2 refused as full", st)
	}
	checkVerdicts(t, db, 2)
	if seen, err := db.Seen(short.ID); err != nil || !seen {
		t.Errorf("Seen(refused event) = %v, %v; want true so that replays are duplicates", seen, err)
	}

	// Newer verdicts on stored indicators need no room.
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.1"), now.Add(time.Second), 10*time.Minute), true)
	checkVerdicts(t, db, 2)
}

func TestCapNeverEvictsOwnVerdicts(t *testing.T) {
	clk := newClock()
	db := newCappedDB(t, clk, 2, pubA)
	now := clk.Now()
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.1"), now, time.Hour), true)
	mustPut(t, db, verdict(pubB, ipv4("85.0.0.2"), now, 5*time.Hour), true)

	// The foreign verdict goes although the own one expires first.
	mustPut(t, db, verdict(pubC, ipv4("85.0.0.3"), now, 10*time.Hour), true)
	if ids := activeIDs(t, db, "ipv4:85.0.0.2", now); len(ids) != 0 {
		t.Errorf("foreign verdict not evicted: %v", ids)
	}
	if ids := activeIDs(t, db, "ipv4:85.0.0.1", now); len(ids) != 1 {
		t.Errorf("own verdict evicted: %v", ids)
	}

	// An own verdict evicts a foreign one whatever their expiry.
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.4"), now, time.Minute), true)
	checkVerdicts(t, db, 2)
	if ids := activeIDs(t, db, "ipv4:85.0.0.3", now); len(ids) != 0 {
		t.Errorf("foreign verdict not evicted for an own one: %v", ids)
	}

	// With only own verdicts left, foreign ones are refused and own ones
	// still stored.
	mustPut(t, db, verdict(pubB, ipv4("85.0.0.5"), now, 20*time.Hour), false)
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.6"), now, time.Hour), true)
	checkVerdicts(t, db, 3)
	if st := db.Stats(); st.Evicted != 2 || st.Full != 1 {
		t.Errorf("stats = %+v, want 2 evicted and 1 refused", st)
	}
}

// TestCapEvictsRefreshedVerdictExpiringFirst checks that a verdict whose
// refresh moves its expiry before the last evicted one's is still evicted
// first.
func TestCapEvictsRefreshedVerdictExpiringFirst(t *testing.T) {
	clk := newClock()
	db := newCappedDB(t, clk, 3, "")
	now := clk.Now()
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.1"), now, time.Hour), true)
	mustPut(t, db, verdict(pubB, ipv4("85.0.0.2"), now, 2*time.Hour), true)
	mustPut(t, db, verdict(pubC, ipv4("85.0.0.3"), now, 3*time.Hour), true)
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.4"), now, 5*time.Hour), true) // evicts 85.0.0.1

	mustPut(t, db, verdict(pubB, ipv4("85.0.0.2"), now.Add(time.Second), 30*time.Minute), true)
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.5"), now, 6*time.Hour), true)
	if ids := activeIDs(t, db, "ipv4:85.0.0.2", now); len(ids) != 0 {
		t.Errorf("refreshed verdict expiring first not evicted: %v", ids)
	}
	if ids := activeIDs(t, db, "ipv4:85.0.0.3", now); len(ids) != 1 {
		t.Errorf("verdict expiring later evicted: %v", ids)
	}
	checkVerdicts(t, db, 3)
}

// TestCapBoundsFloodFromTrustedPeer floods the store with unique valid
// indicators from one publisher: the store keeps the verdicts that expire
// last, never more than the cap, and deletes the events of the others.
func TestCapBoundsFloodFromTrustedPeer(t *testing.T) {
	const maxIndicators, flood = 500, 5000
	clk := newClock()
	db := newCappedDB(t, clk, maxIndicators, pubA)
	now := clk.Now()
	own := verdict(pubA, ipv4("85.255.0.1"), now, time.Minute)
	mustPut(t, db, own, true)

	rng := rand.New(rand.NewPCG(1, 2)) // #nosec G404 -- reproducible test data.
	var ttls []time.Duration
	for i := range flood {
		ttl := time.Duration(60+rng.IntN(86400)) * time.Second
		ttls = append(ttls, ttl)
		if _, err := db.Put(verdict(pubB, ipv4(fmt.Sprintf("85.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff)), now, ttl)); err != nil {
			t.Fatal(err)
		}
		if got := db.Verdicts(); got > maxIndicators {
			t.Fatalf("after %d events the store holds %d verdicts, cap %d", i+1, got, maxIndicators)
		}
	}
	checkVerdicts(t, db, maxIndicators)
	if got := keys(t, db, prefixEvent); got != maxIndicators {
		t.Errorf("%d events stored, want %d: evicted events must go too", got, maxIndicators)
	}
	if ids := activeIDs(t, db, own.Key(), now); len(ids) != 1 {
		t.Errorf("own verdict evicted by the flood: %v", ids)
	}
	st := db.Stats()
	if st.Evicted+st.Full != flood-(maxIndicators-1) {
		t.Errorf("stats = %+v, want %d evicted or refused", st, flood-(maxIndicators-1))
	}

	// The survivors expire no earlier than every evicted or refused one:
	// the flood's maxIndicators-1 longest TTLs survive.
	slices.Sort(ttls)
	longestDropped := ttls[flood-maxIndicators]
	page, err := db.ListIndicators(now, Filter{Publisher: pubB}, Page{Limit: MaxPageLimit})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if got := item.Verdicts[0].ExpiresAt().Sub(now); got < longestDropped {
			t.Fatalf("kept a verdict with TTL %s, shorter than a dropped one's %s", got, longestDropped)
		}
	}
}

func TestCapShrinksLoweredStore(t *testing.T) {
	clk := newClock()
	dir := t.TempDir()
	db := New(dir, discardLogger(), Options{Now: clk.Now})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := clk.Now()
	for i := range 6 {
		mustPut(t, db, verdict(pubB, ipv4(fmt.Sprintf("85.0.0.%d", i+1)), now, time.Duration(i+1)*time.Hour), true)
	}
	if err := db.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	db = startDB(t, New(dir, discardLogger(), Options{Now: clk.Now, MaxIndicators: 3}))
	checkVerdicts(t, db, 6)
	for i := range 3 {
		mustPut(t, db, verdict(pubC, ipv4(fmt.Sprintf("85.0.1.%d", i+1)), now, 10*time.Hour), true)
	}
	checkVerdicts(t, db, 3)
}

func TestVerdictCountFollowsEveryChange(t *testing.T) {
	clk := newClock()
	dir := t.TempDir()
	db := New(dir, discardLogger(), Options{Now: clk.Now})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := clk.Now()
	v1 := verdict(pubA, ipv4("85.0.0.1"), now, time.Hour)
	v2 := verdict(pubB, ipv4("85.0.0.1"), now, 2*time.Hour)
	mustPut(t, db, v1, true)
	mustPut(t, db, v2, true)
	checkVerdicts(t, db, 2)
	// A refresh replaces the record.
	mustPut(t, db, verdict(pubA, ipv4("85.0.0.1"), now.Add(time.Second), 3*time.Hour), true)
	checkVerdicts(t, db, 2)
	// A revoked verdict is held until it expires.
	mustPut(t, db, revoke(pubB, v2, now.Add(time.Second)), true)
	checkVerdicts(t, db, 2)

	if err := db.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	db = startDB(t, New(dir, discardLogger(), Options{Now: clk.Now}))
	checkVerdicts(t, db, 2)

	// Expiry removes the revoked record, then the refreshed one.
	if err := db.Sweep(now.Add(2*time.Hour + time.Second)); err != nil {
		t.Fatal(err)
	}
	checkVerdicts(t, db, 1)
	if err := db.Sweep(now.Add(3*time.Hour + time.Second)); err != nil {
		t.Fatal(err)
	}
	checkVerdicts(t, db, 0)
}

// BenchmarkPutAtCap measures a Put that evicts: the store is full and every
// new verdict expires later than all stored ones.
func BenchmarkPutAtCap(b *testing.B) {
	const maxIndicators = 10000
	clk := newClock()
	db := newCappedDB(b, clk, maxIndicators, "")
	now := clk.Now()
	put := func(i int) {
		ev := verdict(pubB, ipv4(fmt.Sprintf("85.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff)), now, time.Hour+time.Duration(i)*time.Second)
		if _, err := db.Put(ev); err != nil {
			b.Fatal(err)
		}
	}
	for i := range maxIndicators {
		put(i)
	}
	b.ResetTimer()
	for i := range b.N {
		put(maxIndicators + i)
	}
	b.StopTimer()
	if st := db.Stats(); st.Evicted != uint64(b.N) { // #nosec G115 -- b.N is positive.
		b.Fatalf("evicted %d, want %d", st.Evicted, b.N)
	}
}
