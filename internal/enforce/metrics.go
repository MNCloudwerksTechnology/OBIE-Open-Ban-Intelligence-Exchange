package enforce

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// Address families as the family label of obie_enforcer_entries.
const (
	familyIPv4 = "ipv4"
	familyIPv6 = "ipv6"
)

// Results as the result label of obie_enforcer_apply_total.
const (
	resultSuccess = "success"
	resultError   = "error"
)

var (
	nodeMode = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "node_mode",
		Help:      "1 for the current node.mode (observe or enforce), 0 for the other.",
	}, []string{"mode"})
	entriesGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "enforcer_entries",
		Help:      "Entries the enforcement backend applies after the last successful reconciliation, by address family.",
	}, []string{"family"})
	applyTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "enforcer_apply_total",
		Help:      "Reconciliations of the enforcement backend, by result (success, error).",
	}, []string{"result"})
	applyDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "obie",
		Name:      "enforcer_apply_duration_seconds",
		Help:      "Duration of the backend calls that apply a change (additions and removals).",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})
	skippedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "enforcer_skipped_total",
		Help:      "Decided blocks newly left out of the enforcement backend, by reason (allowlist, max_entries).",
	}, []string{"reason"})
)

func init() {
	for _, mode := range []config.Mode{config.ModeObserve, config.ModeEnforce} {
		nodeMode.WithLabelValues(string(mode))
	}
	for _, family := range []string{familyIPv4, familyIPv6} {
		entriesGauge.WithLabelValues(family)
	}
	for _, result := range []string{resultSuccess, resultError} {
		applyTotal.WithLabelValues(result)
	}
	for _, reason := range []string{SkipAllowlist, SkipMaxEntries} {
		skippedTotal.WithLabelValues(reason)
	}
	prometheus.MustRegister(nodeMode, entriesGauge, applyTotal, applyDuration, skippedTotal)
}

// setModeMetric publishes the current node.mode.
func setModeMetric(mode config.Mode) {
	for _, m := range []config.Mode{config.ModeObserve, config.ModeEnforce} {
		v := 0.0
		if m == mode {
			v = 1
		}
		nodeMode.WithLabelValues(string(m)).Set(v)
	}
}

// setEntriesMetric publishes the applied entries by address family.
func setEntriesMetric(entries []Entry) {
	var v4, v6 int
	for _, e := range entries {
		if e.Prefix.Addr().Is4() {
			v4++
		} else {
			v6++
		}
	}
	entriesGauge.WithLabelValues(familyIPv4).Set(float64(v4))
	entriesGauge.WithLabelValues(familyIPv6).Set(float64(v6))
}

// observeApply records the duration of a backend call that applied a
// change, started at start.
func observeApply(start time.Time) {
	applyDuration.Observe(time.Since(start).Seconds())
}
