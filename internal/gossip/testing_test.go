package gossip

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// observed is a message outcome seen by Testing.Observe.
type observed struct {
	id      string
	from    peer.ID
	outcome Outcome
}

// recorder collects what the Testing hooks of a node report.
type recorder struct {
	mu        sync.Mutex
	observed  []observed
	joined    []string
	delivered map[string]peer.ID
	// rejected counts the messages GossipSub rejected after validation.
	rejected int
}

func (r *recorder) observe(id string, from peer.ID, outcome Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observed = append(r.observed, observed{id: id, from: from, outcome: outcome})
}

func (r *recorder) outcomes() []observed {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.observed)
}

func (r *recorder) rejections() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rejected
}

// testTracer is the pubsub.RawTracer of a recorder.
type testTracer struct{ r *recorder }

func (t testTracer) Join(topic string) {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	t.r.joined = append(t.r.joined, topic)
}

func (t testTracer) DeliverMessage(msg *pubsub.Message) {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	if t.r.delivered == nil {
		t.r.delivered = map[string]peer.ID{}
	}
	t.r.delivered[msg.ID] = msg.ReceivedFrom
}

// RejectMessage counts the messages the validator rejected. GossipSub's
// peer score is an earlier tracer, so it has charged the forwarder by now.
func (t testTracer) RejectMessage(_ *pubsub.Message, reason string) {
	if reason != pubsub.RejectValidationFailed {
		return
	}
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	t.r.rejected++
}

// The remaining methods of pubsub.RawTracer do nothing.
func (testTracer) OnNewOutboundStream(peer.ID, protocol.ID) {}
func (testTracer) OnClosedOutboundStream(peer.ID)           {}
func (testTracer) Leave(string)                             {}
func (testTracer) Graft(peer.ID, string)                    {}
func (testTracer) Prune(peer.ID, string)                    {}
func (testTracer) ValidateMessage(*pubsub.Message)          {}
func (testTracer) DuplicateMessage(*pubsub.Message)         {}
func (testTracer) ThrottlePeer(peer.ID)                     {}
func (testTracer) RecvRPC(*pubsub.RPC)                      {}
func (testTracer) SendRPC(*pubsub.RPC, peer.ID)             {}
func (testTracer) DropRPC(*pubsub.RPC, peer.ID)             {}
func (testTracer) UndeliverableMessage(*pubsub.Message)     {}

// newTestingNode returns a node with the given Testing hooks.
func newTestingNode(t *testing.T, testing Testing) *node {
	t.Helper()
	p := newPublisher(t)
	key, err := crypto.UnmarshalEd25519PrivateKey(p.key)
	if err != nil {
		t.Fatal(err)
	}
	n := &node{publisher: p, host: newHost(t, key), store: newStore(t), metrics: &countingMetrics{}}
	n.gossip, err = New(n.host, Options{Store: n.store, Metrics: n.metrics, Testing: testing}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.gossip.Close)
	return n
}

// TestTestingHooksSeeEveryMessage: Observe gets the message ID, the
// forwarding peer and the outcome of every checked message, and the tracer
// sees the node join the topic and deliver the valid event.
func TestTestingHooksSeeEveryMessage(t *testing.T) {
	rec := &recorder{}
	b := newTestingNode(t, Testing{Tracer: testTracer{r: rec}, Observe: rec.observe})
	raw := newUnsignedRawPublisher(t)
	connect(t, raw.host, b.host, raw.topic)

	tampered := raw.verdict(t, time.Now(), 3600)
	raw.publish(t, []byte(strings.Replace(string(marshal(t, tampered)), `"events":47`, `"events":48`, 1)))
	valid := raw.verdict(t, time.Now(), 3600)
	raw.publish(t, marshal(t, valid))
	waitFor(t, propagationDeadline, "the valid event on B", func() bool { return b.has(valid.ID) })

	want := []observed{
		{id: tampered.ID, from: raw.host.ID(), outcome: InvalidSignature},
		{id: valid.ID, from: raw.host.ID(), outcome: Accepted},
	}
	waitFor(t, time.Second, "both outcomes", func() bool { return len(rec.outcomes()) == len(want) })
	// GossipSub validates in parallel workers: the outcomes need not come
	// in the order of the messages.
	byID := func(a, b observed) int { return strings.Compare(a.id, b.id) }
	got := rec.outcomes()
	slices.SortFunc(got, byID)
	slices.SortFunc(want, byID)
	if !slices.Equal(got, want) {
		t.Errorf("observed %+v, want %+v", got, want)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !slices.Contains(rec.joined, obieproto.Topic) {
		t.Errorf("tracer saw joins %v, want %s", rec.joined, obieproto.Topic)
	}
	if from, ok := rec.delivered[valid.ID]; !ok || from != raw.host.ID() {
		t.Errorf("tracer saw the valid event delivered from %v (%v), want %s", from, ok, raw.host.ID())
	}
}

// TestTestingRouterReplacesScoring: with v0.1's peer scoring, a peer that
// forwarded six invalid events is graylisted and its next valid event is
// ignored; a Router without scoring accepts it. Five invalid events score
// 5² × invalidMessageCost = −250, below graylistThreshold, so GossipSub
// may drop the sixth unseen; four score −160, so the fifth always arrives.
// The test waits for the tracer's rejections rather than the validator's
// outcomes, which come before GossipSub charges the score.
func TestTestingRouterReplacesScoring(t *testing.T) {
	for _, tc := range []struct {
		name   string
		router *Router
		accept bool
	}{
		{name: "v0.1", router: nil, accept: false},
		{name: "no scoring", router: &Router{Params: pubsub.DefaultGossipSubParams(), FloodPublish: true}, accept: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			b := newTestingNode(t, Testing{Router: tc.router, Tracer: testTracer{r: rec}})
			raw := newUnsignedRawPublisher(t)
			connect(t, raw.host, b.host, raw.topic)
			for range 6 {
				bad := raw.verdict(t, time.Now(), 3600)
				raw.publish(t, []byte(strings.Replace(string(marshal(t, bad)), `"events":47`, `"events":48`, 1)))
			}
			waitFor(t, propagationDeadline, "five invalid events", func() bool { return rec.rejections() >= 5 })

			valid := raw.verdict(t, time.Now(), 3600)
			raw.publish(t, marshal(t, valid))
			if tc.accept {
				waitFor(t, propagationDeadline, "the valid event", func() bool { return b.has(valid.ID) })
				return
			}
			time.Sleep(time.Second) // a graylisted peer's RPCs are dropped unseen
			if b.has(valid.ID) {
				t.Error("B accepted an event from a graylisted peer")
			}
		})
	}
}

func TestRouterOptions(t *testing.T) {
	if got := len(routerOptions(nil)); got != 2 {
		t.Errorf("v0.1 router options: %d, want peer scoring and flood publishing", got)
	}
	full := &Router{Params: pubsub.DefaultGossipSubParams(), Score: peerScoreParams(), Thresholds: peerScoreThresholds(),
		OutboundQueueSize: 128}
	if got := len(routerOptions(full)); got != 4 {
		t.Errorf("router options with scoring and a queue size: %d, want 4", got)
	}
	if got := len(routerOptions(&Router{Params: pubsub.DefaultGossipSubParams()})); got != 2 {
		t.Errorf("router options without scoring: %d, want parameters and flood publishing", got)
	}
}
