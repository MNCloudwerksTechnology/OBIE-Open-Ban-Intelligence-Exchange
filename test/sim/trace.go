package sim

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
)

// outcome is what became of a copy of an event at a node: the outcome of
// the gossip validator, or why GossipSub dropped the copy before it.
type outcome uint8

const (
	// none marks a pair of an event and a node without any record.
	none outcome = iota
	published
	accepted
	// duplicate: the node had seen the event before (GossipSub's seen
	// cache, or the store).
	duplicate
	rateLimited
	expired
	invalidSignature
	invalidSchema
	tooLarge
	// throttled: GossipSub's validation throttle dropped the copy after it
	// remembered the copy's ID.
	throttled
	// queueFull: GossipSub's validation queue was full; it dropped the copy
	// without remembering its ID, so a later copy is validated.
	queueFull
	// blacklisted: GossipSub dropped the copy of a graylisted peer.
	blacklisted
	// relayed: an adversary got the copy.
	relayed
	other
)

var outcomeNames = [...]string{
	none: "none", published: "published", accepted: "accepted", duplicate: "duplicate",
	rateLimited: "rate_limited", expired: "expired", invalidSignature: "invalid_signature",
	invalidSchema: "invalid_schema", tooLarge: "too_large", throttled: "throttled", queueFull: "queue_full",
	blacklisted: "blacklisted", relayed: "relayed", other: "other",
}

func (o outcome) String() string { return outcomeNames[o] }

// fromGossip maps a gossip validator outcome.
func fromGossip(o gossip.Outcome) outcome {
	switch o {
	case gossip.Accepted:
		return accepted
	case gossip.Duplicate:
		return duplicate
	case gossip.RateLimited:
		return rateLimited
	case gossip.Expired:
		return expired
	case gossip.InvalidSignature:
		return invalidSignature
	case gossip.InvalidSchema:
		return invalidSchema
	case gossip.TooLarge:
		return tooLarge
	default:
		return other
	}
}

// record is one line of the per-event trace in memory: node got a copy
// of the event from the node from at the time at (since the run started)
// with the outcome.
type record struct {
	at      time.Duration
	event   int32
	from    int32
	outcome outcome
}

// Record is a line of the per-event trace in the shape of ADR 0032's
// trace files (internal/eventtrace of #1764): node, event, from, at,
// outcome.
type Record struct {
	Node    string    `json:"node"`
	Event   string    `json:"event"`
	From    string    `json:"from"`
	At      time.Time `json:"at"`
	Outcome string    `json:"outcome"`
}

// traceLog holds a node's records and its current mesh, which its tracer
// fills in from GossipSub's goroutines.
type traceLog struct {
	mu      sync.Mutex
	records []record
	// mesh are the node's current mesh peers on the topic.
	mesh map[int32]struct{}
}

func (l *traceLog) add(r record) {
	l.mu.Lock()
	l.records = append(l.records, r)
	l.mu.Unlock()
}

// meshPeers returns the node's current mesh peers.
func (l *traceLog) meshPeers() []int32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]int32, 0, len(l.mesh))
	for p := range l.mesh {
		out = append(out, p)
	}
	return out
}

func (l *traceLog) setMesh(p int32, in bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if in {
		if l.mesh == nil {
			l.mesh = map[int32]struct{}{}
		}
		l.mesh[p] = struct{}{}
		return
	}
	delete(l.mesh, p)
}

// clearMesh empties the mesh when the node goes offline.
func (l *traceLog) clearMesh() {
	l.mu.Lock()
	l.mesh = nil
	l.mu.Unlock()
}

// tracer records a node's copies of the run's events and follows its
// mesh; it is the node's gossip.Testing hooks.
type tracer struct {
	w   *world
	log *traceLog
}

func (t tracer) record(id string, from peer.ID, o outcome) {
	ev, ok := t.w.eventIdx[id]
	if !ok {
		return // junk or a forgery: not an event the run measures
	}
	t.log.add(record{at: t.w.elapsed(), event: ev, from: t.w.index(from), outcome: o})
}

// observe is the gossip.Testing.Observe hook.
func (t tracer) observe(id string, from peer.ID, o gossip.Outcome) { t.record(id, from, fromGossip(o)) }

func (t tracer) Graft(p peer.ID, _ string) { t.log.setMesh(t.w.index(p), true) }
func (t tracer) Prune(p peer.ID, _ string) { t.log.setMesh(t.w.index(p), false) }

// OnClosedOutboundStream: GossipSub forgets a peer, and its mesh slot,
// when the stream to it closes.
func (t tracer) OnClosedOutboundStream(p peer.ID) { t.log.setMesh(t.w.index(p), false) }

func (t tracer) DuplicateMessage(msg *pubsub.Message) { t.record(msg.ID, msg.ReceivedFrom, duplicate) }

