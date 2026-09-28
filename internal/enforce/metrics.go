package enforce

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

var (
	entriesGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "obie",
		Subsystem: "enforce",
		Name:      "entries",
		Help:      "Entries applied by the enforcement backend after the last successful reconciliation.",
	})
	skippedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "obie",
		Subsystem: "enforce",
		Name:      "skipped_entries",
		Help:      "Decided blocks not applied in the last reconciliation, by reason (allowlist, max_entries).",
	}, []string{"reason"})
	enforcingGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "obie",
		Subsystem: "enforce",
		Name:      "enforcing",
		Help:      "1 while the node is in enforce mode, 0 in observe mode.",
	})
	applyFailuresTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "obie",
		Subsystem: "enforce",
		Name:      "failures_total",
		Help:      "Failed reconciliations of the enforcement backend.",
	})
)

func init() {
	for _, reason := range []string{SkipAllowlist, SkipMaxEntries} {
		skippedGauge.WithLabelValues(reason)
	}
	prometheus.MustRegister(entriesGauge, skippedGauge, enforcingGauge, applyFailuresTotal)
}

// setMetrics publishes the outcome of a successful reconciliation.
func setMetrics(mode config.Mode, applied int, skipped map[string]int) {
	enforcing := 0.0
	if mode == config.ModeEnforce {
		enforcing = 1
	}
	enforcingGauge.Set(enforcing)
	entriesGauge.Set(float64(applied))
	for _, reason := range []string{SkipAllowlist, SkipMaxEntries} {
		skippedGauge.WithLabelValues(reason).Set(float64(skipped[reason]))
	}
}
