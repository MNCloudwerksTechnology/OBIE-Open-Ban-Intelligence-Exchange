package gossip

import (
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// ScoreInspectInterval is how often the peer scores are read (ADR 0032).
const ScoreInspectInterval = 10 * time.Second

// Threshold is a GossipSub peer score threshold: below it, GossipSub treats
// a peer worse.
type Threshold struct {
	// Name labels it in obie_gossip_peers_below_threshold: gossip, publish
	// or graylist.
	Name  string
	Value float64
}

// Thresholds lists the peer score thresholds, highest first: below gossip
// a peer exchanges no gossip with the node, below publish it gets none of
// the node's own events, below graylist its messages are ignored.
var Thresholds = [...]Threshold{
	{"gossip", gossipThreshold},
	{"publish", publishThreshold},
	{"graylist", graylistThreshold},
}

// PeerScore is a peer's GossipSub score as it was last read, with its
// components (ADR 0009).
type PeerScore struct {
	Score float64
	// TimeInMesh is how long the peer has been in the node's mesh of the
	// topic; zero if it is not in it.
	TimeInMesh time.Duration
	// FirstMessageDeliveries and InvalidMessageDeliveries are the decayed
	// counters of the first valid copies and of the invalid messages the
	// peer sent.
	FirstMessageDeliveries, InvalidMessageDeliveries float64
	// IPColocationFactor, BehaviourPenalty and AppSpecificScore are the
	// score's other components.
	IPColocationFactor, BehaviourPenalty, AppSpecificScore float64
	// ReadAt is when the score was read.
	ReadAt time.Time
}

// Below returns the names of the thresholds the score is below, highest
// first; none if it is above them all.
func (s *PeerScore) Below() []string {
	var out []string
	for _, th := range Thresholds {
		if s.Score < th.Value {
			out = append(out, th.Name)
		}
	}
	return out
}

// scoreBoard keeps the peer scores of the last inspection and this node's
// share of the score gauges. GossipSub calls inspect from goroutines of
// its own, so it is safe for concurrent use.
type scoreBoard struct {
	now func() time.Time

	mu     sync.Mutex
	scores map[peer.ID]PeerScore
	// scored and below are what this node added to obie_gossip_scored_peers
	// and, per threshold, to obie_gossip_peers_below_threshold; closed is
	// set once they are withdrawn.
	scored int
	below  [len(Thresholds)]int
	closed bool
}

func newScoreBoard(now func() time.Time) *scoreBoard {
	return &scoreBoard{now: now}
}

// inspect keeps the scores of an inspection and exports them: one
// observation per peer in obie_gossip_peer_score, and the peers scored and
// below each threshold in the gauges.
func (b *scoreBoard) inspect(snapshot map[peer.ID]*pubsub.PeerScoreSnapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	at := b.now()
	scores := make(map[peer.ID]PeerScore, len(snapshot))
	var below [len(Thresholds)]int
	for id, snap := range snapshot {
		s := peerScoreOf(snap, at)
		scores[id] = s
		peerScoreHistogram.Observe(s.Score)
		for i, th := range Thresholds {
			if s.Score < th.Value {
				below[i]++
			}
		}
	}
	b.scores = scores
	scoredPeers.Add(float64(len(scores) - b.scored))
	b.scored = len(scores)
	for i, th := range Thresholds {
		peersBelowThreshold.WithLabelValues(th.Name).Add(float64(below[i] - b.below[i]))
	}
	b.below = below
}

// peerScoreOf converts GossipSub's snapshot of a peer's score, read at at.
func peerScoreOf(snap *pubsub.PeerScoreSnapshot, at time.Time) PeerScore {
	s := PeerScore{Score: snap.Score, IPColocationFactor: snap.IPColocationFactor, BehaviourPenalty: snap.BehaviourPenalty,
		AppSpecificScore: snap.AppSpecificScore, ReadAt: at}
	if topic := snap.Topics[obieproto.Topic]; topic != nil {
		s.TimeInMesh = topic.TimeInMesh
		s.FirstMessageDeliveries, s.InvalidMessageDeliveries = topic.FirstMessageDeliveries, topic.InvalidMessageDeliveries
	}
	return s
}

// get returns the score of peer id at the last inspection; false if it had
// none.
func (b *scoreBoard) get(id peer.ID) (PeerScore, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.scores[id]
	return s, ok
}

// close withdraws this node's share of the gauges and keeps no more
// scores.
func (b *scoreBoard) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	scoredPeers.Sub(float64(b.scored))
	for i, th := range Thresholds {
		peersBelowThreshold.WithLabelValues(th.Name).Sub(float64(b.below[i]))
	}
	b.scores, b.scored, b.below = nil, 0, [len(Thresholds)]int{}
}
