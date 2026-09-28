package decision

import "github.com/prometheus/client_golang/prometheus"

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

// publishMetrics sets the gauges from the kept decisions.
func (e *Engine) publishMetrics() {
	counts := make(map[State]int, len(States))
	indicators, verdicts := 0, 0
	e.mu.RLock()
	for key, d := range e.decisions {
		counts[d.State]++
		if n := e.verdicts[key]; n > 0 {
			indicators++
			verdicts += n
		}
	}
	e.mu.RUnlock()
	for _, s := range States {
		decisionsGauge.WithLabelValues(string(s)).Set(float64(counts[s]))
	}
	activeIndicators.Set(float64(indicators))
	activeVerdicts.Set(float64(verdicts))
}
