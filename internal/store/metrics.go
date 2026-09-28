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
	resultFull
	numResults
)

var resultNames = [numResults]string{
	resultAccepted:      "accepted",
	resultDuplicate:     "duplicate",
	resultStale:         "stale",
	resultExpired:       "expired",
	resultForeignRevoke: "foreign_revoke",
	resultInvalidRevoke: "invalid_revoke",
	resultFull:          "full",
}

var eventsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "obie",
	Subsystem: "store",
	Name:      "events_total",
	Help:      "Events passed to the store, by outcome.",
}, []string{"result"})

var evictionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Namespace: "obie",
	Subsystem: "store",
	Name:      "evictions_total",
	Help:      "Stored verdicts evicted because the store reached store.max_indicators.",
})

var verdictsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: "obie",
	Subsystem: "store",
	Name:      "verdict_records",
	Help:      "Verdicts held by the store (one per publisher and indicator; active, revoked or expired but not yet swept), bounded by store.max_indicators.",
})

var endedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "obie",
	Subsystem: "store",
	Name:      "ended_verdicts",
	Help:      "Verdicts the store keeps for a day after they ended, by how they ended; of other publishers at most a tenth of store.max_indicators each.",
}, []string{"state"})

func init() {
	for _, name := range resultNames {
		eventsTotal.WithLabelValues(name)
	}
	for _, state := range EndedStates {
		endedGauge.WithLabelValues(string(state))
	}
	prometheus.MustRegister(eventsTotal, evictionsTotal, verdictsGauge, endedGauge)
}

// counters holds the per-database Put outcomes behind Stats.
type counters struct {
	results [numResults]atomic.Uint64
	evicted atomic.Uint64
}

func (c *counters) add(r result) {
	c.results[r].Add(1)
	eventsTotal.WithLabelValues(resultNames[r]).Inc()
}

// evict counts n evicted verdicts.
func (c *counters) evict(n int) {
	if n > 0 {
		c.evicted.Add(uint64(n))
		evictionsTotal.Add(float64(n))
	}
}

func (c *counters) stats() Stats {
	r := &c.results
	return Stats{
		Accepted:      r[resultAccepted].Load(),
		Duplicate:     r[resultDuplicate].Load(),
		Stale:         r[resultStale].Load(),
		Expired:       r[resultExpired].Load(),
		ForeignRevoke: r[resultForeignRevoke].Load(),
		InvalidRevoke: r[resultInvalidRevoke].Load(),
		Full:          r[resultFull].Load(),
		Evicted:       c.evicted.Load(),
	}
}
