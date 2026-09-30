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
	"slices"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/eventtrace"
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

// maxHeld bounds the events held while no peer is on the topic; beyond
// it the oldest is dropped (ADR 0026).
const maxHeld = 10000

// heldMargin is how long a held event must still live to be sent when a
// peer joins; one about to expire is not worth a peer's validation.
const heldMargin = time.Minute

// Held events go out in batches of heldBatch every heldInterval, 8 a
// second: GossipSub drops what exceeds its per-peer queue of 32 messages,
// and a peer ignores this node's events beyond its mesh.rate_limit
// publisher bucket (10 a second, burst 50 by default), both unseen.
const (
	heldBatch    = 16
	heldInterval = 2 * time.Second
)

// MaxRPCSize bounds a GossipSub RPC, which bundles messages and control
// data, in both directions: a peer sending a larger one has its stream
// reset before anything in it is parsed, and outgoing RPCs are split to
// fit. It holds 16 events of obieproto.MaxEventSize, against GossipSub's
// default of 1 MiB (ADR 0017).
const MaxRPCSize = 16 * obieproto.MaxEventSize

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
	// ScoreInspectInterval is how often the peer scores are read; zero
	// for ScoreInspectInterval.
	ScoreInspectInterval time.Duration
	// Trace records every copy of an event received and every event
	// published (mesh.trace_path); nil for none (ADR 0032).
	Trace *eventtrace.Writer
	// AllowDocumentationRanges accepts indicators in the documentation
	// ranges (obieproto.ReceiveDocumentationRanges). Only for multi-node
	// tests; production nodes never set it.
	AllowDocumentationRanges bool
}

// Gossip is the node's participation in the GossipSub topic.
type Gossip struct {
	topic *pubsub.Topic
	self  peer.ID
	store store.Store
	now   func() time.Time
	log   *slog.Logger
	// receive are the obieproto.Receive options besides the clock.
	receive []obieproto.Option
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	// tracer counts what GossipSub does in the obie_gossip_* metrics;
	// scores keeps and exports the peer scores.
	tracer *tracer
	scores *scoreBoard
	// trace records the node's publications; nil for none.
	trace *eventtrace.Writer

	// pubMu serializes the node's publications, so that held events go
	// out before any later one, and guards the fields below.
	pubMu sync.Mutex
	// held are the node's events published while no peer was on the
	// topic, and those published after them while they wait, oldest
	// first; they are sent in batches once a peer is on the topic
	// (ADR 0026). heldLimit bounds them; heldBatch and heldInterval pace
	// them, and lastBatch is when the last batch went out. sent, expired
	// and dropped count what happened to them since they were last all
	// sent.
	held                   []heldEvent
	heldLimit, heldBatch   int
	heldInterval           time.Duration
	lastBatch              time.Time
	sent, expired, dropped int
	// wake starts sending held events at once, rather than on the next
	// tick.
	wake chan struct{}
}

// heldEvent is an event waiting for a peer, with its encoding.
type heldEvent struct {
	event *obieproto.Event
	data  []byte
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
	if opts.ScoreInspectInterval <= 0 {
		opts.ScoreInspectInterval = ScoreInspectInterval
	}
	var receive []obieproto.Option
	if opts.AllowDocumentationRanges {
		receive = append(receive, obieproto.ReceiveDocumentationRanges())
	}
	defaults := config.Default().Mesh.RateLimit
	opts.PublisherLimit = bucketOrDefault(opts.PublisherLimit, defaults.Publisher)
	opts.PeerLimit = bucketOrDefault(opts.PeerLimit, defaults.Peer)
	v := &validator{
		self:       h.ID(),
		store:      opts.Store,
		metrics:    opts.Metrics,
		trace:      opts.Trace,
		log:        log,
		now:        opts.Now,
		receive:    receive,
		publishers: newLimiter(opts.PublisherLimit.EventsPerSecond, opts.PublisherLimit.Burst),
		peers:      newLimiter(opts.PeerLimit.EventsPerSecond, opts.PeerLimit.Burst),
	}

	ctx, cancel := context.WithCancel(context.Background())
	tr, scores := newTracer(h.ID(), opts.Trace, opts.Now), newScoreBoard(opts.Now)
	topic, sub, err := join(ctx, h, v, validateQueueBursts*opts.PeerLimit.Burst,
		pubsub.WithRawTracer(tr),
		pubsub.WithPeerScoreInspect(pubsub.ExtendedPeerScoreInspectFn(scores.inspect), opts.ScoreInspectInterval))
	if err != nil {
		cancel()
		tr.close()
		scores.close()
		return nil, err
	}
	peers, err := topic.EventHandler()
	if err != nil {
		cancel()
		tr.close()
		scores.close()
		return nil, fmt.Errorf("watch the peers of %s: %w", obieproto.Topic, err)
	}
	g := &Gossip{topic: topic, self: h.ID(), store: opts.Store, now: opts.Now, log: log, receive: receive, cancel: cancel,
		tracer: tr, scores: scores, trace: opts.Trace, heldLimit: maxHeld, heldBatch: heldBatch, heldInterval: heldInterval,
		wake: make(chan struct{}, 1)}
	// Subscribing makes the node a member of the topic's mesh. Accepted
	// events are stored by the validator, so deliveries are discarded.
	g.wg.Go(func() {
		for {
			if _, err := sub.Next(ctx); err != nil {
				return
			}
		}
	})
	// A peer joining the topic starts sending what was held for want of
	// one; a tick continues, and retries a batch that failed or a join the
	// watcher missed.
	g.wg.Go(func() {
		defer peers.Cancel()
		for {
			ev, err := peers.NextPeerEvent(ctx)
			if err != nil {
				return
			}
			if ev.Type == pubsub.PeerJoin {
				g.wakeUp()
			}
		}
	})
	g.wg.Go(func() { g.sendHeld(ctx) })
	return g, nil
}

