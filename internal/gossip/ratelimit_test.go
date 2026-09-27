package gossip

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	l := newLimiter(2, 3)

	for i := range 3 {
		if !l.allow("a", start) {
			t.Fatalf("event %d within the burst was denied", i+1)
		}
	}
	if l.allow("a", start) {
		t.Error("event beyond the burst was allowed")
	}
	if !l.allow("b", start) {
		t.Error("another key shares the exhausted bucket")
	}
	if !l.allow("a", start.Add(500*time.Millisecond)) {
		t.Error("the bucket did not refill at 2 events per second")
	}
	if l.allow("a", start.Add(500*time.Millisecond)) {
		t.Error("the bucket refilled more than one token in 500ms")
	}
}

func TestLimiterDropsFullBuckets(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	l := newLimiter(10, 50) // refills in 5s

	for i := range 50 {
		l.allow(string(rune('a'+i%26))+"-"+string(rune('0'+i/26)), start)
	}
	l.allow("busy", start)
	if got := l.size(); got != 51 {
		t.Fatalf("size = %d, want 51", got)
	}
	// "busy" keeps spending tokens; the others refill and are dropped.
	for s := 1; s <= 6; s++ {
		for range 20 {
			l.allow("busy", start.Add(time.Duration(s)*time.Second))
		}
	}
	if got := l.size(); got != 1 {
		t.Errorf("size after refill = %d, want only the busy bucket", got)
	}
	if l.allow("busy", start.Add(6*time.Second)) {
		t.Error("dropping idle buckets reset the busy one")
	}
}
