package gossip

import (
	"context"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

func TestTracerMetricsRegistered(t *testing.T) {
	for _, c := range []prometheus.Collector{deliveriesTotal, duplicatesTotal, rejectsTotal, ignoresTotal, graftsTotal,
		prunesTotal, ihaveTotal, iwantTotal, meshPeers} {
		if err := prometheus.Register(c); err == nil {
			t.Errorf("collector was not registered")
		}
	}
	if n := testutil.CollectAndCount(rejectsTotal, "obie_gossip_rejects_total"); n != len(RejectReasons) {
		t.Errorf("obie_gossip_rejects_total has %d series, want one per reason (%d)", n, len(RejectReasons))
	}
	for name, c := range map[string]prometheus.Collector{"obie_gossip_ihave_total": ihaveTotal, "obie_gossip_iwant_total": iwantTotal} {
		if n := testutil.CollectAndCount(c, name); n != 2 {
			t.Errorf("%s has %d series, want sent and received", name, n)
		}
	}
}

// fromPeer returns a message of the topic received from p.
func fromPeer(p peer.ID) *pubsub.Message {
	topic := obieproto.Topic
	return &pubsub.Message{Message: &pb.Message{Data: []byte(`{}`), Topic: &topic}, ReceivedFrom: p}
}

// delta returns a function that reports how much c rose since delta was
// called.
func delta(c prometheus.Collector) func() float64 {
	before := testutil.ToFloat64(c)
	return func() float64 { return testutil.ToFloat64(c) - before }
}

// TestTracerCountsMessages: deliveries, duplicates, rejects and ignores
// of messages from peers are counted; the node's own are not.
func TestTracerCountsMessages(t *testing.T) {
	self := peer.ID("self")
	tr := newTracer(self)
	own := fromPeer(self)
	local := fromPeer(peerA)
	local.Local = true

	deliveries, duplicates := delta(deliveriesTotal), delta(duplicatesTotal)
	tr.DeliverMessage(fromPeer(peerA))
	tr.DeliverMessage(own)
	tr.DeliverMessage(local)
	tr.DuplicateMessage(fromPeer(peerA))
	tr.DuplicateMessage(fromPeer(peerB))
	tr.DuplicateMessage(own)
	if got := deliveries(); got != 1 {
		t.Errorf("obie_gossip_deliveries_total rose by %v, want 1", got)
	}
	if got := duplicates(); got != 2 {
		t.Errorf("obie_gossip_duplicates_total rose by %v, want 2", got)
	}

	ignores := delta(ignoresTotal)
	tr.RejectMessage(fromPeer(peerA), pubsub.RejectValidationIgnored)
	tr.RejectMessage(own, pubsub.RejectValidationIgnored)
	if got := ignores(); got != 1 {
		t.Errorf("obie_gossip_ignores_total rose by %v, want 1", got)
	}

	for _, c := range []struct{ reason, label string }{
		{pubsub.RejectValidationFailed, "validation_failed"},
		{pubsub.RejectValidationQueueFull, "queue_full"},
		{pubsub.RejectValidationThrottled, "throttled"},
		{pubsub.RejectMissingSignature, "signature"},
		{pubsub.RejectUnexpectedSignature, "signature"},
		{pubsub.RejectInvalidSignature, "signature"},
		{pubsub.RejectUnexpectedAuthInfo, "author"},
		{pubsub.RejectBlacklstedPeer, "blacklisted"},
		{pubsub.RejectBlacklistedSource, "blacklisted"},
		{pubsub.RejectSelfOrigin, "self_origin"},
		{"a reason of a later release", "other"},
	} {
		rejected := delta(rejectsTotal.WithLabelValues(c.label))
		tr.RejectMessage(fromPeer(peerA), c.reason)
		tr.RejectMessage(own, c.reason)
		if got := rejected(); got != 1 {
			t.Errorf("reject %q: obie_gossip_rejects_total{reason=%q} rose by %v, want 1", c.reason, c.label, got)
		}
	}
}

// TestTracerTracksMesh: grafts and prunes of the topic are counted, and
// the mesh gauge follows its members, also when a peer disconnects
// without a PRUNE and when the node leaves.
func TestTracerTracksMesh(t *testing.T) {
	tr := newTracer("self")
	grafts, prunes, mesh := delta(graftsTotal), delta(prunesTotal), delta(meshPeers)
	check := func(step string, wantGrafts, wantPrunes, wantMesh float64) {
		t.Helper()
		if g, p, m := grafts(), prunes(), mesh(); g != wantGrafts || p != wantPrunes || m != wantMesh {
			t.Errorf("after %s: grafts +%v, prunes +%v, mesh +%v; want +%v, +%v, +%v", step, g, p, m, wantGrafts,
				wantPrunes, wantMesh)
		}
	}

	tr.Graft(peerA, obieproto.Topic)
	tr.Graft(peerA, obieproto.Topic)
	check("grafting A twice", 2, 0, 1)
	tr.Graft(peerB, "another/topic")
	tr.Prune(peerA, "another/topic")
	check("changes of another topic", 2, 0, 1)
	tr.Graft(peerB, obieproto.Topic)
	check("grafting B", 3, 0, 2)
	tr.Prune(peerA, obieproto.Topic)
	check("pruning A", 3, 1, 1)
	tr.Prune(peerA, obieproto.Topic)
	check("pruning A again", 3, 2, 1)
	tr.OnClosedOutboundStream(peerB)
	check("B disconnecting", 3, 2, 0)
	tr.Graft(peerA, obieproto.Topic)
	tr.Graft(peerB, obieproto.Topic)
	check("grafting A and B again", 5, 2, 2)
	tr.close()
	check("leaving", 5, 2, 0)
	tr.Graft(peerA, obieproto.Topic)
	tr.OnClosedOutboundStream(peerB)
	tr.close()
	check("changes after leaving", 6, 2, 0)
}

// TestTracerCountsControl: the message IDs of IHAVE and IWANT are counted
// by direction.
func TestTracerCountsControl(t *testing.T) {
	tr := newTracer("self")
	topic := obieproto.Topic
	rpc := &pubsub.RPC{RPC: pb.RPC{Control: &pb.ControlMessage{
		Ihave: []*pb.ControlIHave{{TopicID: &topic, MessageIDs: []string{"a", "b"}}, {TopicID: &topic, MessageIDs: []string{"c"}}},
		Iwant: []*pb.ControlIWant{{MessageIDs: []string{"d", "e"}}},
		Graft: []*pb.ControlGraft{{TopicID: &topic}},
	}}}
	haveSent, haveReceived := delta(ihaveTotal.WithLabelValues("sent")), delta(ihaveTotal.WithLabelValues("received"))
	wantSent, wantReceived := delta(iwantTotal.WithLabelValues("sent")), delta(iwantTotal.WithLabelValues("received"))

	tr.SendRPC(rpc, peerA)
	tr.RecvRPC(rpc)
	tr.RecvRPC(rpc)
	tr.SendRPC(&pubsub.RPC{}, peerA)
	tr.RecvRPC(&pubsub.RPC{RPC: pb.RPC{Control: &pb.ControlMessage{}}})
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"ihave sent", haveSent(), 3}, {"ihave received", haveReceived(), 6},
		{"iwant sent", wantSent(), 2}, {"iwant received", wantReceived(), 4},
	} {
		if c.got != c.want {
			t.Errorf("%s rose by %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestGossipIsTraced runs three fully connected nodes: each grafts the
// other two into its mesh, an event is delivered to the two that did not
// publish it and each also gets a copy from the other, and the mesh
// shrinks as a node leaves.
func TestGossipIsTraced(t *testing.T) {
	grafts, mesh := delta(graftsTotal), delta(meshPeers)
	a, b, c := newNode(t), newNode(t), newNode(t)
	connect(t, a.host, b.host, a.gossip.topic, b.gossip.topic)
	connect(t, b.host, c.host, b.gossip.topic, c.gossip.topic)
	connect(t, a.host, c.host, a.gossip.topic, c.gossip.topic)
	waitFor(t, 5*time.Second, "every node's mesh to hold the other two", func() bool { return mesh() == 6 })
	if got := grafts(); got < 6 {
		t.Errorf("obie_gossip_grafts_total rose by %v, want at least 6", got)
	}

	deliveries, duplicates := delta(deliveriesTotal), delta(duplicatesTotal)
	v := a.verdict(t, time.Now(), 3600)
	if err := a.gossip.Publish(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	waitFor(t, propagationDeadline, "the verdict on B and C", func() bool { return b.has(v.ID) && c.has(v.ID) })
	waitFor(t, propagationDeadline, "two deliveries and two duplicates", func() bool {
		return deliveries() == 2 && duplicates() == 2
	})

	c.gossip.Close()
	if err := c.host.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "only A and B in each other's mesh", func() bool { return mesh() == 2 })
}
