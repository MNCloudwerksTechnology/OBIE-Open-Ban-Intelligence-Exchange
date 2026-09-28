package decision

import (
	"maps"

	"github.com/prometheus/client_golang/prometheus"
)

// States lists every decision State.
var States = []State{StateBlock, StateNone, StateAllowed}

var (
	decisionsGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "decisions",
		Help:      "Decisions kept by the decision engine, by state (block, none, allowed).",
	}, []string{"state"})
	activeIndicators = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "store_active_indicators",
		Help:      "Indicators with at least one active verdict in the store.",
	})
	activeVerdicts = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "store_active_verdicts",
		Help:      "Active verdicts in the store, at most one per publisher and indicator.",
	})
)

func init() {
	for _, s := range States {
		decisionsGauge.WithLabelValues(string(s))
	}
	prometheus.MustRegister(decisionsGauge, activeIndicators, activeVerdicts)
}

// Counts are the numbers of the kept decisions after an evaluation pass.
type Counts struct {
	// Decisions counts the kept decisions by state.
	Decisions map[State]int
	// Indicators counts the indicators with at least one active verdict;
	// Verdicts counts their active verdicts.
	Indicators, Verdicts int
}

// Counts returns the numbers of the kept decisions after the last
// evaluation pass, the same the metrics show; zero before Start. Reading
// them costs nothing, however many decisions are kept.
func (e *Engine) Counts() Counts {
	e.countsMu.Lock()
	defer e.countsMu.Unlock()
	c := e.counts
	c.Decisions = maps.Clone(c.Decisions)
	return c
}

// publishMetrics counts the kept decisions for Counts and sets the gauges.
func (e *Engine) publishMetrics() {
	c := Counts{Decisions: make(map[State]int, len(States))}
	e.mu.RLock()
	for key, d := range e.decisions {
		c.Decisions[d.State]++
		if n := len(e.held[key]); n > 0 {
			c.Indicators++
			c.Verdicts += n
		}
	}
	e.mu.RUnlock()
	e.countsMu.Lock()
	e.counts = c
	e.countsMu.Unlock()
	for _, s := range States {
		decisionsGauge.WithLabelValues(string(s)).Set(float64(c.Decisions[s]))
	}
	activeIndicators.Set(float64(c.Indicators))
	activeVerdicts.Set(float64(c.Verdicts))
}
