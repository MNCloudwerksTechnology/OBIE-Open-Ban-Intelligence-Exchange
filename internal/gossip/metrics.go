package gossip

import (
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
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
var Outcomes = [...]Outcome{Accepted, InvalidSignature, InvalidSchema, Expired, Duplicate, RateLimited, TooLarge}

// Metrics observes the outcome of every message received from a peer,
// with the peer that sent it; the node's own publications are not
// observed. Observe is called concurrently on the validation path and must
// be fast. It is an observer in addition to the Prometheus metrics, which
// are always updated.
type Metrics interface {
	Observe(from peer.ID, o Outcome)
}

type nopMetrics struct{}

func (nopMetrics) Observe(peer.ID, Outcome) {}

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
		Help: "Time from an accepted event's creation to its receipt by this node: from the millisecond time its UUIDv7 " +
			"id carries if that lies within the second of its issued_at, else from issued_at.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
	})

	// What GossipSub does with the topic's messages (ADR 0032).
	deliveriesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "gossip_deliveries_total",
		Help:      "Messages from peers that GossipSub accepted and delivered: the first valid copy of each event.",
	})
	duplicatesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "gossip_duplicates_total",
		Help:      "Copies of a message this node had already seen, dropped by GossipSub before validation.",
	})
	rejectsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "gossip_rejects_total",
		Help:      "Messages from peers that GossipSub dropped, by reason; ignored ones are in obie_gossip_ignores_total.",
	}, []string{"reason"})
	ignoresTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "gossip_ignores_total",
		Help:      "Messages from peers that the validator ignored: duplicates known to the store, rate-limited or slightly expired events.",
	})
	graftsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "gossip_grafts_total",
		Help:      "Peers added to this node's mesh of the topic (GRAFT).",
	})
	prunesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "gossip_prunes_total",
		Help:      "Peers removed from this node's mesh of the topic by a PRUNE; a disconnect removes one without.",
	})
	ihaveTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "gossip_ihave_total",
		Help:      "Message IDs announced in IHAVE gossip, by direction (sent, received).",
	}, []string{"direction"})
	iwantTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "obie",
		Name:      "gossip_iwant_total",
		Help:      "Message IDs requested in IWANT, by direction (sent, received).",
	}, []string{"direction"})
	meshPeers = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "gossip_mesh_peers",
		Help:      "Peers in this node's mesh of the topic, to which it relays every event in full.",
	})

	// The GossipSub peer scores (ADR 0032).
	peerScoreHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "obie",
		Name:      "gossip_peer_score",
		Help:      "GossipSub peer scores: one observation per scored peer every 10 seconds.",
		Buckets:   []float64{graylistThreshold, publishThreshold, gossipThreshold, -10, -1, 0, 1, 5, 10},
	})
	peersBelowThreshold = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "gossip_peers_below_threshold",
		Help:      "Peers whose GossipSub score was below a threshold (gossip, publish, graylist) at the last reading.",
	}, []string{"threshold"})
	scoredPeers = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "obie",
		Name:      "gossip_scored_peers",
		Help:      "Peers with a GossipSub score at the last reading: the connected ones and those that left within the hour.",
	})
)

func init() {
	for _, o := range Outcomes {
		receivedTotal.WithLabelValues(string(o))
	}
	for _, typ := range eventTypes {
		publishedTotal.WithLabelValues(typ)
	}
	for _, r := range rejectLabels {
		rejectsTotal.WithLabelValues(r)
	}
	for _, d := range []string{directionSent, directionReceived} {
		ihaveTotal.WithLabelValues(d)
		iwantTotal.WithLabelValues(d)
	}
	for _, th := range thresholds {
		peersBelowThreshold.WithLabelValues(th.Name)
	}
	prometheus.MustRegister(receivedTotal, publishedTotal, propagationDelay,
		deliveriesTotal, duplicatesTotal, rejectsTotal, ignoresTotal, graftsTotal, prunesTotal, ihaveTotal, iwantTotal,
		meshPeers, peerScoreHistogram, peersBelowThreshold, scoredPeers)
}

// typeLabel returns the type label of an event type: "indicator.verdict"
// becomes "verdict".
func typeLabel(eventType string) string {
	return strings.TrimPrefix(eventType, "indicator.")
}

// observeDelay records the delay from an accepted event's creation to
// now, clamped at zero for a publisher whose clock is ahead.
func observeDelay(created, now time.Time) {
	propagationDelay.Observe(max(now.Sub(created), 0).Seconds())
}

// createdAt returns when ev was created: the millisecond time its UUIDv7
// id carries if that lies within the second its issued_at names, else its
// issued_at, which obie/0.1 gives in whole seconds (ADR 0032).
func createdAt(ev *obieproto.Event) time.Time {
	issued := ev.IssuedAt.Time
	if t, ok := obieproto.IDTime(ev.ID); ok && !t.Before(issued) && t.Before(issued.Add(time.Second)) {
		return t
	}
	return issued
}
