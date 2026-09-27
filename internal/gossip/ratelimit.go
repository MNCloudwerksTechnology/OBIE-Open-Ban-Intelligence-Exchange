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
	// fill is the time an empty bucket takes to refill, and how often full
	// buckets are dropped.
	fill time.Duration

	mu        sync.Mutex
	buckets   map[string]*rate.Limiter
	lastSweep time.Time
}

func newLimiter(eventsPerSecond float64, burst int) *limiter {
	return &limiter{
		limit:   rate.Limit(eventsPerSecond),
		burst:   burst,
		fill:    time.Duration(float64(burst) / eventsPerSecond * float64(time.Second)),
		buckets: make(map[string]*rate.Limiter),
	}
}

// allow takes a token from key's bucket at now and reports whether there
// was one.
func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	b := l.buckets[key]
	if b == nil {
		b = rate.NewLimiter(l.limit, l.burst)
		l.buckets[key] = b
	}
	return b.AllowN(now, 1)
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
