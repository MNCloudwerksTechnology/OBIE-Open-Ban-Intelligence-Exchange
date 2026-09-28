package gossip

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Outcome is what became of a message received on the topic.
type Outcome string

// Outcomes of a received message.
const (
	// Accepted: valid and new; stored and relayed.
	Accepted Outcome = "accepted"
	// InvalidSignature: the signature is missing, malformed or wrong.
	InvalidSignature Outcome = "invalid_signature"
	// InvalidSchema: the event violates the obie/0.1 format or field rules.
	InvalidSchema Outcome = "invalid_schema"
	// Expired: the event expired or is dated too far in the future.
	Expired Outcome = "expired"
	// Duplicate: the event is already stored.
	Duplicate Outcome = "duplicate"
	// RateLimited: the publisher or the forwarding peer exceeded its limit.
	RateLimited Outcome = "rate_limited"
	// TooLarge: the message exceeds obieproto.MaxEventSize.
	TooLarge Outcome = "too_large"
)

// Outcomes lists every Outcome, e.g. to initialize counters.
var Outcomes = []Outcome{Accepted, InvalidSignature, InvalidSchema, Expired, Duplicate, RateLimited, TooLarge}

// Metrics observes the outcome of every message received from a peer; the
// node's own publications are not observed. Observe is called concurrently
// on the validation path and must be fast. It is an observer in addition
// to the Prometheus metrics, which are always updated.
type Metrics interface {
	Observe(Outcome)
}

type nopMetrics struct{}

func (nopMetrics) Observe(Outcome) {}

// Event types as the type label of obie_events_published_total.
var eventTypes = []string{typeLabel(obieproto.TypeVerdict), typeLabel(obieproto.TypeRevoke)}

var (
	receivedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "events_received_total",
		Help:      "Events received from mesh peers, by validation outcome.",
	}, []string{"outcome"})
	publishedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "events_published_total",
		Help:      "Events this node published to the mesh, by type (verdict, revoke).",
	}, []string{"type"})
	propagationDelay = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "obie",
		Name:      "propagation_delay_seconds",
		Help:      "Time from an accepted event's issued_at to its receipt by this node; issued_at has whole-second precision.",
		Buckets:   []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
	})
)

func init() {
	for _, o := range Outcomes {
		receivedTotal.WithLabelValues(string(o))
	}
	for _, typ := range eventTypes {
		publishedTotal.WithLabelValues(typ)
	}
	prometheus.MustRegister(receivedTotal, publishedTotal, propagationDelay)
}

// typeLabel returns the type label of an event type: "indicator.verdict"
// becomes "verdict".
func typeLabel(eventType string) string {
	return strings.TrimPrefix(eventType, "indicator.")
}

// observeDelay records the delay from an accepted event's issued_at to
// now, clamped at zero for a publisher whose clock is ahead.
func observeDelay(issued, now time.Time) {
	propagationDelay.Observe(max(now.Sub(issued), 0).Seconds())
}
