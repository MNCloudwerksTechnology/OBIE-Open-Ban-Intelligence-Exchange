package gossip

import (
	"slices"
	"strings"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

func TestScoreMetricsRegistered(t *testing.T) {
	for _, c := range []prometheus.Collector{peerScoreHistogram, peersBelowThreshold, scoredPeers} {
		if err := prometheus.Register(c); err == nil {
			t.Errorf("collector was not registered")
		}
	}
	if n := len(thresholds); n != 3 {
		t.Fatalf("%d thresholds, want gossip, publish and graylist", n)
	}
	for _, th := range thresholds {
		if _, err := peersBelowThreshold.GetMetricWithLabelValues(th.Name); err != nil {
			t.Errorf("no series for threshold %s: %v", th.Name, err)
		}
	}
}

func TestPeerScoreBelow(t *testing.T) {
	for _, c := range []struct {
		score float64
		want  []string
	}{
		{10, nil}, {0, nil}, {-50, nil}, {-50.5, []string{"gossip"}}, {-100, []string{"gossip"}},
		{-150, []string{"gossip", "publish"}}, {-200.1, []string{"gossip", "publish", "graylist"}},
	} {
		s := PeerScore{Score: c.score}
		if got := s.Below(); !slices.Equal(got, c.want) {
			t.Errorf("score %v is below %v, want %v", c.score, got, c.want)
		}
	}
}

// histogramCount returns a function that reports how many observations h
// gained since it was called.
func histogramCount(t *testing.T, h prometheus.Histogram) func() uint64 {
	t.Helper()
	before, _ := histogram(t, h)
	return func() uint64 {
		n, _ := histogram(t, h)
		return n - before
	}
}

// TestScoreBoardExportsScores: every inspection replaces the scores, adds
// one observation per peer to the histogram and moves the gauges by this
// node's share; closing withdraws it.
func TestScoreBoardExportsScores(t *testing.T) {
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	board := newScoreBoard(func() time.Time { return at })
	observed := histogramCount(t, peerScoreHistogram)
	scored := delta(scoredPeers)
	below := map[string]func() float64{}
	for _, th := range thresholds {
		below[th.Name] = delta(peersBelowThreshold.WithLabelValues(th.Name))
	}
	check := func(step string, wantObserved uint64, wantScored, wantGossip, wantPublish, wantGraylist float64) {
		t.Helper()
		got := []float64{scored(), below["gossip"](), below["publish"](), below["graylist"]()}
		want := []float64{wantScored, wantGossip, wantPublish, wantGraylist}
		if n := observed(); n != wantObserved || !slices.Equal(got, want) {
			t.Errorf("after %s: %d observations, scored/gossip/publish/graylist +%v; want %d, +%v", step, n, got,
				wantObserved, want)
		}
	}
	peerC := peer.ID("peer-c")

	board.inspect(map[peer.ID]*pubsub.PeerScoreSnapshot{
		peerA: {Score: -60, IPColocationFactor: 1, BehaviourPenalty: 2, AppSpecificScore: 3,
			Topics: map[string]*pubsub.TopicScoreSnapshot{obieproto.Topic: {TimeInMesh: time.Minute,
				FirstMessageDeliveries: 4, InvalidMessageDeliveries: 5, MeshMessageDeliveries: 6}}},
		peerB: {Score: 1.5},
		peerC: {Score: -250},
	})
	check("the first inspection", 3, 3, 2, 1, 1)
	want := PeerScore{Score: -60, TimeInMesh: time.Minute, FirstMessageDeliveries: 4, InvalidMessageDeliveries: 5,
		IPColocationFactor: 1, BehaviourPenalty: 2, AppSpecificScore: 3, ReadAt: at}
	if got, ok := board.get(peerA); !ok || got != want {
		t.Errorf("score of A = %+v, %v; want %+v", got, ok, want)
	}
	if got, ok := board.get(peerB); !ok || got.Score != 1.5 || got.TimeInMesh != 0 {
		t.Errorf("score of B, not on the topic = %+v, %v", got, ok)
	}

	board.inspect(map[peer.ID]*pubsub.PeerScoreSnapshot{peerB: {Score: -120}})
	check("the second inspection", 4, 1, 1, 1, 0)
	if _, ok := board.get(peerA); ok {
		t.Error("A kept a score it no longer has")
	}

	board.close()
	check("closing", 4, 0, 0, 0, 0)
	board.inspect(map[peer.ID]*pubsub.PeerScoreSnapshot{peerA: {Score: -300}})
	board.close()
	check("an inspection after closing", 4, 0, 0, 0, 0)
	if _, ok := board.get(peerA); ok {
		t.Error("a closed board kept a score")
	}
}

// TestGossipScoresPeers: a peer that forwards three invalid messages
// falls below the gossip threshold at the next reading, which the gauge
// and PeerScore show; closing the node withdraws its share.
func TestGossipScoresPeers(t *testing.T) {
	belowGossip, belowPublish := delta(peersBelowThreshold.WithLabelValues("gossip")),
		delta(peersBelowThreshold.WithLabelValues("publish"))
	scored := delta(scoredPeers)
	a := newNode(t, func(_ *node, o *Options) { o.ScoreInspectInterval = 20 * time.Millisecond })
	raw := newUnsignedRawPublisher(t)
	connect(t, raw.host, a.host, raw.topic)
	waitFor(t, 5*time.Second, "a score of the raw peer", func() bool {
		_, ok := a.gossip.PeerScore(raw.host.ID())
		return ok
	})
	if s, _ := a.gossip.PeerScore(raw.host.ID()); s.Score < 0 || s.ReadAt.IsZero() {
		t.Errorf("score of a new peer = %+v, want at least 0, read at a time", s)
	}

	for range 3 {
		tampered := raw.verdict(t, time.Now(), 3600)
		raw.publish(t, []byte(strings.Replace(string(marshal(t, tampered)), `"events":47`, `"events":48`, 1)))
	}
	waitFor(t, 5*time.Second, "the raw peer below the gossip threshold", func() bool {
		s, ok := a.gossip.PeerScore(raw.host.ID())
		return ok && s.InvalidMessageDeliveries >= 3 && slices.Equal(s.Below(), []string{"gossip"})
	})
	if got := belowGossip(); got != 1 {
		t.Errorf("obie_gossip_peers_below_threshold{threshold=\"gossip\"} rose by %v, want 1", got)
	}
	if got := belowPublish(); got != 0 {
		t.Errorf("obie_gossip_peers_below_threshold{threshold=\"publish\"} rose by %v, want 0", got)
	}
	if got := scored(); got != 1 {
		t.Errorf("obie_gossip_scored_peers rose by %v, want 1", got)
	}

	a.gossip.Close()
	if g, s := belowGossip(), scored(); g != 0 || s != 0 {
		t.Errorf("after the node closed: below gossip +%v, scored +%v; want 0", g, s)
	}
	if _, ok := a.gossip.PeerScore(raw.host.ID()); ok {
		t.Error("a closed node reports a peer score")
	}
}
