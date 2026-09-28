package ops

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/MNCloudwerksTechnology/obie/internal/version"
)

// buildInfo is always 1; its label carries the build version.
var buildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "obie",
	Name:      "build_info",
	Help:      "Always 1; the version label is the build version of obied.",
}, []string{"version"})

func init() {
	buildInfo.WithLabelValues(version.Version).Set(1)
	prometheus.MustRegister(buildInfo)
}
