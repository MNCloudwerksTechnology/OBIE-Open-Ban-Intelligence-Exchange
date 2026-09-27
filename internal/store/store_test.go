package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

var _ lifecycle.ReadinessChecker = (*DB)(nil)

func TestLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "db")
	db := New(dir, discardLogger(), Options{})
	if db.Name() != Name {
		t.Errorf("Name = %q, want %q", db.Name(), Name)
	}
	if err := db.Ready(); !errors.Is(err, ErrClosed) {
		t.Errorf("Ready before Start = %v, want ErrClosed", err)
	}
	if _, err := db.Get("x"); !errors.Is(err, ErrClosed) {
		t.Errorf("Get before Start = %v, want ErrClosed", err)
	}

	ctx := context.Background()
	if err := db.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := db.Ready(); err != nil {
		t.Errorf("Ready after Start = %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("database directory mode = %o, want 700", perm)
	}
	if err := db.Start(ctx); err == nil {
		t.Error("second Start succeeded")
	}

	if err := db.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := db.Ready(); !errors.Is(err, ErrClosed) {
		t.Errorf("Ready after Stop = %v, want ErrClosed", err)
	}
	if _, err := db.Put(verdict(pubA, ipv4("11.0.0.1"), time.Now(), time.Hour)); !errors.Is(err, ErrClosed) {
		t.Errorf("Put after Stop = %v, want ErrClosed", err)
	}
	if err := db.Stop(ctx); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

func TestStartFailsOnUnusableDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	db := New(filepath.Join(file, "db"), discardLogger(), Options{})
	if err := db.Start(context.Background()); err == nil {
		_ = db.Stop(context.Background())
		t.Fatal("Start succeeded below a regular file")
	}
}

