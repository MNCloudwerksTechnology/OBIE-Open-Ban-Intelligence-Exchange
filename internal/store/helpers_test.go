package store

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

const (
	pubA = "12D3KooWPublisherAaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pubB = "12D3KooWPublisherBbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pubC = "12D3KooWPublisherCccccccccccccccccccccccccccccccc"
)

// clock is a settable test clock. It starts at the current second, because
// Badger applies TTLs against the wall clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock {
	return &clock{t: time.Now().UTC().Truncate(time.Second)}
}

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

var idCounter atomic.Uint64

// newID returns a unique, increasing UUIDv7-shaped event ID.
func newID() string {
	return fmt.Sprintf("01900000-0000-7000-8000-%012x", idCounter.Add(1))
}

func ipv4(value string) obieproto.Indicator {
	return obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"}
}

// verdict returns a verdict by publisher on ind issued at issued with ttl.
func verdict(publisher string, ind obieproto.Indicator, issued time.Time, ttl time.Duration) *obieproto.Event {
	return &obieproto.Event{
		ID:        newID(),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(issued),
		Indicator: ind,
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 10, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.9, TTLSeconds: int64(ttl / time.Second)},
		Publisher: obieproto.Publisher{PeerID: publisher},
	}
}

// revoke returns a revocation of v by publisher, issued at issued.
func revoke(publisher string, v *obieproto.Event, issued time.Time) *obieproto.Event {
	return &obieproto.Event{
		ID:        newID(),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeRevoke,
		IssuedAt:  obieproto.NewTimestamp(issued),
		Indicator: v.Indicator,
		Revokes:   v.ID,
		Reason:    "false_positive",
		Publisher: obieproto.Publisher{PeerID: publisher},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// startDB starts db and stops it when the test ends.
func startDB(t testing.TB, db *DB) *DB {
	t.Helper()
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return db
}

// newMemDB returns a started in-memory store on clk.
func newMemDB(t testing.TB, clk *clock) *DB {
	t.Helper()
	return startDB(t, NewMemory(discardLogger(), Options{Now: clk.Now}))
}

// recorder collects change notifications.
type recorder struct {
	mu      sync.Mutex
	changes []Change
}

func watch(db *DB) *recorder {
	r := &recorder{}
	db.Subscribe(func(c Change) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.changes = append(r.changes, c)
	})
	return r
}

// take returns and clears the recorded changes.
func (r *recorder) take() []Change {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.changes
	r.changes = nil
	return out
}

func mustPut(t *testing.T, db *DB, ev *obieproto.Event, want bool) {
	t.Helper()
	got, err := db.Put(ev)
	if err != nil {
		t.Fatalf("Put(%s): %v", ev.ID, err)
	}
	if got != want {
		t.Fatalf("Put(%s %s) accepted = %v, want %v", ev.Type, ev.ID, got, want)
	}
}

// activeIDs returns the IDs of the active verdicts on key.
func activeIDs(t *testing.T, db *DB, key string, now time.Time) []string {
	t.Helper()
	evs, err := db.ActiveVerdicts(key, now)
	if err != nil {
		t.Fatalf("ActiveVerdicts: %v", err)
	}
	ids := make([]string, len(evs))
	for i, ev := range evs {
		ids[i] = ev.ID
	}
	return ids
}
