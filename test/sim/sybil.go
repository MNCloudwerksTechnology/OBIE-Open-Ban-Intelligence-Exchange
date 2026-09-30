package sim

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"sync"
	"time"

	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-msgio/pbio"

	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// meshsub is the GossipSub protocol the adversaries speak; honest nodes
// offer it among newer ones.
const meshsub = protocol.ID("/meshsub/1.1.0")

// The paper's attacker re-GRAFTs a peer that pruned it after
// regraftBackoff plus a random part of regraftDelay, to evade the
// hardening backoff (gossipsub-hardening, badboy.go); it remembers
// messages for seenTTL. It redials a peer that disconnected it after
// redialDelay: the paper's testbed never disconnected anyone, v0.1's
// connection manager does.
const (
	regraftBackoff = time.Minute
	regraftDelay   = 15 * time.Second
	seenTTL        = 2 * time.Minute
	redialDelay    = 10 * time.Second
)

// sybil is an adversary: a minimal GossipSub speaker ported from the
// paper's attacker. It subscribes to and GRAFTs every peer that announces
// the topic, re-GRAFTs after a PRUNE, forwards every new message to its
// mesh peers until its attack starts and drops everything after (ADR
// 0033). A preempter also answers a chosen event with a forgery of the
// same ID; a flooder injects junk events.
type sybil struct {
	w    *world
	n    *node
	h    host.Host
	ctx  context.Context
	stop context.CancelFunc

	// attackAt is when the sybil stops forwarding, since the run started;
	// 0 attacks from the start, a negative value never.
	attackAt time.Duration
	// preempt forges the chosen events.
	preempt bool

	mu   sync.Mutex
	out  map[peer.ID]*sybilOut
	mesh map[peer.ID]bool
	seen map[string]time.Duration
	// forged are the chosen events already answered.
	forged map[string]bool
	// timers are the pending regrafts and redials.
	timers map[*time.Timer]struct{}
}

// sybilOut is the stream to a peer and a writer that serializes writes.
type sybilOut struct {
	mu sync.Mutex
	s  network.Stream
	w  pbio.WriteCloser
}

func (o *sybilOut) send(rpc *pb.RPC) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.w.WriteMsg(rpc) == nil
}

func newSybil(w *world, n *node, h host.Host, attackAt time.Duration) *sybil {
	ctx, stop := context.WithCancel(context.Background())
	s := &sybil{w: w, n: n, h: h, ctx: ctx, stop: stop, attackAt: attackAt,
		out: map[peer.ID]*sybilOut{}, mesh: map[peer.ID]bool{}, seen: map[string]time.Duration{}, forged: map[string]bool{},
		timers: map[*time.Timer]struct{}{}}
	h.SetStreamHandler(meshsub, s.handleIncoming)
	h.Network().Notify(&network.NotifyBundle{DisconnectedF: s.disconnected})
	return s
}

// close stops the sybil's timers; closing its host is the caller's job.
func (s *sybil) close() {
	s.stop()
	s.mu.Lock()
	for t := range s.timers {
		t.Stop()
	}
	s.timers = nil
	s.mu.Unlock()
}

func (s *sybil) attacking() bool {
	return s.attackAt >= 0 && s.w.elapsed() >= s.attackAt
}

// later runs f after d unless the sybil stops first. A timer rather than
// a goroutine waits: thousands of sybils each wait for dozens of regrafts.
func (s *sybil) later(d time.Duration, f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timers == nil {
		return // closed
	}
	var t *time.Timer
	t = time.AfterFunc(d, func() {
		s.mu.Lock()
		_, pending := s.timers[t]
		delete(s.timers, t)
		s.mu.Unlock()
		if pending && s.ctx.Err() == nil {
			f()
		}
	})
	s.timers[t] = struct{}{}
}

// dial connects to the honest node target, if it is up.
func (s *sybil) dial(target *node) {
	if s.ctx.Err() != nil || !target.up() {
		return
	}
	_, _ = s.w.mn.ConnectPeers(s.n.pid, target.pid) // fails only if the link is gone
}

// disconnected redials a target that closed the connection.
func (s *sybil) disconnected(_ network.Network, c network.Conn) {
	p := c.RemotePeer()
	if s.ctx.Err() != nil || s.h.Network().Connectedness(p) == network.Connected {
		return
	}
	if i := s.w.index(p); i >= 0 && s.w.nodes[i].honest {
		target := s.w.nodes[i]
		s.later(redialDelay, func() { s.dial(target) })
	}
}

func (s *sybil) handleIncoming(st network.Stream) {
	defer func() { _ = st.Reset() }()
	p := st.Conn().RemotePeer()
	out, err := s.openOut(p)
	if err != nil {
		return
	}
	defer s.dropOut(p, out)
	r := pbio.NewDelimitedReader(st, gossip.MaxRPCSize)
	for {
		var rpc pb.RPC
		if err := r.ReadMsg(&rpc); err != nil {
			return
		}
		s.handleControl(p, &rpc, out)
		for _, msg := range rpc.GetPublish() {
			s.handleMessage(p, msg)
		}
	}
}

// openOut opens the sybil's own stream to p.
func (s *sybil) openOut(p peer.ID) (*sybilOut, error) {
	st, err := s.h.NewStream(s.ctx, p, meshsub)
	if err != nil {
		return nil, err
	}
	out := &sybilOut{s: st, w: pbio.NewDelimitedWriter(st)}
	s.mu.Lock()
	if prev := s.out[p]; prev != nil {
		_ = prev.s.Reset()
	}
	s.out[p] = out
	s.mu.Unlock()
	return out, nil
}