func TestPutRejectsMalformedEvents(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	valid := func() *obieproto.Event { return verdict(pubA, ipv4("11.0.0.1"), clk.Now(), time.Hour) }
	tests := map[string]*obieproto.Event{
		"nil":             nil,
		"no id":           func() *obieproto.Event { e := valid(); e.ID = ""; return e }(),
		"no publisher":    func() *obieproto.Event { e := valid(); e.Publisher.PeerID = ""; return e }(),
		"no indicator":    func() *obieproto.Event { e := valid(); e.Indicator = obieproto.Indicator{}; return e }(),
		"no verdict body": func() *obieproto.Event { e := valid(); e.Verdict = nil; return e }(),
		"unknown type":    func() *obieproto.Event { e := valid(); e.Type = "x"; return e }(),
		"revoke without revokes": func() *obieproto.Event {
			e := revoke(pubA, valid(), clk.Now())
			e.Revokes = ""
			return e
		}(),
	}
	for name, ev := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := db.Put(ev); !errors.Is(err, ErrInvalid) {
				t.Errorf("Put = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestPutDeduplicates(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	v := verdict(pubA, ind, clk.Now(), time.Hour)

	mustPut(t, db, v, true)
	if got := rec.take(); !reflect.DeepEqual(got, []Change{{Key: ind.Key(), Reason: ReasonVerdict}}) {
		t.Errorf("changes = %v", got)
	}
	replay := *v
	mustPut(t, db, &replay, false)
	if got := rec.take(); len(got) != 0 {
		t.Errorf("replay notified %v", got)
	}

	got, err := db.Get(v.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got, v) {
		t.Errorf("Get = %+v, want %+v", got, v)
	}
	if _, err := db.Get(newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get unknown = %v, want ErrNotFound", err)
	}
	if st := db.Stats(); st.Accepted != 1 || st.Duplicate != 1 {
		t.Errorf("Stats = %+v", st)
	}
}

func TestSeen(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	cur := verdict(pubA, ind, clk.Now(), time.Hour)
	stale := verdict(pubA, ind, clk.Now().Add(-time.Minute), time.Hour)
	unknown := verdict(pubA, ind, clk.Now(), time.Hour)
	mustPut(t, db, cur, true)
	mustPut(t, db, stale, false)

	for _, tt := range []struct {
		name string
		id   string
		want bool
	}{
		{"accepted", cur.ID, true},
		{"ignored", stale.ID, true},
		{"never put", unknown.ID, false},
	} {
		if got, err := db.Seen(tt.id); got != tt.want || err != nil {
			t.Errorf("Seen(%s) = %v, %v; want %v", tt.name, got, err, tt.want)
		}
	}
}

func TestPutIgnoresExpiredEvents(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	v := verdict(pubA, ipv4("11.0.0.1"), clk.Now().Add(-2*time.Hour), time.Hour)
	mustPut(t, db, v, false)
	if st := db.Stats(); st.Expired != 1 {
		t.Errorf("Stats = %+v", st)
	}
	if _, err := db.Get(v.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
}

func TestLatestVerdictWins(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	now := clk.Now()

	first := verdict(pubA, ind, now.Add(-time.Minute), time.Hour)
	second := verdict(pubA, ind, now, time.Hour)
	older := verdict(pubA, ind, now.Add(-2*time.Minute), 2*time.Hour)
	other := verdict(pubB, ind, now.Add(-time.Hour), 2*time.Hour)

	mustPut(t, db, first, true)
	mustPut(t, db, second, true)
	mustPut(t, db, older, false)
	mustPut(t, db, other, true)

	if got, want := activeIDs(t, db, ind.Key(), now), []string{second.ID, other.ID}; !slices.Equal(got, want) {
		t.Errorf("active = %v, want %v", got, want)
	}
	wantChanges := []Change{
		{Key: ind.Key(), Reason: ReasonVerdict},
		{Key: ind.Key(), Reason: ReasonVerdict},
		{Key: ind.Key(), Reason: ReasonVerdict},
	}
	if got := rec.take(); !reflect.DeepEqual(got, wantChanges) {
		t.Errorf("changes = %v, want %v", got, wantChanges)
	}
	if st := db.Stats(); st.Stale != 1 || st.Accepted != 3 {
		t.Errorf("Stats = %+v", st)
	}
	// The superseded verdict is still known for deduplication.
	mustPut(t, db, first, false)
}

func TestSameSecondTieBreaksOnID(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	low := verdict(pubA, ind, clk.Now(), time.Hour)
	high := verdict(pubA, ind, clk.Now(), time.Hour) // newID increases
	mustPut(t, db, high, true)
	mustPut(t, db, low, false)
	if got := activeIDs(t, db, ind.Key(), clk.Now()); !slices.Equal(got, []string{high.ID}) {
		t.Errorf("active = %v, want [%s]", got, high.ID)
	}
}

func TestRevokeBySamePublisher(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	v := verdict(pubA, ind, clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	rec.take()

	r := revoke(pubA, v, clk.Now())
	mustPut(t, db, r, true)
	if got := activeIDs(t, db, ind.Key(), clk.Now()); len(got) != 0 {
		t.Errorf("active after revoke = %v", got)
	}
	if got := rec.take(); !reflect.DeepEqual(got, []Change{{Key: ind.Key(), Reason: ReasonRevoke}}) {
		t.Errorf("changes = %v", got)
	}
	if _, err := db.Get(r.ID); err != nil {
		t.Errorf("Get revoke: %v", err)
	}
	// A replayed verdict stays revoked, a newer one is active again.
	mustPut(t, db, v, false)
	newer := verdict(pubA, ind, clk.Now().Add(time.Second), time.Hour)
	mustPut(t, db, newer, true)
	if got := activeIDs(t, db, ind.Key(), clk.Now()); !slices.Equal(got, []string{newer.ID}) {
		t.Errorf("active = %v, want [%s]", got, newer.ID)
	}
}

func TestRevokeByOtherPublisherIsIgnored(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	v := verdict(pubA, ind, clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	rec.take()

	r := revoke(pubB, v, clk.Now())
	mustPut(t, db, r, false)
	if got := activeIDs(t, db, ind.Key(), clk.Now()); !slices.Equal(got, []string{v.ID}) {
		t.Errorf("active = %v, want [%s]", got, v.ID)
	}
	if got := rec.take(); len(got) != 0 {
		t.Errorf("foreign revoke notified %v", got)
	}
	if st := db.Stats(); st.ForeignRevoke != 1 {
		t.Errorf("Stats = %+v", st)
	}
	if _, err := db.Get(r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign revoke stored: %v", err)
	}
}

func TestRevokeForOtherIndicatorIsIgnored(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	v := verdict(pubA, ipv4("11.0.0.1"), clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	r := revoke(pubA, v, clk.Now())
	r.Indicator = ipv4("11.0.0.2")
	mustPut(t, db, r, false)
	if st := db.Stats(); st.InvalidRevoke != 1 {
		t.Errorf("Stats = %+v", st)
	}
	if got := activeIDs(t, db, v.Key(), clk.Now()); !slices.Equal(got, []string{v.ID}) {
		t.Errorf("active = %v", got)
	}
}

func TestRevokeOfSupersededVerdict(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	old := verdict(pubA, ind, clk.Now().Add(-time.Minute), time.Hour)
	cur := verdict(pubA, ind, clk.Now(), time.Hour)
	mustPut(t, db, old, true)
	mustPut(t, db, cur, true)
	rec.take()

	mustPut(t, db, revoke(pubA, old, clk.Now()), true)
	if got := activeIDs(t, db, ind.Key(), clk.Now()); !slices.Equal(got, []string{cur.ID}) {
		t.Errorf("active = %v, want [%s]", got, cur.ID)
	}
	if got := rec.take(); len(got) != 0 {
		t.Errorf("changes = %v, want none", got)
	}
}

func TestRevokeBeforeVerdict(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	v := verdict(pubA, ind, clk.Now(), time.Hour)

	mustPut(t, db, revoke(pubB, v, clk.Now()), true) // publisher unknown yet
	mustPut(t, db, revoke(pubA, v, clk.Now()), true)
	mustPut(t, db, v, true)
	if got := activeIDs(t, db, ind.Key(), clk.Now()); len(got) != 0 {
		t.Errorf("active = %v, want none", got)
	}
	if got := rec.take(); len(got) != 0 {
		t.Errorf("changes = %v, want none", got)
	}
	if st := db.Stats(); st.ForeignRevoke != 1 {
		t.Errorf("Stats = %+v", st)
	}

	// Only another publisher's early revocation: the verdict stays active.
	w := verdict(pubA, ipv4("11.0.0.2"), clk.Now(), time.Hour)
	mustPut(t, db, revoke(pubB, w, clk.Now()), true)
	mustPut(t, db, w, true)
	if got := activeIDs(t, db, w.Key(), clk.Now()); !slices.Equal(got, []string{w.ID}) {
		t.Errorf("active = %v, want [%s]", got, w.ID)
	}
}

func TestExpirySweepNotifies(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	short := verdict(pubA, ind, clk.Now(), time.Hour)
	long := verdict(pubB, ind, clk.Now(), 2*time.Hour)
	revoked := verdict(pubC, ind, clk.Now(), time.Hour)
	mustPut(t, db, short, true)
	mustPut(t, db, long, true)
	mustPut(t, db, revoked, true)
	mustPut(t, db, revoke(pubC, revoked, clk.Now()), true)
	rec.take()

	sweep := func() {
		t.Helper()
		if err := db.Sweep(clk.Now()); err != nil {
			t.Fatalf("Sweep: %v", err)
		}
	}
	sweep()
	if got := rec.take(); len(got) != 0 {
		t.Errorf("early sweep notified %v", got)
	}

	clk.Advance(time.Hour)
	sweep()
	if got := rec.take(); !reflect.DeepEqual(got, []Change{{Key: ind.Key(), Reason: ReasonExpiry}}) {
		t.Errorf("changes = %v", got)
	}
	if got := activeIDs(t, db, ind.Key(), clk.Now()); !slices.Equal(got, []string{long.ID}) {
		t.Errorf("active = %v, want [%s]", got, long.ID)
	}
	sweep()
	if got := rec.take(); len(got) != 0 {
		t.Errorf("repeated sweep notified %v", got)
	}

	clk.Advance(time.Hour)
	sweep()
	if got := rec.take(); !reflect.DeepEqual(got, []Change{{Key: ind.Key(), Reason: ReasonExpiry}}) {
		t.Errorf("changes = %v", got)
	}
	page, err := db.ListIndicators(clk.Now(), Filter{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Errorf("indicators after expiry = %+v", page.Items)
	}
	if _, err := db.Get(short.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get expired = %v, want ErrNotFound", err)
	}
}

func TestSweepSkipsSupersededVerdicts(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	mustPut(t, db, verdict(pubA, ind, clk.Now(), time.Hour), true)
	cur := verdict(pubA, ind, clk.Now().Add(time.Second), 3*time.Hour)
	mustPut(t, db, cur, true)
	rec.take()

	clk.Advance(2 * time.Hour)
	if err := db.Sweep(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if got := rec.take(); len(got) != 0 {
		t.Errorf("changes = %v, want none", got)
	}
	if got := activeIDs(t, db, ind.Key(), clk.Now()); !slices.Equal(got, []string{cur.ID}) {
		t.Errorf("active = %v", got)
	}
}

func TestSweepHandlesManyExpiries(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	const n = sweepBatchSize*2 + 7
	for i := range n {
		mustPut(t, db, verdict(pubA, ipv4(ipv4Value(i)), clk.Now(), time.Hour), true)
	}
	rec.take()
	clk.Advance(time.Hour)
	if err := db.Sweep(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if got := len(rec.take()); got != n {
		t.Errorf("notified %d indicators, want %d", got, n)
	}
}

func TestBackgroundSweep(t *testing.T) {
	clk := newClock()
	db := startDB(t, NewMemory(discardLogger(), Options{Now: clk.Now, SweepInterval: 5 * time.Millisecond}))
	expired := make(chan Change, 1)
	db.Subscribe(func(c Change) {
		if c.Reason == ReasonExpiry {
			expired <- c
		}
	})
	ind := ipv4("11.0.0.1")
	mustPut(t, db, verdict(pubA, ind, clk.Now(), time.Minute), true)
	clk.Advance(time.Minute)
	select {
	case c := <-expired:
		if c.Key != ind.Key() {
			t.Errorf("expired %q, want %q", c.Key, ind.Key())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no expiry notification from the background sweep")
	}
}

func TestUnsubscribe(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	calls := 0
	unsubscribe := db.Subscribe(func(Change) { calls++ })
	mustPut(t, db, verdict(pubA, ipv4("11.0.0.1"), clk.Now(), time.Hour), true)
	unsubscribe()
	mustPut(t, db, verdict(pubA, ipv4("11.0.0.2"), clk.Now(), time.Hour), true)
	if calls != 1 {
		t.Errorf("subscriber called %d times, want 1", calls)
	}
}

func TestSubscriberMayReadStore(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	var active []*obieproto.Event
	db.Subscribe(func(c Change) {
		var err error
		if active, err = db.ActiveVerdicts(c.Key, clk.Now()); err != nil {
			t.Errorf("ActiveVerdicts in subscriber: %v", err)
		}
	})
	v := verdict(pubA, ipv4("11.0.0.1"), clk.Now(), time.Hour)
	mustPut(t, db, v, true)
	if len(active) != 1 || active[0].ID != v.ID {
		t.Errorf("subscriber saw %v", active)
	}
}

func TestRestartPersistsState(t *testing.T) {
	clk := newClock()
	dir := filepath.Join(t.TempDir(), "db")
	ctx := context.Background()
	open := func() *DB {
		t.Helper()
		db := New(dir, discardLogger(), Options{Now: clk.Now})
		if err := db.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		return db
	}

	db := open()
	ind := ipv4("11.0.0.1")
	a := verdict(pubA, ind, clk.Now(), time.Hour)
	b := verdict(pubB, ind, clk.Now(), time.Hour)
	c := verdict(pubC, ind, clk.Now(), time.Hour)
	r := revoke(pubC, c, clk.Now())
	for _, ev := range []*obieproto.Event{a, b, c, r} {
		mustPut(t, db, ev, true)
	}
	o := Override{Indicator: ipv4("11.0.0.9"), Action: ForceAllow, Note: "our monitoring"}
	if err := db.SetOverride(o); err != nil {
		t.Fatal(err)
	}
	before, err := db.ListIndicators(clk.Now(), Filter{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	db = open()
	defer func() {
		if err := db.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()
	after, err := db.ListIndicators(clk.Now(), Filter{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Errorf("indicators after restart = %+v, want %+v", after, before)
	}
	if got := activeIDs(t, db, ind.Key(), clk.Now()); !slices.Equal(got, []string{a.ID, b.ID}) {
		t.Errorf("active after restart = %v", got)
	}
	for _, ev := range []*obieproto.Event{a, b, c, r} {
		mustPut(t, db, ev, false) // still deduplicated
	}
	got, err := db.Override(o.Indicator.Key(), clk.Now())
	if err != nil {
		t.Fatalf("Override after restart: %v", err)
	}
	if got.Action != ForceAllow || got.Note != o.Note {
		t.Errorf("override after restart = %+v", got)
	}

	// Expiry still works after a restart.
	rec := watch(db)
	clk.Advance(time.Hour)
	if err := db.Sweep(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if got := rec.take(); !reflect.DeepEqual(got, []Change{{Key: ind.Key(), Reason: ReasonExpiry}}) {
		t.Errorf("changes = %v", got)
	}
}

func TestValueLogGC(t *testing.T) {
	clk := newClock()
	db := startDB(t, New(filepath.Join(t.TempDir(), "db"), discardLogger(), Options{Now: clk.Now}))
	mustPut(t, db, verdict(pubA, ipv4("11.0.0.1"), clk.Now(), time.Hour), true)
	if err := db.runValueLogGC(); err != nil {
		t.Errorf("runValueLogGC: %v", err)
	}
	mem := newMemDB(t, clk)
	if err := mem.runValueLogGC(); err != nil {
		t.Errorf("runValueLogGC in memory: %v", err)
	}
}

func TestIgnoredEventsReplayAsDuplicates(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	cur := verdict(pubA, ind, clk.Now(), time.Hour)
	stale := verdict(pubA, ind, clk.Now().Add(-time.Minute), time.Hour)
	foreign := revoke(pubB, cur, clk.Now())
	mustPut(t, db, cur, true)
	for range 3 {
		mustPut(t, db, stale, false)
		mustPut(t, db, foreign, false)
	}
	if st := db.Stats(); st.Stale != 1 || st.ForeignRevoke != 1 || st.Duplicate != 4 {
		t.Errorf("Stats = %+v", st)
	}
	if _, err := db.Get(stale.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get ignored event = %v, want ErrNotFound", err)
	}
}

func TestEarlyRevokeForOtherIndicatorIsCounted(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	v := verdict(pubA, ipv4("11.0.0.1"), clk.Now(), time.Hour)
	r := revoke(pubA, v, clk.Now())
	r.Indicator = ipv4("11.0.0.2")
	mustPut(t, db, r, true)
	mustPut(t, db, v, true)
	if got := activeIDs(t, db, v.Key(), clk.Now()); !slices.Equal(got, []string{v.ID}) {
		t.Errorf("active = %v, want [%s]", got, v.ID)
	}
	if st := db.Stats(); st.InvalidRevoke != 1 {
		t.Errorf("Stats = %+v", st)
	}
}

func TestPutRejectsNULInKeyFields(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	v := verdict(pubA, ipv4("11.0.0.1\x00x"), clk.Now(), time.Hour)
	if _, err := db.Put(v); !errors.Is(err, ErrInvalid) {
		t.Errorf("Put = %v, want ErrInvalid", err)
	}
}

// TestExpiryAfterBadgerTTL covers production timing: Badger has already
// hidden the expired entries by wall clock when the sweep runs.
func TestExpiryAfterBadgerTTL(t *testing.T) {
	db := startDB(t, NewMemory(discardLogger(), Options{}))
	rec := watch(db)
	now := time.Now()
	ind := ipv4("11.0.0.1")
	v := verdict(pubA, ind, now.Add(-(time.Minute - 2*time.Second)), time.Minute)
	mustPut(t, db, v, true)
	over := ipv4("11.0.0.2")
	if err := db.SetOverride(Override{Indicator: over, Action: ForceBlock, ExpiresAt: now.Add(3 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	rec.take()

	time.Sleep(3 * time.Second)
	if _, err := db.Get(v.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after TTL = %v, want ErrNotFound", err)
	}
	if err := db.Sweep(time.Now()); err != nil {
		t.Fatal(err)
	}
	want := []Change{{ind.Key(), ReasonExpiry}, {over.Key(), ReasonExpiry}}
	if got := rec.take(); !slices.Equal(got, want) {
		t.Errorf("changes = %v, want %v", got, want)
	}
}

func TestSweepDropsUndecodableEntries(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.2")
	mustPut(t, db, verdict(pubA, ind, clk.Now(), time.Hour), true)
	bad := verdictKey("ipv4:11.0.0.1", pubA)
	err := db.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(bad, []byte("{not json")); err != nil {
			return err
		}
		return txn.Set(expiryKey(clk.Now().Add(time.Minute), bad), nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	rec.take()

	clk.Advance(time.Hour)
	if err := db.Sweep(clk.Now()); err != nil {
		t.Fatalf("Sweep = %v", err)
	}
	if got := rec.take(); !slices.Equal(got, []Change{{ind.Key(), ReasonExpiry}}) {
		t.Errorf("changes = %v", got)
	}
}

func TestConcurrentStop(t *testing.T) {
	db := NewMemory(discardLogger(), Options{})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- db.Stop(context.Background()) }()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("Stop: %v", err)
		}
	}
}
