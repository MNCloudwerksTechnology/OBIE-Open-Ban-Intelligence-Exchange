package gossip

import (
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Peer score thresholds. A peer that forwarded three invalid messages
// (score -90) no longer exchanges gossip with the node, one that forwarded
// four (-160) gets none of its publications, and one that forwarded five
// (-250) is ignored altogether until its score decays.
const (
	gossipThreshold    = -50
	publishThreshold   = -100
	graylistThreshold  = -200
	invalidMessageCost = -10
)

// peerScoreParams are conservative GossipSub v1.1 score parameters: the
// score is driven by invalid messages (P4), which are squared and decay
// within an hour. Verdicts are sparse, so a peer is never penalized for
// delivering few messages (P3, P3b off); rewards for time in the mesh and
// first deliveries are small and capped.
func peerScoreParams() *pubsub.PeerScoreParams {
	return &pubsub.PeerScoreParams{
		Topics: map[string]*pubsub.TopicScoreParams{
			obieproto.Topic: {
				SkipAtomicValidation: true, // leaves P3 and P3b at zero, i.e. off
				TopicWeight:          1,

				TimeInMeshWeight:  0.01,
				TimeInMeshQuantum: time.Second,
				TimeInMeshCap:     300,

				FirstMessageDeliveriesWeight: 1,
				FirstMessageDeliveriesDecay:  pubsub.ScoreParameterDecay(time.Hour),
				FirstMessageDeliveriesCap:    5,

				InvalidMessageDeliveriesWeight: invalidMessageCost,
				InvalidMessageDeliveriesDecay:  pubsub.ScoreParameterDecay(time.Hour),
			},
		},
		TopicScoreCap: 10,

		AppSpecificScore:  func(peer.ID) float64 { return 0 },
		AppSpecificWeight: 1,

		IPColocationFactorWeight:    -1,
		IPColocationFactorThreshold: 10,

		BehaviourPenaltyWeight:    -1,
		BehaviourPenaltyThreshold: 6,
		BehaviourPenaltyDecay:     pubsub.ScoreParameterDecay(10 * time.Minute),

		DecayInterval: pubsub.DefaultDecayInterval,
		DecayToZero:   pubsub.DefaultDecayToZero,
		RetainScore:   time.Hour,
	}
}

func peerScoreThresholds() *pubsub.PeerScoreThresholds {
	return &pubsub.PeerScoreThresholds{
		GossipThreshold:   gossipThreshold,
		PublishThreshold:  publishThreshold,
		GraylistThreshold: graylistThreshold,
		// Peer exchange is not used: peers come from mesh.bootstrap only.
		AcceptPXThreshold:           100,
		OpportunisticGraftThreshold: 3,
	}
}