func (s *sybil) dropOut(p peer.ID, out *sybilOut) {
	s.mu.Lock()
	if s.out[p] == out {
		delete(s.out, p)
		delete(s.mesh, p)
	}
	s.mu.Unlock()
	_ = out.s.Reset()
}

// handleControl subscribes to and GRAFTs every topic p announces, and
// follows p's GRAFTs and PRUNEs, re-GRAFTing after a PRUNE.
func (s *sybil) handleControl(p peer.ID, rpc *pb.RPC, out *sybilOut) {
	for _, sub := range rpc.GetSubscriptions() {
		if !sub.GetSubscribe() || sub.GetTopicid() != obieproto.Topic {
			continue
		}
		topic, yes := obieproto.Topic, true
		if out.send(&pb.RPC{
			Subscriptions: []*pb.RPC_SubOpts{{Subscribe: &yes, Topicid: &topic}},
			Control:       &pb.ControlMessage{Graft: []*pb.ControlGraft{{TopicID: &topic}}},
		}) {
			s.setMesh(p, true)
		}
	}
	ctl := rpc.GetControl()
	for range ctl.GetGraft() {
		s.setMesh(p, true)
	}
	for range ctl.GetPrune() {
		s.setMesh(p, false)
		delay := regraftBackoff + time.Duration(rand.Float64()*float64(regraftDelay)) // #nosec G404 -- simulation.
		s.later(delay, func() { s.regraft(p) })
	}
}

func (s *sybil) regraft(p peer.ID) {
	s.mu.Lock()
	out := s.out[p]
	s.mu.Unlock()
	topic := obieproto.Topic
	if out != nil && out.send(&pb.RPC{Control: &pb.ControlMessage{Graft: []*pb.ControlGraft{{TopicID: &topic}}}}) {
		s.setMesh(p, true)
	}
}

func (s *sybil) setMesh(p peer.ID, in bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in {
		s.mesh[p] = true
	} else {
		delete(s.mesh, p)
	}
}

// handleMessage records the first copy of a message, forges a chosen event
// as a preempter and withholds the genuine one, and forwards any other
// message to the mesh unless attacking.
func (s *sybil) handleMessage(from peer.ID, msg *pb.Message) {
	id := eventID(msg.GetData())
	now := s.w.elapsed()
	s.mu.Lock()
	if at, ok := s.seen[id]; ok && now-at < seenTTL {
		s.mu.Unlock()
		return
	}
	s.seen[id] = now
	s.mu.Unlock()
	if e, ok := s.w.eventIdx[id]; ok {
		s.n.trace.add(record{at: now, event: e, from: s.w.index(from), outcome: relayed})
		if s.preempt && s.w.events[e].chosen {
			s.forge(id, msg)
			return
		}
	}
	if s.attacking() {
		return
	}
	s.forward(&pb.RPC{Publish: []*pb.Message{msg}}, from)
}

// forward sends rpc to every mesh peer but from.
func (s *sybil) forward(rpc *pb.RPC, from peer.ID) {
	s.mu.Lock()
	targets := make([]*sybilOut, 0, len(s.mesh))
	for p := range s.mesh {
		if p != from && s.out[p] != nil {
			targets = append(targets, s.out[p])
		}
	}
	s.mu.Unlock()
	for _, out := range targets {
		out.send(rpc)
	}
}

// forge sends every peer, once, a copy of the event msg with its reason
// changed and so an invalid signature: GossipSub remembers its ID and
// drops the genuine event that arrives later (ADR 0009).
func (s *sybil) forge(id string, msg *pb.Message) {
	s.mu.Lock()
	if s.forged[id] {
		s.mu.Unlock()
		return
	}
	s.forged[id] = true
	outs := make([]*sybilOut, 0, len(s.out))
	for _, out := range s.out {
		outs = append(outs, out)
	}
	s.mu.Unlock()
	var ev obieproto.Event
	if json.Unmarshal(msg.GetData(), &ev) != nil {
		return
	}
	ev.Reason = "superseded"
	data, err := json.Marshal(&ev)
	if err != nil {
		return
	}
	topic := obieproto.Topic
	rpc := &pb.RPC{Publish: []*pb.Message{{Data: data, Topic: &topic}}}
	for _, out := range outs {
		out.send(rpc)
	}
}

// inject sends data as a new message to every peer in to.
func (s *sybil) inject(data []byte, to []peer.ID) {
	topic := obieproto.Topic
	rpc := &pb.RPC{Publish: []*pb.Message{{Data: data, Topic: &topic}}}
	s.mu.Lock()
	outs := make([]*sybilOut, 0, len(to))
	for _, p := range to {
		if out := s.out[p]; out != nil {
			outs = append(outs, out)
		}
	}
	s.mu.Unlock()
	for _, out := range outs {
		out.send(rpc)
	}
}

// eventID is the GossipSub message ID OBIE nodes give data: the event ID
// ([TRN-3]); data without one gets an ID of its own that no event has.
func eventID(data []byte) string {
	var ev struct {
		ID string `json:"id"`
	}
	if len(data) <= obieproto.MaxEventSize && json.Unmarshal(data, &ev) == nil && ev.ID != "" {
		return ev.ID
	}
	return "junk"
}
