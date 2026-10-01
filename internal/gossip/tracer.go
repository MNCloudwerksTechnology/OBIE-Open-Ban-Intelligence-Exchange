package gossip

import (
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/MNCloudwerksTechnology/obie/internal/eventtrace"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Directions of a control message, as the direction label of
// obie_gossip_ihave_total and obie_gossip_iwant_total.
const (
	directionSent     = "sent"
	directionReceived = "received"
)

// Reasons GossipSub drops a message for, as the reason label of
// obie_gossip_rejects_total (ADR 0032). Ignored messages are counted apart.
const (
	// rejectValidationFailed: OBIE's validator rejected it;
	// obie_events_received_total says why.
	rejectValidationFailed = "validation_failed"
	// rejectQueueFull and rejectThrottled: validation had no room for it.
	rejectQueueFull = "queue_full"
	rejectThrottled = "throttled"
	// rejectSignature: it carries a GossipSub signature, which the
	// StrictNoSign policy forbids (ADR 0009).
	rejectSignature = "signature"
	// rejectAuthor: it carries an author, sequence number or key.
	rejectAuthor = "author"
	// rejectBlacklisted: it came from or through a blacklisted peer.
	rejectBlacklisted = "blacklisted"
	// rejectSelfOrigin: a peer forwarded it as this node's.
	rejectSelfOrigin = "self_origin"
	// rejectOther: any reason a later go-libp2p-pubsub adds.
	rejectOther = "other"
)

// rejectReasons maps GossipSub's reject reasons to the reason label.
var rejectReasons = map[string]string{
	pubsub.RejectValidationFailed:    rejectValidationFailed,
	pubsub.RejectValidationQueueFull: rejectQueueFull,
	pubsub.RejectValidationThrottled: rejectThrottled,
	pubsub.RejectMissingSignature:    rejectSignature,
	pubsub.RejectUnexpectedSignature: rejectSignature,
	pubsub.RejectInvalidSignature:    rejectSignature,
	pubsub.RejectUnexpectedAuthInfo:  rejectAuthor,
	pubsub.RejectBlacklstedPeer:      rejectBlacklisted,
	pubsub.RejectBlacklistedSource:   rejectBlacklisted,
	pubsub.RejectSelfOrigin:          rejectSelfOrigin,
}

// rejectLabels lists every reason label of obie_gossip_rejects_total.
var rejectLabels = [...]string{rejectValidationFailed, rejectQueueFull, rejectThrottled, rejectSignature, rejectAuthor,
	rejectBlacklisted, rejectSelfOrigin, rejectOther}

// rejectReason returns the reason label of GossipSub's reason.
func rejectReason(reason string) string {
	if label, ok := rejectReasons[reason]; ok {
		return label
	}
	return rejectOther
}

// tracer is the node's GossipSub RawTracer: it counts what GossipSub does
// with the messages of the topic in the obie_gossip_* metrics, besides the
// validator's outcomes (ADR 0032). GossipSub calls it from its event loop
// and its validation goroutines, so every method is fast and safe for
// concurrent use. Messages this node published are not counted.
type tracer struct {
	self peer.ID
	// trace records the copies GossipSub drops before validation; now is
	// its clock.
	trace *eventtrace.Writer
	now   func() time.Time

	// mu guards mesh, the peers in this node's mesh of the topic, and
	// closed, set once the node left: from then on the mesh is not
	// tracked. meshPeers holds the size of every running node's mesh.
	mu     sync.Mutex
	mesh   map[peer.ID]struct{}
	closed bool
}

var _ pubsub.RawTracer = (*tracer)(nil)

// newTracer returns the tracer of the node self, which records the
// copies GossipSub drops before validation in trace (nil for none) at the
// times now returns.
func newTracer(self peer.ID, trace *eventtrace.Writer, now func() time.Time) *tracer {
	return &tracer{self: self, trace: trace, now: now, mesh: map[peer.ID]struct{}{}}
}

// Graft counts a peer added to the mesh of a topic.
func (t *tracer) Graft(p peer.ID, topic string) {
	if topic != obieproto.Topic {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.mesh[p]; ok || t.closed {
		return
	}
	t.mesh[p] = struct{}{}
	meshPeers.Inc()
	graftsTotal.Inc()
}

// Prune counts a peer removed from the mesh of a topic. GossipSub also
// reports a PRUNE from a peer that is not in the mesh, e.g. when both
// sides prune each other at once; that removes nothing and is not counted.
func (t *tracer) Prune(p peer.ID, topic string) {
	if topic != obieproto.Topic {
		return
	}
	if t.leaveMesh(p) {
		prunesTotal.Inc()
	}
}

// OnClosedOutboundStream removes peer p from the mesh: GossipSub drops a
// peer whose stream closed from its mesh without a PRUNE.
func (t *tracer) OnClosedOutboundStream(p peer.ID) { t.leaveMesh(p) }

// leaveMesh removes peer p from the mesh and reports whether it was in it.
func (t *tracer) leaveMesh(p peer.ID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.mesh[p]; !ok {
		return false
	}
	delete(t.mesh, p)
	meshPeers.Dec()
	return true
}

// close withdraws this node's mesh from obie_gossip_mesh_peers; the
// tracer counts no mesh changes after it.
func (t *tracer) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	meshPeers.Sub(float64(len(t.mesh)))
	t.mesh = nil
}

// DeliverMessage counts a message from a peer that GossipSub accepted: the
// first valid copy of an event.
func (t *tracer) DeliverMessage(msg *pubsub.Message) {
	if t.own(msg) {
		return
	}
	deliveriesTotal.Inc()
}

// DuplicateMessage counts a copy of a message this node has seen, which
// GossipSub drops before validation.
func (t *tracer) DuplicateMessage(msg *pubsub.Message) {
	if t.own(msg) {
		return
	}
	duplicatesTotal.Inc()
	t.trace.Write(idOf(msg), msg.ReceivedFrom.String(), t.now(), eventtrace.Duplicate)
}

// RejectMessage counts a message GossipSub dropped, by reason; ignored
// messages are counted apart.
func (t *tracer) RejectMessage(msg *pubsub.Message, reason string) {
	if t.own(msg) {
		return
	}
	if reason == pubsub.RejectValidationIgnored {
		ignoresTotal.Inc()
		return
	}
	label := rejectReason(reason)
	rejectsTotal.WithLabelValues(label).Inc()
	// The validator traced the outcome of what it rejected.
	if label != rejectValidationFailed {
		t.trace.Write(idOf(msg), msg.ReceivedFrom.String(), t.now(), label)
	}
}

// RecvRPC counts the message IDs a peer announced or requested.
func (t *tracer) RecvRPC(rpc *pubsub.RPC) { countControl(rpc, directionReceived) }

// SendRPC counts the message IDs this node announced or requested.
func (t *tracer) SendRPC(rpc *pubsub.RPC, _ peer.ID) { countControl(rpc, directionSent) }

// countControl counts the message IDs of the IHAVE and IWANT control
// messages of rpc in direction.
func countControl(rpc *pubsub.RPC, direction string) {
	ctl := rpc.GetControl()
	if ctl == nil {
		return
	}
	var have, want int
	for _, ih := range ctl.GetIhave() {
		have += len(ih.GetMessageIDs())
	}
	for _, iw := range ctl.GetIwant() {
		want += len(iw.GetMessageIDs())
	}
	if have > 0 {
		ihaveTotal.WithLabelValues(direction).Add(float64(have))
	}
	if want > 0 {
		iwantTotal.WithLabelValues(direction).Add(float64(want))
	}
}

// own reports whether msg is this node's own publication.
func (t *tracer) own(msg *pubsub.Message) bool {
	return msg.Local || msg.ReceivedFrom == t.self
}

// The other events are not counted.

func (*tracer) OnNewOutboundStream(peer.ID, protocol.ID) {}
func (*tracer) Join(string)                              {}
func (*tracer) Leave(string)                             {}
func (*tracer) ValidateMessage(*pubsub.Message)          {}
func (*tracer) ThrottlePeer(peer.ID)                     {}
func (*tracer) DropRPC(*pubsub.RPC, peer.ID)             {}
func (*tracer) UndeliverableMessage(*pubsub.Message)     {}
