package store

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// result is the outcome of Put.
type result int

const (
	resultAccepted result = iota
	resultDuplicate
	resultStale
	resultExpired
	resultForeignRevoke
	resultInvalidRevoke
	numResults
)

var resultNames = [numResults]string{
	resultAccepted:      "accepted",
	resultDuplicate:     "duplicate",
	resultStale:         "stale",
	resultExpired:       "expired",
	resultForeignRevoke: "foreign_revoke",
	resultInvalidRevoke: "invalid_revoke",
}

var eventsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "obie",
	Subsystem: "store",
	Name:      "events_total",
	Help:      "Events passed to the store, by outcome.",
}, []string{"result"})

func init() {
	for _, name := range resultNames {
		eventsTotal.WithLabelValues(name)
	}
	prometheus.MustRegister(eventsTotal)
}

// counters holds the per-database Put outcomes behind Stats.
type counters [numResults]atomic.Uint64

func (c *counters) add(r result) {
	c[r].Add(1)
	eventsTotal.WithLabelValues(resultNames[r]).Inc()
}

func (c *counters) stats() Stats {
	return Stats{
		Accepted:      c[resultAccepted].Load(),
		Duplicate:     c[resultDuplicate].Load(),
		Stale:         c[resultStale].Load(),
		Expired:       c[resultExpired].Load(),
		ForeignRevoke: c[resultForeignRevoke].Load(),
		InvalidRevoke: c[resultInvalidRevoke].Load(),
	}
}