// bucketOrDefault returns b, or def if b is not a usable rate limit.
func bucketOrDefault(b, def config.TokenBucket) config.TokenBucket {
	if !(b.EventsPerSecond > 0) || b.Burst < 1 {
		return def
	}
	return b
}

// join starts GossipSub on h with the options instruments, which trace
// it, and subscribes to the topic with v as its validator, which up to
// queueSize received messages wait for.
func join(ctx context.Context, h host.Host, v *validator, queueSize int, instruments ...pubsub.Option) (*pubsub.Topic,
	*pubsub.Subscription, error) {
	ps, err := pubsub.NewGossipSub(ctx, h, append([]pubsub.Option{
		// Events carry their own signature: messages have no author,
		// sequence number or pubsub signature, and are rejected if they do.
		pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		pubsub.WithNoAuthor(),
		pubsub.WithMessageIdFn(messageID),
		pubsub.WithPeerScore(peerScoreParams(), peerScoreThresholds()),
		pubsub.WithValidateQueueSize(queueSize),
		pubsub.WithMaxMessageSize(MaxRPCSize),
		pubsub.WithMaxControlMessageSize(MaxRPCSize),
		// The node's own events go to every topic peer above the publish
		// threshold, not only to its mesh peers (GossipSub v1.1 flood
		// publishing): one peer dropping them does not lose them.
		pubsub.WithFloodPublish(true),
	}, instruments...)...)
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
// on this node even without peers. While no peer is on the topic, the
// event is held and sent once one joins, and while events are held, later
// ones wait behind them (ADR 0026). If sending fails after the event was
// stored, calling Publish again sends it.
func (g *Gossip) Publish(ctx context.Context, ev *obieproto.Event) error {
	if ev.Publisher.PeerID != g.self.String() {
		return fmt.Errorf("publish event %s by %s: %w", ev.ID, ev.Publisher.PeerID, ErrNotLocal)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	checked, err := obieproto.Receive(data, append(slices.Clip(g.receive), obieproto.WithClock(g.now))...)
	if err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	if _, err := g.store.Put(checked); err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	g.trace.Write(ev.ID, g.self.String(), g.now(), eventtrace.Published)
	g.pubMu.Lock()
	defer g.pubMu.Unlock()
	if alone := len(g.topic.ListPeers()) == 0; alone || len(g.held) > 0 {
		g.hold(checked, data, alone)
		return nil
	}
	if err := g.topic.Publish(ctx, data); err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	publishedTotal.WithLabelValues(typeLabel(ev.Type)).Inc()
	return nil
}

// hold keeps ev, encoded as data, until it can be sent — alone, no peer
// is on the topic; else it waits behind the events held already —
// dropping the oldest held event over the limit. The caller holds pubMu.
func (g *Gossip) hold(ev *obieproto.Event, data []byte, alone bool) {
	if len(g.held) >= g.heldLimit {
		if g.dropped == 0 {
			g.log.Warn("too many events wait for a peer; dropping the oldest, which still count on this node",
				"held", len(g.held))
		}
		g.held = slices.Delete(g.held, 0, 1)
		g.dropped++
	}
	g.held = append(g.held, heldEvent{event: ev, data: data})
	if alone {
		g.log.Info("no peer is on the topic; the event is held and sent when one joins", "event", ev.ID,
			"indicator", ev.Key(), "held", len(g.held))
	} else {
		g.log.Debug("the event waits behind the held events", "event", ev.ID, "held", len(g.held))
	}
	g.wakeUp()
}

// wakeUp starts sending held events at once.
func (g *Gossip) wakeUp() {
	select {
	case g.wake <- struct{}{}:
	default: // a wake-up is pending
	}
}

// sendHeld sends a batch of the held events whenever it is woken and,
// while events are held, every heldInterval, until ctx ends. With nothing
// held it only waits to be woken.
func (g *Gossip) sendHeld(ctx context.Context) {
	for {
		g.pubMu.Lock()
		waiting, interval := len(g.held) > 0, g.heldInterval
		g.pubMu.Unlock()
		var tick <-chan time.Time
		var timer *time.Timer
		if waiting {
			timer = time.NewTimer(interval)
			tick = timer.C
		}
		select {
		case <-ctx.Done():
		case <-g.wake:
		case <-tick:
		}
		if timer != nil {
			timer.Stop()
		}
		if ctx.Err() != nil {
			return
		}
		g.flush(ctx)
	}
}

// flush sends the next batch of held events, oldest first, if a peer is on
// the topic and the last batch went out at least heldInterval ago; events
// about to expire are dropped. What it could not send waits for the next
// batch.
func (g *Gossip) flush(ctx context.Context) {
	g.pubMu.Lock()
	defer g.pubMu.Unlock()
	if len(g.held) == 0 || time.Since(g.lastBatch) < g.heldInterval || len(g.topic.ListPeers()) == 0 {
		return
	}
	g.lastBatch = time.Now()
	now := g.now()
	for batch := 0; len(g.held) > 0 && batch < g.heldBatch; {
		h := g.held[0]
		if h.event.Expired(now.Add(heldMargin)) {
			g.held, g.expired = g.held[1:], g.expired+1
			continue
		}
		if err := g.topic.Publish(ctx, h.data); err != nil {
			g.log.Warn("sending the held events failed; they are tried again", "held", len(g.held), "error", err)
			return
		}
		publishedTotal.WithLabelValues(typeLabel(h.event.Type)).Inc()
		g.held, g.sent, batch = g.held[1:], g.sent+1, batch+1
	}
	if len(g.held) > 0 {
		g.log.Debug("held events sent", "sent", g.sent, "held", len(g.held))
		return
	}
	g.log.Info("a peer is on the topic; the held events were sent", "sent", g.sent, "expired", g.expired,
		"dropped", g.dropped)
	g.held, g.sent, g.expired, g.dropped = nil, 0, 0, 0
}

// Held reports whether the event with the ID id waits to be sent: for a
// peer to join the topic, or behind the events that did.
func (g *Gossip) Held(id string) bool {
	g.pubMu.Lock()
	defer g.pubMu.Unlock()
	for _, h := range g.held {
		if h.event.ID == id {
			return true
		}
	}
	return false
}

// Backlog counts the held events and estimates how long sending them all
// takes once a peer is on the topic, batch by batch.
func (g *Gossip) Backlog() (events int, wait time.Duration) {
	g.pubMu.Lock()
	defer g.pubMu.Unlock()
	events = len(g.held)
	batches := (events + g.heldBatch - 1) / g.heldBatch
	return events, time.Duration(max(batches-1, 0)) * g.heldInterval
}

// TopicPeers counts the peers on the topic: those the node's events are
// sent to.
func (g *Gossip) TopicPeers() int { return len(g.topic.ListPeers()) }

// Close stops GossipSub and waits for the subscription reader. Validations
// already running finish on their own; stop the store after the host.
func (g *Gossip) Close() {
	g.cancel()
	g.wg.Wait()
	g.tracer.close()
	g.scores.close()
}

// PeerScore returns the GossipSub score of peer id as it was last read;
// false if it had none: it was never on the topic, left more than an hour
// ago, or no reading has been taken yet (ADR 0032).
func (g *Gossip) PeerScore(id peer.ID) (PeerScore, bool) { return g.scores.get(id) }
