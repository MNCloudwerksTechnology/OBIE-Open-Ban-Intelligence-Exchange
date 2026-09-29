package console

import "time"

// PeerSet is what the node knows about its peers at one moment, which the
// peers view shows (ADR 0021). The daemon reads it with cheap reads only.
type PeerSet struct {
	// Peers are the configured and the connected peers, without this node.
	Peers []Peer
	// DefaultWeight is trust.default_weight, the weight of publishers not
	// listed in trust.publishers; LocalWeight is trust.local_weight, the
	// weight of this node's own verdicts.
	DefaultWeight, LocalWeight float64
	// Verdicts counts the active verdicts the node holds, by publisher peer
	// ID; this node's own are under its peer ID.
	Verdicts map[string]VerdictCount
	// EventWindow is how far back Peer.Events count.
	EventWindow time.Duration
}

// Peer is a peer as the mesh sees it.
type Peer struct {
	ID string
	// Name is the peer's name in trust.publishers; empty if not listed.
	Name string
	// Bootstrap and Publisher are set for peers listed in mesh.bootstrap
	// and in trust.publishers.
	Bootstrap, Publisher bool
	Connected            bool
	// Addrs are the addresses of the open connections or, while the peer
	// is not connected, its mesh.bootstrap addresses.
	Addrs []string
	// ConnectedSince is when the oldest open connection was opened;
	// Latency is the smoothed ping round-trip time, 0 while unmeasured.
	ConnectedSince time.Time
	Latency        time.Duration
	// LastSeen is when a configured peer that is not connected was last
	// connected; zero if it was not since obied started.
	LastSeen time.Time
	// DialError is why the last dial of a bootstrap peer that is not
	// connected failed, and DialFailedAt when; empty if none failed.
	DialError    string
	DialFailedAt time.Time
	// Weight is the peer's trust weight in decisions: its weight in
	// trust.publishers, else trust.default_weight.
	Weight float64
	// Events counts the events the peer sent over the last
	// PeerSet.EventWindow.
	Events EventCounts
}

// EventCounts count the events a peer sent to this node, by what became of
// them.
type EventCounts struct {
	Accepted, Duplicates int
	// Rejected counts the rejected events by reason: the outcome of the
	// gossip validation, e.g. "invalid_signature".
	Rejected map[string]int
}

// VerdictCount counts the active verdicts of one publisher that the node
// holds.
type VerdictCount struct {
	// Held counts them; Counting those that count in the decisions.
	Held, Counting int
}

// VerdictPage is a page of the active verdicts of one publisher.
type VerdictPage struct {
	// Verdicts are ordered by indicator key.
	Verdicts []Verdict
	// Next is the indicator key to read the following page after; empty on
	// the last page.
	Next string
}

// Verdict is an active verdict the node holds.
type Verdict struct {
	// Key is the indicator key; Address is the address or range.
	Key, Address string
	// Action is the suggested action: ban or watch.
	Action     string
	Confidence float64
	// Reason classifies the behavior; Protocol names the attacked service.
	Reason, Protocol string
	ExpiresAt        time.Time
}
