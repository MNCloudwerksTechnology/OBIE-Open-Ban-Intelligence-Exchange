package gossip

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// limiter is a set of token buckets, one per key, created full on first
// use. Buckets that have refilled completely are dropped: they behave
// exactly like a new one, so the set only holds recently active keys.
type limiter struct {
	limit rate.Limit
	burst int
	// fill is the time an empty bucket takes to refill, at most
	// maxSweepInterval, and how often full buckets are dropped.
	fill time.Duration

	mu        sync.Mutex
	buckets   map[string]*rate.Limiter
	lastSweep time.Time
	// latest is the latest time seen. Concurrent callers may pass their
	// times out of order, and a rate.Limiter given an earlier time than
	// its last one credits the interval between them again.
	latest time.Time
}

// maxSweepInterval bounds the sweep interval of very slow buckets.
const maxSweepInterval = time.Hour

func newLimiter(eventsPerSecond float64, burst int) *limiter {
	fill := min(float64(burst)/eventsPerSecond, maxSweepInterval.Seconds())
	return &limiter{
		limit:   rate.Limit(eventsPerSecond),
		burst:   burst,
		fill:    time.Duration(fill * float64(time.Second)),
		buckets: make(map[string]*rate.Limiter),
	}
}

// take takes a token from key's bucket at now, or at the latest time
// seen if that is later. It returns nil if the bucket is empty, else the
// reservation, which gives the token back through cancel.
func (l *limiter) take(key string, now time.Time) *rate.Reservation {
	l.mu.Lock()
	defer l.mu.Unlock()
	now = l.advance(now)
	l.sweep(now)
	b := l.buckets[key]
	if b == nil {
		b = rate.NewLimiter(l.limit, l.burst)
		l.buckets[key] = b
	}
	r := b.ReserveN(now, 1)
	if !r.OK() || r.DelayFrom(now) > 0 {
		r.CancelAt(now)
		return nil
	}
	return r
}

// cancel gives back the token of a reservation made by take.
func (l *limiter) cancel(r *rate.Reservation, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r.CancelAt(l.advance(now))
}

// advance returns now, or the latest time seen if that is later.
func (l *limiter) advance(now time.Time) time.Time {
	if now.Before(l.latest) {
		return l.latest
	}
	l.latest = now
	return now
}

// takeBoth takes a token from a's bucket for keyA and from b's bucket for
// keyB, or from neither if either is empty, and reports whether it did.
func takeBoth(a *limiter, keyA string, b *limiter, keyB string, now time.Time) bool {
	ra := a.take(keyA, now)
	if ra == nil {
		return false
	}
	if b.take(keyB, now) == nil {
		a.cancel(ra, now)
		return false
	}
	return true
}

// sweep drops the full buckets, at most once per fill time.
func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.fill {
		return
	}
	l.lastSweep = now
	for key, b := range l.buckets {
		if b.TokensAt(now) >= float64(l.burst) {
			delete(l.buckets, key)
		}
	}
}

// size returns the number of buckets held.
func (l *limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
