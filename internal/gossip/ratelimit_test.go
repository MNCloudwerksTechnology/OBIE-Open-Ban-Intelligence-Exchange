package gossip

import (
	"testing"
	"time"
)

func allow(l *limiter, key string, now time.Time) bool { return l.take(key, now) != nil }

func TestLimiter(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	l := newLimiter(2, 3)

	for i := range 3 {
		if !allow(l, "a", start) {
			t.Fatalf("event %d within the burst was denied", i+1)
		}
	}
	if allow(l, "a", start) {
		t.Error("event beyond the burst was allowed")
	}
	if !allow(l, "b", start) {
		t.Error("another key shares the exhausted bucket")
	}
	if !allow(l, "a", start.Add(500*time.Millisecond)) {
		t.Error("the bucket did not refill at 2 events per second")
	}
	if allow(l, "a", start.Add(500*time.Millisecond)) {
		t.Error("the bucket refilled more than one token in 500ms")
	}
}

func TestLimiterDropsFullBuckets(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	l := newLimiter(10, 50) // refills in 5s

	for i := range 50 {
		allow(l, string(rune('a'+i%26))+"-"+string(rune('0'+i/26)), start)
	}
	allow(l, "busy", start)
	if got := l.size(); got != 51 {
		t.Fatalf("size = %d, want 51", got)
	}
	// "busy" keeps spending tokens; the others refill and are dropped.
	for s := 1; s <= 6; s++ {
		for range 20 {
			allow(l, "busy", start.Add(time.Duration(s)*time.Second))
		}
	}
	if got := l.size(); got != 1 {
		t.Errorf("size after refill = %d, want only the busy bucket", got)
	}
	if allow(l, "busy", start.Add(6*time.Second)) {
		t.Error("dropping idle buckets reset the busy one")
	}
}

func TestTakeBoth(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	publishers, peers := newLimiter(1, 2), newLimiter(1, 2)

	for range 2 {
		if !takeBoth(publishers, "flooder", peers, "relay", now) {
			t.Fatal("event within both bursts was denied")
		}
	}
	// The publisher's bucket is empty: the relay keeps its tokens.
	for range 5 {
		if takeBoth(publishers, "flooder", peers, "other-relay", now) {
			t.Fatal("event beyond the publisher's burst was allowed")
		}
	}
	if !takeBoth(publishers, "honest-1", peers, "other-relay", now) || !takeBoth(publishers, "honest-2", peers, "other-relay", now) {
		t.Error("a relay was charged for events dropped by the publisher limit")
	}
	// The relay's bucket is empty now: the publisher keeps its token.
	if takeBoth(publishers, "honest-3", peers, "other-relay", now) {
		t.Fatal("event beyond the relay's burst was allowed")
	}
	for range 2 {
		if !takeBoth(publishers, "honest-3", peers, "relay-3", now) {
			t.Error("a publisher was charged for an event dropped by the peer limit")
		}
	}
}

func TestLimiterSweepIntervalIsBounded(t *testing.T) {
	if l := newLimiter(1e-12, 250); l.fill != maxSweepInterval {
		t.Errorf("fill = %s, want %s", l.fill, maxSweepInterval)
	}
	if l := newLimiter(10, 50); l.fill != 5*time.Second {
		t.Errorf("fill = %s, want 5s", l.fill)
	}
}

// Concurrent validators read the clock before verifying the signature, so
// the limiter sees their times out of order; an earlier time must not
// credit the same interval twice.
func TestLimiterTimeOutOfOrder(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	l := newLimiter(10, 50)

	admitted := 0
	for ms := range 1000 {
		now := start.Add(time.Duration(ms) * time.Millisecond)
		for _, at := range []time.Time{now, now.Add(-20 * time.Millisecond)} {
			if allow(l, "a", at) {
				admitted++
			}
		}
	}
	// The burst plus 10 events per second for 1s.
	if admitted > 60 {
		t.Errorf("admitted %d events in 1s, want at most 60", admitted)
	}
}
