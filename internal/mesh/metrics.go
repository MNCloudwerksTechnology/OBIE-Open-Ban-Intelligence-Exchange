package mesh

import "github.com/prometheus/client_golang/prometheus"

var (
	peersConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "peers_connected",
		Help:      "Mesh peers this node is connected to.",
	})
	peersConfigured = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "peers_configured",
		Help:      "Bootstrap peers configured in mesh.bootstrap (without this node itself).",
	})
)

func init() {
	prometheus.MustRegister(peersConnected, peersConfigured)
}
