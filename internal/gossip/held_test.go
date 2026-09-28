package gossip

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// newClockedNode is newNode with a clock ahead of time.Now by *ahead.
func newClockedNode(t *testing.T, ahead *atomic.Int64) *node {
	t.Helper()
	p := newPublisher(t)
	key, err := crypto.UnmarshalEd25519PrivateKey(p.key)
	if err != nil {
		t.Fatal(err)
	}
	n := &node{publisher: p, host: newHost(t, key), store: newStore(t), metrics: &countingMetrics{}}
	n.gossip, err = New(n.host, Options{
		Store:          n.store,
		PublisherLimit: defaultLimit.Publisher,
		PeerLimit:      defaultLimit.Peer,
		Metrics:        n.metrics,
		Now:            func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) },
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.gossip.Close)
	return n
}

// publish publishes ev on n.
func (n *node) publish(t *testing.T, ev *obieproto.Event) {
	t.Helper()
	if err := n.gossip.Publish(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
}

// revocation returns n's signed revocation of v.
func (n *node) revocation(t *testing.T, v *obieproto.Event) *obieproto.Event {
	t.Helper()
	r := &obieproto.Event{
		ID: newID(), Spec: obieproto.Spec, Type: obieproto.TypeRevoke, IssuedAt: obieproto.NewTimestamp(time.Now()),
		Indicator: v.Indicator, Revokes: v.ID, Reason: "false_positive",
		Publisher: obieproto.Publisher{PeerID: n.peerID},
	}
	n.sign(t, r)
	return r
}

// settle waits until from holds nothing any more and a fresh event sent
// after that reached to: whatever from sent before has arrived by then.
func settle(t *testing.T, from, to *node, held ...*obieproto.Event) {
	t.Helper()
	waitFor(t, propagationDeadline, "the held events to be sent", func() bool {
		for _, ev := range held {
			if from.gossip.Held(ev.ID) {
				return false
			}
		}
		return true
	})
	marker := from.verdict(t, time.Now(), 3600)
	from.publish(t, marker)
	waitFor(t, propagationDeadline, "a fresh event on the peer", func() bool { return to.has(marker.ID) })
}

// TestHeldUntilAPeerJoins: events published while no peer is on the topic
// count on the node at once, wait, and reach the first peer that joins,
// in order (ADR 0026).
func TestHeldUntilAPeerJoins(t *testing.T) {
	a := newNode(t)
	v := a.verdict(t, time.Now(), 3600)
	a.publish(t, v)
	r := a.revocation(t, v)
	a.publish(t, r)
	if !a.has(v.ID) || !a.has(r.ID) {
		t.Fatal("the held events are not in the local store")
	}
	if !a.gossip.Held(v.ID) || !a.gossip.Held(r.ID) || a.gossip.TopicPeers() != 0 {
		t.Fatalf("Held = %v, %v, TopicPeers = %d; want both held, no peer", a.gossip.Held(v.ID), a.gossip.Held(r.ID),
			a.gossip.TopicPeers())
	}

	b := newNode(t)
	connect(t, a.host, b.host, a.gossip.topic, b.gossip.topic)
	waitFor(t, propagationDeadline, "the held verdict and its revocation on the peer", func() bool {
		active, err := b.store.ActiveVerdicts(v.Key(), time.Now())
		return b.has(v.ID) && err == nil && len(active) == 0
	})
	if a.gossip.Held(v.ID) || a.gossip.Held(r.ID) || a.gossip.TopicPeers() != 1 {
		t.Errorf("after the peer joined: Held = %v, %v, TopicPeers = %d", a.gossip.Held(v.ID), a.gossip.Held(r.ID),
			a.gossip.TopicPeers())
	}

	// With a peer on the topic, an event is sent at once.
	now := a.verdict(t, time.Now(), 3600)
	a.publish(t, now)
	if a.gossip.Held(now.ID) {
		t.Error("an event published with a peer on the topic is held")
	}
	waitFor(t, propagationDeadline, "the next event on the peer", func() bool { return b.has(now.ID) })
}

// TestHeldEventsExpireAndOverflow: a held event that expires before a
// peer joins, or the oldest over the limit, is not sent; the others are.
func TestHeldEventsExpireAndOverflow(t *testing.T) {
	var ahead atomic.Int64
	a := newClockedNode(t, &ahead)
	a.gossip.heldLimit = 2
	short := a.verdict(t, time.Now(), 120)
	oldest := a.verdict(t, time.Now(), 3600)
	kept := a.verdict(t, time.Now(), 3600)
	for _, ev := range []*obieproto.Event{oldest, short, kept} {
		a.publish(t, ev)
	}
	if a.gossip.Held(oldest.ID) || !a.gossip.Held(short.ID) || !a.gossip.Held(kept.ID) {
		t.Fatalf("Held = %v, %v, %v; want the oldest dropped over the limit", a.gossip.Held(oldest.ID),
			a.gossip.Held(short.ID), a.gossip.Held(kept.ID))
	}
	// 90 s later the short verdict has 30 s left: too little to send.
	ahead.Store(int64(90 * time.Second))

	b := newNode(t)
	connect(t, a.host, b.host, a.gossip.topic, b.gossip.topic)
	settle(t, a, b, short, kept)
	if !b.has(kept.ID) {
		t.Error("the held event did not reach the peer")
	}
	if b.has(short.ID) || b.has(oldest.ID) {
		t.Errorf("the peer received the expiring (%v) or dropped (%v) event", b.has(short.ID), b.has(oldest.ID))
	}
	for _, ev := range []*obieproto.Event{oldest, short, kept} {
		if !a.has(ev.ID) {
			t.Errorf("event %s is not in the local store", ev.ID)
		}
	}
}
