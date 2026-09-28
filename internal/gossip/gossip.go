// Package gossip spreads obie/0.1 events over the libp2p mesh with
// GossipSub on the topic obieproto.Topic. Every message is checked before
// it is stored or relayed: invalid events are rejected (and their
// forwarder penalized through GossipSub peer scoring), duplicates and
// events beyond the per-publisher and per-peer rate limits are ignored
// (ADR 0009).
package gossip

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// ErrNotLocal is returned by Publish for an event that another node
// published: a node only publishes its own events.
var ErrNotLocal = errors.New("event was not published by this node")

// validateQueueBursts is how many full peer bursts (mesh.rate_limit.peer)
// the GossipSub validation queue holds. Messages waiting in the queue are
// not yet charged to any rate limit, and a full queue drops every peer's
// messages alike: GossipSub's default of 32 would let a single flooding
// neighbor crowd out the events of honest ones before the limits see it.
const validateQueueBursts = 4

// Options configures a Gossip.
type Options struct {
	// Store receives every accepted event and answers the duplicate check.
	Store store.Store
	// PublisherLimit and PeerLimit bound the events accepted per publisher
	// and per forwarding peer (mesh.rate_limit); unusable (e.g. zero)
	// buckets take the defaults of config.Default.
	PublisherLimit, PeerLimit config.TokenBucket
	// Metrics observes the outcome of every received message besides the
	// Prometheus metrics; nil for none.
	Metrics Metrics
	// Now is the clock events are checked against; nil for time.Now.
	Now func() time.Time
}

// Gossip is the node's participation in the GossipSub topic.
type Gossip struct {
	topic  *pubsub.Topic
	self   peer.ID
	store  store.Store
	now    func() time.Time
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New joins the topic on h, which must be listening, and starts relaying.
// Close leaves it again; closing h is the caller's job.
func New(h host.Host, opts Options, log *slog.Logger) (*Gossip, error) {
	if opts.Store == nil {
		return nil, errors.New("gossip: no store")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Metrics == nil {
		opts.Metrics = nopMetrics{}
	}
	defaults := config.Default().Mesh.RateLimit
	opts.PublisherLimit = bucketOrDefault(opts.PublisherLimit, defaults.Publisher)
	opts.PeerLimit = bucketOrDefault(opts.PeerLimit, defaults.Peer)
	v := &validator{
		self:       h.ID(),
		store:      opts.Store,
		metrics:    opts.Metrics,
		log:        log,
		now:        opts.Now,
		publishers: newLimiter(opts.PublisherLimit.EventsPerSecond, opts.PublisherLimit.Burst),
		peers:      newLimiter(opts.PeerLimit.EventsPerSecond, opts.PeerLimit.Burst),
	}

	ctx, cancel := context.WithCancel(context.Background())
	topic, sub, err := join(ctx, h, v, validateQueueBursts*opts.PeerLimit.Burst)
	if err != nil {
		cancel()
		return nil, err
	}
	g := &Gossip{topic: topic, self: h.ID(), store: opts.Store, now: opts.Now, cancel: cancel}
	// Subscribing makes the node a member of the topic's mesh. Accepted
	// events are stored by the validator, so deliveries are discarded.
	g.wg.Go(func() {
		for {
			if _, err := sub.Next(ctx); err != nil {
				return
			}
		}
	})
	return g, nil
}

// bucketOrDefault returns b, or def if b is not a usable rate limit.
func bucketOrDefault(b, def config.TokenBucket) config.TokenBucket {
	if !(b.EventsPerSecond > 0) || b.Burst < 1 {
		return def
	}
	return b
}

// join starts GossipSub on h and subscribes to the topic with v as its
// validator, which up to queueSize received messages wait for.
func join(ctx context.Context, h host.Host, v *validator, queueSize int) (*pubsub.Topic, *pubsub.Subscription, error) {
	ps, err := pubsub.NewGossipSub(ctx, h,
		// Events carry their own signature: messages have no author,
		// sequence number or pubsub signature, and are rejected if they do.
		pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		pubsub.WithNoAuthor(),
		pubsub.WithMessageIdFn(messageID),
		pubsub.WithPeerScore(peerScoreParams(), peerScoreThresholds()),
		pubsub.WithValidateQueueSize(queueSize),
		// The node's own events go to every topic peer above the publish
		// threshold, not only to its mesh peers (GossipSub v1.1 flood
		// publishing): one peer dropping them does not lose them.
		pubsub.WithFloodPublish(true),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start gossipsub: %w", err)
	}
	if err := ps.RegisterTopicValidator(obieproto.Topic, v.validate); err != nil {
		return nil, nil, fmt.Errorf("register validator: %w", err)
	}
	topic, err := ps.Join(obieproto.Topic)
	if err != nil {
		return nil, nil, fmt.Errorf("join %s: %w", obieproto.Topic, err)
	}
	sub, err := topic.Subscribe()
	if err != nil {
		return nil, nil, fmt.Errorf("subscribe to %s: %w", obieproto.Topic, err)
	}
	return topic, sub, nil
}

// messageID is the GossipSub message ID: the event ID ([TRN-3]). Data
// without one cannot be a valid event; its hash keeps such messages apart.
func messageID(msg *pb.Message) string {
	data := msg.GetData()
	if len(data) <= obieproto.MaxEventSize {
		var ev struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(data, &ev) == nil && ev.ID != "" {
			return ev.ID
		}
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Publish sends ev, an event signed by this node, to the mesh. The event is
// checked like a received one and stored locally first, so it takes effect
// on this node even without peers. If sending fails after the event was
// stored, calling Publish again sends it.
func (g *Gossip) Publish(ctx context.Context, ev *obieproto.Event) error {
	if ev.Publisher.PeerID != g.self.String() {
		return fmt.Errorf("publish event %s by %s: %w", ev.ID, ev.Publisher.PeerID, ErrNotLocal)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	checked, err := obieproto.Receive(data, obieproto.WithClock(g.now))
	if err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	if _, err := g.store.Put(checked); err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	if err := g.topic.Publish(ctx, data); err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	publishedTotal.WithLabelValues(typeLabel(ev.Type)).Inc()
	return nil
}

// Close stops GossipSub and waits for the subscription reader. Validations
// already running finish on their own; stop the store after the host.
func (g *Gossip) Close() {
	g.cancel()
	g.wg.Wait()
}