// RejectMessage records the copies GossipSub drops before the validator
// runs; the validator's own verdicts come through observe.
func (t tracer) RejectMessage(msg *pubsub.Message, reason string) {
	switch reason {
	case pubsub.RejectValidationFailed, pubsub.RejectValidationIgnored:
	case pubsub.RejectValidationQueueFull:
		t.record(msg.ID, msg.ReceivedFrom, queueFull)
	case pubsub.RejectValidationThrottled:
		t.record(msg.ID, msg.ReceivedFrom, throttled)
	case pubsub.RejectBlacklstedPeer, pubsub.RejectBlacklistedSource:
		t.record(msg.ID, msg.ReceivedFrom, blacklisted)
	default:
		t.record(msg.ID, msg.ReceivedFrom, other)
	}
}

// The remaining events of pubsub.RawTracer are not measured.

func (tracer) OnNewOutboundStream(peer.ID, protocol.ID) {}
func (tracer) Join(string)                              {}
func (tracer) Leave(string)                             {}
func (tracer) ValidateMessage(*pubsub.Message)          {}
func (tracer) DeliverMessage(*pubsub.Message)           {}
func (tracer) ThrottlePeer(peer.ID)                     {}
func (tracer) RecvRPC(*pubsub.RPC)                      {}
func (tracer) SendRPC(*pubsub.RPC, peer.ID)             {}
func (tracer) DropRPC(*pubsub.RPC, peer.ID)             {}
func (tracer) UndeliverableMessage(*pubsub.Message)     {}

// delivery is what the trace says about one event at one node.
type delivery struct {
	// at is when the node accepted (or published, or as an adversary
	// relayed) the event; valid if parent >= 0.
	at time.Duration
	// parent is the node the accepted copy came from, the node itself
	// for its own event, and -1 if the node did not accept the event.
	parent int32
	// copies counts the copies the node received.
	copies int32
	// first is the outcome of the first copy GossipSub validated: the first
	// whose outcome is not provisional, or the first copy's if every
	// outcome is.
	first outcome
}

// provisional reports whether a copy's outcome leaves the loss to a later
// copy. Copies that arrive at the same instant are validated in parallel,
// so a duplicate can be recorded before the copy it duplicates; and
// GossipSub forgets a copy it dropped from a full validation queue.
func provisional(o outcome) bool { return o == duplicate || o == queueFull }

// joined is the trace of a run joined per event and node.
type joined struct {
	// d[e][n] is event e at node n.
	d [][]delivery
}

// join joins the records of every node of w.
func join(w *world) *joined {
	j := &joined{d: make([][]delivery, len(w.events))}
	for e := range j.d {
		j.d[e] = make([]delivery, len(w.nodes))
		for n := range j.d[e] {
			j.d[e][n].parent = -1
		}
	}
	for n, nd := range w.nodes {
		nd.trace.mu.Lock()
		recs := slices.Clone(nd.trace.records)
		nd.trace.mu.Unlock()
		slices.SortStableFunc(recs, func(a, b record) int { return int(a.at - b.at) })
		for _, r := range recs {
			d := &j.d[r.event][n]
			if r.outcome != published {
				d.copies++
			}
			if d.first == none || (provisional(d.first) && !provisional(r.outcome)) {
				d.first = r.outcome
			}
			if d.parent < 0 && (r.outcome == accepted || r.outcome == published || r.outcome == relayed) {
				d.at, d.parent = r.at, r.from
				if r.outcome == published {
					d.parent = int32(n) // #nosec G115 -- node counts fit in int32.
				}
			}
		}
	}
	return j
}

// hops returns the hop count of event e to every node: the length of the
// path of accepted copies back to the publisher, 0 for the publisher and
// -1 where the node did not get the event or the path is not traced.
func (j *joined) hops(e int) []int {
	d := j.d[e]
	h := make([]int, len(d))
	for n := range h {
		h[n] = -2 // not computed yet
	}
	var walk func(n int, depth int) int
	walk = func(n int, depth int) int {
		if h[n] != -2 {
			return h[n]
		}
		p := int(d[n].parent)
		switch {
		case p < 0 || depth > len(d):
			h[n] = -1
		case p == n:
			h[n] = 0
		default:
			if up := walk(p, depth+1); up < 0 {
				h[n] = -1
			} else {
				h[n] = up + 1
			}
		}
		return h[n]
	}
	for n := range h {
		walk(n, 0)
	}
	return h
}

// writeTrace writes every record of w as JSON lines in the shape of ADR
// 0032's trace files, ordered by node and time.
func writeTrace(out io.Writer, w *world) error {
	enc := json.NewEncoder(out)
	for _, nd := range w.nodes {
		nd.trace.mu.Lock()
		recs := slices.Clone(nd.trace.records)
		nd.trace.mu.Unlock()
		slices.SortStableFunc(recs, func(a, b record) int { return int(a.at - b.at) })
		for _, r := range recs {
			from := nd.pid.String()
			if r.from >= 0 {
				from = w.nodes[r.from].pid.String()
			}
			rec := Record{Node: nd.pid.String(), Event: w.events[r.event].ev.ID, From: from,
				At: w.start.Add(r.at).UTC(), Outcome: r.outcome.String()}
			if err := enc.Encode(rec); err != nil {
				return err
			}
		}
	}
	return nil
}

// traceFile writes the trace of w to the file at path.
func traceFile(path string, w *world) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.Create(path) // #nosec G304 -- the operator chooses the trace directory.
	if err != nil {
		return err
	}
	if err := writeTrace(f, w); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
