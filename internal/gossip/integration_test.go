package gossip

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// propagationDeadline is how fast an event must reach every node of a
// three-node mesh (WP #1656).
const propagationDeadline = 5 * time.Second

// defaultLimit is the default of mesh.rate_limit.
var defaultLimit = config.Default().Mesh.RateLimit

// node is an in-process OBIE node: a host whose key also signs events.
type node struct {
	publisher
	host    host.Host
	store   *store.DB
	metrics *countingMetrics
	gossip  *Gossip
}

func newHost(t *testing.T, key crypto.PrivKey) host.Host {
	t.Helper()
	h, err := libp2p.New(libp2p.Identity(key), libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func newNode(t *testing.T) *node {
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
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.gossip.Close)
	return n
}

func (n *node) has(id string) bool {
	_, err := n.store.Get(id)
	return err == nil
}

// connect connects a and b and waits until each sees the other on the
// topic.
func connect(t *testing.T, a, b host.Host, topics ...*pubsub.Topic) {
	t.Helper()
	if err := a.Connect(context.Background(), peer.AddrInfo{ID: b.ID(), Addrs: b.Addrs()}); err != nil {
		t.Fatal(err)
	}
	for _, topic := range topics {
		waitFor(t, 5*time.Second, "topic peers", func() bool {
			peers := topic.ListPeers()
			return slices.Contains(peers, a.ID()) || slices.Contains(peers, b.ID())
		})
	}
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// rawPublisher is a GossipSub peer without OBIE validation: it publishes
// whatever bytes it is given, as an attacker would.
type rawPublisher struct {
	publisher
	host  host.Host
	topic *pubsub.Topic
}

func newRawPublisher(t *testing.T, opts ...pubsub.Option) *rawPublisher {
	t.Helper()
	p := newPublisher(t)
	key, err := crypto.UnmarshalEd25519PrivateKey(p.key)
	if err != nil {
		t.Fatal(err)
	}
	h := newHost(t, key)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ps, err := pubsub.NewGossipSub(ctx, h, opts...)
	if err != nil {
		t.Fatal(err)
	}
	topic, err := ps.Join(obieproto.Topic)
	if err != nil {
		t.Fatal(err)
	}
	return &rawPublisher{publisher: p, host: h, topic: topic}
}

// newUnsignedRawPublisher publishes like an OBIE node, without author.
func newUnsignedRawPublisher(t *testing.T) *rawPublisher {
	return newRawPublisher(t, pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign), pubsub.WithNoAuthor(),
		pubsub.WithMessageIdFn(messageID))
}

func (r *rawPublisher) publish(t *testing.T, data []byte) {
	t.Helper()
	if err := r.topic.Publish(context.Background(), data); err != nil {
		t.Fatal(err)
	}
}

// line connects the nodes in a chain, each only to its neighbors, and
// waits until events of the first node reach the last.
func line(t *testing.T, nodes ...*node) {
	t.Helper()
	for i := 1; i < len(nodes); i++ {
		connect(t, nodes[i-1].host, nodes[i].host, nodes[i-1].gossip.topic, nodes[i].gossip.topic)
	}
	warmUp(t, nodes[0], nodes[len(nodes)-1])
}

// warmUp publishes probe events on from until one reaches to. GossipSub
// relays only to the peers of a topic mesh, which forms in the heartbeat
// after the peers connected; an event published earlier may reach the
// direct peers only.
func warmUp(t *testing.T, from, to *node) {
	t.Helper()
	for range 10 {
		probe := from.verdict(t, time.Now(), 3600)
		if err := from.gossip.Publish(context.Background(), probe); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if to.has(probe.ID) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("the topic mesh did not form")
}

func TestPublishStoresLocallyWithoutPeers(t *testing.T) {
	n := newNode(t)
	ev := n.verdict(t, time.Now(), 3600)
	if err := n.gossip.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !n.has(ev.ID) {
		t.Error("published event is not in the local store")
	}
}

func TestPublishRejects(t *testing.T) {
	n := newNode(t)
	foreign := newPublisher(t).verdict(t, time.Now(), 3600)
	if err := n.gossip.Publish(context.Background(), foreign); !errors.Is(err, ErrNotLocal) {
		t.Errorf("Publish(other publisher's event) = %v, want ErrNotLocal", err)
	}
	unsigned := n.verdict(t, time.Now(), 3600)
	unsigned.Publisher.Signature = ""
	if err := n.gossip.Publish(context.Background(), unsigned); !errors.Is(err, obieproto.ErrInvalidSignature) {
		t.Errorf("Publish(unsigned) = %v, want ErrInvalidSignature", err)
	}
	expired := n.verdict(t, time.Now().Add(-time.Hour), 60)
	if err := n.gossip.Publish(context.Background(), expired); !errors.Is(err, obieproto.ErrExpired) {
		t.Errorf("Publish(expired) = %v, want ErrExpired", err)
	}
	for _, ev := range []*obieproto.Event{foreign, unsigned, expired} {
		if n.has(ev.ID) {
			t.Errorf("rejected event %s was stored", ev.ID)
		}
	}
}

// TestGossipPropagates publishes on A in the mesh A–B–C: C only hears of
// the events through B.
func TestGossipPropagates(t *testing.T) {
	a, b, c := newNode(t), newNode(t), newNode(t)
	line(t, a, b, c)

	v := a.verdict(t, time.Now(), 3600)
	start := time.Now()
	if err := a.gossip.Publish(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	waitFor(t, propagationDeadline, "the verdict on B and C", func() bool { return b.has(v.ID) && c.has(v.ID) })
	t.Logf("verdict reached B and C in %s", time.Since(start))

	r := &obieproto.Event{
		ID: newID(), Spec: obieproto.Spec, Type: obieproto.TypeRevoke, IssuedAt: obieproto.NewTimestamp(time.Now()),
		Indicator: v.Indicator, Revokes: v.ID, Reason: "false_positive",
		Publisher: obieproto.Publisher{PeerID: a.peerID},
	}
	a.sign(t, r)
	if err := a.gossip.Publish(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	waitFor(t, propagationDeadline, "the revocation on C", func() bool {
		active, err := c.store.ActiveVerdicts(v.Key(), time.Now())
		return err == nil && len(active) == 0
	})
}

// TestGossipDropsTamperedEvents injects a tampered event at A in the mesh
// raw–A–B–C: A rejects it, so B and C never see it.
func TestGossipDropsTamperedEvents(t *testing.T) {
	a, b, c := newNode(t), newNode(t), newNode(t)
	line(t, a, b, c)
	raw := newUnsignedRawPublisher(t)
	connect(t, raw.host, a.host, raw.topic)

	tampered := raw.verdict(t, time.Now(), 3600)
	raw.publish(t, []byte(strings.Replace(string(marshal(t, tampered)), `"events":47`, `"events":48`, 1)))
	// A valid event on the same path shows when the tampered one would
	// have arrived.
	valid := raw.verdict(t, time.Now(), 3600)
	raw.publish(t, marshal(t, valid))
	waitFor(t, propagationDeadline, "the valid event on C", func() bool { return c.has(valid.ID) })

	if got := a.metrics.count(InvalidSignature); got != 1 {
		t.Errorf("A: invalid_signature = %d, want 1", got)
	}
	for name, n := range map[string]*node{"A": a, "B": b, "C": c} {
		if n.has(tampered.ID) {
			t.Errorf("%s stored the tampered event", name)
		}
	}
	for name, n := range map[string]*node{"B": b, "C": c} {
		if got := n.metrics.snapshot(); len(got) != 1 || got[Accepted] == 0 {
			t.Errorf("%s outcomes = %v, want accepted events only", name, got)
		}
	}
}

// TestGossipDropsAuthoredMessages checks [ID-3]: a message with a GossipSub
// author and signature is dropped even though its event is valid.
func TestGossipDropsAuthoredMessages(t *testing.T) {
	a, b := newNode(t), newNode(t)
	line(t, a, b)
	raw := newRawPublisher(t) // GossipSub defaults: StrictSign
	connect(t, raw.host, a.host, raw.topic)

	authored := raw.verdict(t, time.Now(), 3600)
	raw.publish(t, marshal(t, authored))
	// A later event of A reaches B; the authored one must not.
	later := a.verdict(t, time.Now(), 3600)
	time.Sleep(500 * time.Millisecond)
	if err := a.gossip.Publish(context.Background(), later); err != nil {
		t.Fatal(err)
	}
	waitFor(t, propagationDeadline, "A's event on B", func() bool { return b.has(later.ID) })
	if a.has(authored.ID) || b.has(authored.ID) {
		t.Error("a message with a GossipSub author was accepted")
	}
	if got := a.metrics.snapshot(); len(got) != 0 {
		t.Errorf("A validated the authored message: %v", got)
	}
}

// TestGossipRateLimitsFlooder floods B from a raw peer in the mesh
// A–B–C–A while A keeps publishing: the flood is cut to B's rate limit,
// and A's events still reach B and C.
func TestGossipRateLimitsFlooder(t *testing.T) {
	a, b, c := newNode(t), newNode(t), newNode(t)
	line(t, a, b, c)
	connect(t, c.host, a.host, c.gossip.topic, a.gossip.topic)
	flooder := newUnsignedRawPublisher(t)
	connect(t, flooder.host, b.host, flooder.topic)

	const floodSize = 300
	acceptedBefore := b.metrics.count(Accepted)
	start := time.Now()
	flood := make([][]byte, floodSize)
	for i := range flood {
		flood[i] = marshal(t, flooder.verdict(t, time.Now(), 3600))
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, data := range flood {
			if err := flooder.topic.Publish(context.Background(), data); err != nil {
				return
			}
		}
	}()

	var legit []string
	for range 5 {
		ev := a.verdict(t, time.Now(), 3600)
		if err := a.gossip.Publish(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		legit = append(legit, ev.ID)
		time.Sleep(50 * time.Millisecond)
	}
	<-done
	waitFor(t, propagationDeadline, "A's events on B and C", func() bool {
		for _, id := range legit {
			if !b.has(id) || !c.has(id) {
				return false
			}
		}
		return true
	})

	if b.metrics.count(RateLimited) == 0 {
		t.Error("B rate-limited none of the flood")
	}
	// B admits at most the burst plus what refilled while the flood lasted.
	elapsed := time.Since(start).Seconds()
	maxAdmitted := float64(defaultLimit.Publisher.Burst) + defaultLimit.Publisher.EventsPerSecond*elapsed
	if got := float64(b.metrics.count(Accepted) - acceptedBefore - len(legit)); got > maxAdmitted {
		t.Errorf("B accepted %.0f flood events in %.1fs, want at most %.0f", got, elapsed, maxAdmitted)
	}
	t.Logf("B: %v; C: %v", b.metrics.snapshot(), c.metrics.snapshot())
}
