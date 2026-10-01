package sim

import (
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Variant is a router configuration of the honest nodes (ADR 0035).
type Variant struct {
	Name    string
	Summary string
	// Router replaces v0.1's GossipSub settings; nil keeps them.
	Router *gossip.Router
	// ConnManager gives the nodes v0.1's connection manager (32/128).
	ConnManager bool
	// D is the mesh degree the hop counts are compared with.
	D int
}

// Variant names.
const (
	V01   = "v0.1"
	Plain = "plain"
	Paper = "paper"
)

// variants returns the router variants by name.
func variants() map[string]Variant {
	return map[string]Variant{
		V01: {Name: V01, Summary: "OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128",
			ConnManager: true, D: pubsub.GossipSubD},
		Plain: {Name: Plain, Summary: "the paper's plain GossipSub: its mesh parameters, no peer scoring, no flood publishing, no outbound quota, gossip to D_lazy peers only, no connection manager",
			Router: &gossip.Router{Params: plainParams()}, D: 8},
		Paper: {Name: Paper, Summary: "the paper's GossipSub v1.1: its mesh parameters and score function, flood publishing, opportunistic grafting, no connection manager",
			Router: &gossip.Router{Params: paperParams(), Score: paperScore(), Thresholds: paperThresholds(),
				FloodPublish: true, OutboundQueueSize: 128}, D: 8},
	}
}

// paperParams are the mesh parameters of the paper's scenario files
// (gossipsub-hardening, 1k-attack-*.json).
func paperParams() pubsub.GossipSubParams {
	p := pubsub.DefaultGossipSubParams()
	p.D, p.Dlo, p.Dhi, p.Dscore, p.Dlazy = 8, 6, 12, 6, 12
	p.OpportunisticGraftTicks = 60
	return p
}

// plainParams are the paper's plain GossipSub (pGM): its honest nodes set
// only D, D_lo and D_hi (gossipsub-hardening, honest_vanilla.go), and the
// library of 2020 gossiped to D peers outside the mesh (v0.2.7,
// emitGossip), so D_lazy is D, 8. The mitigations go-libp2p-pubsub lets
// one switch off are off: the outbound quota and adaptive gossip; without
// peer scoring there is no opportunistic grafting either.
func plainParams() pubsub.GossipSubParams {
	p := pubsub.DefaultGossipSubParams()
	p.D, p.Dlo, p.Dhi, p.Dlazy = 8, 6, 12, 8
	p.Dout = 0
	p.GossipFactor = 0
	return p
}

// paperScore is the paper's score function (gossipsub-hardening scenario
// files): P1–P4 on the topic, no application score, no IP colocation.
func paperScore() *pubsub.PeerScoreParams {
	return &pubsub.PeerScoreParams{
		Topics: map[string]*pubsub.TopicScoreParams{
			obieproto.Topic: {
				TopicWeight: 0.25,

				TimeInMeshWeight:  0.0027,
				TimeInMeshQuantum: time.Second,
				TimeInMeshCap:     3600,

				FirstMessageDeliveriesWeight: 0.664,
				FirstMessageDeliveriesDecay:  0.9916,
				FirstMessageDeliveriesCap:    1500,

				MeshMessageDeliveriesWeight:     -0.25,
				MeshMessageDeliveriesDecay:      0.997,
				MeshMessageDeliveriesCap:        400,
				MeshMessageDeliveriesThreshold:  10,
				MeshMessageDeliveriesActivation: time.Minute,
				MeshMessageDeliveriesWindow:     5 * time.Millisecond,

				MeshFailurePenaltyWeight: -0.25,
				MeshFailurePenaltyDecay:  0.997,

				InvalidMessageDeliveriesWeight: -99,
				InvalidMessageDeliveriesDecay:  0.9994,
			},
		},
		AppSpecificScore:            func(peer.ID) float64 { return 0 },
		IPColocationFactorThreshold: 1,
		DecayInterval:               time.Second,
		DecayToZero:                 0.01,
		RetainScore:                 30 * time.Second,
	}
}

func paperThresholds() *pubsub.PeerScoreThresholds {
	return &pubsub.PeerScoreThresholds{
		GossipThreshold:   -4000,
		PublishThreshold:  -5000,
		GraylistThreshold: -10000,
	}
}
