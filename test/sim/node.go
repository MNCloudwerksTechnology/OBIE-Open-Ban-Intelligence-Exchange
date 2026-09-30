package sim

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// node is a simulated node: an honest OBIE node (internal/mesh with its
// gossip) or an adversary (sybil), on a mocknet host.
type node struct {
	idx    int32
	id     *simIdentity
	pid    peer.ID
	addr   ma.Multiaddr
	region int
	honest bool
	// publisher: the node publishes the run's verdicts.
	publisher bool
	// dials are the nodes the node dials: its bootstrap list, or a sybil's
	// targets.
	dials []int

	trace traceLog

	// Adversaries only: when the sybil dials its targets and stops
	// forwarding (see sybil.attackAt), and whether it preempts.
	connectAt, attackAt time.Duration
	preempt             bool

	// Honest nodes only.
	store store.Store
	// db is the real store, where eviction is measured; nil otherwise.
	db *store.DB

	mu sync.Mutex
	// running is set while the node's mesh (or a sybil's host) runs.
	running bool
	mesh    *mesh.Mesh
	host    host.Host
	sybil   *sybil
	// offline are the periods the node was down, as [from, to) since the
	// run started; to is 0 while it is.
	offline [][2]int64
}

func (n *node) up() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.running
}

// bootstrapAddr is the node's mesh.bootstrap address.
func (n *node) bootstrapAddr() string { return fmt.Sprintf("%s/p2p/%s", n.addr, n.pid) }

// managedHost is a mocknet host with the node's connection manager: v0.1's
// for the v0.1 variant, none for the paper's variants.
type managedHost struct {
	host.Host
	cm connmgr.ConnManager
}

func (h managedHost) ConnManager() connmgr.ConnManager { return h.cm }

func (h managedHost) Close() error {
	_ = h.cm.Close()
	return h.Host.Close()
}

// newHost adds n to the mocknet, answering pings like a libp2p host.
func (w *world) newHost(n *node) (host.Host, error) {
	key, err := crypto.UnmarshalEd25519PrivateKey(n.id.key)
	if err != nil {
		return nil, err
	}
	h, err := w.mn.AddPeer(key, n.addr)
	if err != nil {
		return nil, err
	}
	ping.NewPingService(h)
	return h, nil
}

// pingInterval is how often an honest node pings its peers: never within
// a run.
const pingInterval = 24 * time.Hour

// startHonest starts n's mesh on the host h, which the mesh's host hook
// hands over.
func (w *world) startHonest(n *node, h host.Host) error {
	boot := make([]string, len(n.dials))
	for i, d := range n.dials {
		boot[i] = w.nodes[d].bootstrapAddr()
	}
	tr := tracer{w: w, log: &n.trace}
	m, err := mesh.New(n.id, mesh.Options{
		Bootstrap: boot,
		UserAgent: "obied/sim",
		Store:     n.store,
		// Pings only estimate latency for the admin API; routing never
		// reads it. Every peer pinged every 15 s costs a tenth of an
		// eclipse run's CPU (ADR 0033).
		PingInterval: pingInterval,

		AllowDocumentationRanges: true,
		Testing: mesh.Testing{
			NewHost: func(_ crypto.PrivKey, cm connmgr.ConnManager) (host.Host, error) {
				if !w.variant.ConnManager {
					_ = cm.Close()
					cm = &connmgr.NullConnMgr{}
				} else {
					h.Network().Notify(cm.Notifee())
				}
				return managedHost{Host: h, cm: cm}, nil
			},
			Gossip: gossip.Testing{Router: w.variant.Router, Tracer: tr, Observe: tr.observe},
		},
	}, w.logger)
	if err != nil {
		return err
	}
	if err := m.Start(context.Background()); err != nil {
		return err
	}
	n.mu.Lock()
	n.mesh, n.host, n.running = m, h, true
	n.mu.Unlock()
	return nil
}

// stopNode stops n's mesh, or closes a sybil, and records the outage.
func (w *world) stopNode(n *node) {
	n.mu.Lock()
	m, s, h, was := n.mesh, n.sybil, n.host, n.running
	n.mesh, n.sybil, n.host, n.running = nil, nil, nil, false
	if was {
		n.offline = append(n.offline, [2]int64{int64(w.elapsed()), 0})
	}
	n.mu.Unlock()
	if !was {
		return
	}
	if m != nil {
		if err := m.Stop(context.Background()); err != nil {
			w.logger.Warn("stopping a mesh failed", "node", n.idx, "error", err)
		}
	}
	if s != nil {
		s.close()
		_ = h.Close()
	}
	n.trace.clearMesh()
}

// publish publishes ev from the honest node n and records it.
func (w *world) publish(n *node, e int32) {
	n.mu.Lock()
	m := n.mesh
	n.mu.Unlock()
	if m == nil {
		return
	}
	n.trace.add(record{at: w.elapsed(), event: e, from: n.idx, outcome: published})
	if err := m.Publish(context.Background(), w.events[e].ev); err != nil {
		w.logger.Warn("publishing failed", "node", n.idx, "error", err)
	}
}

// sign signs ev with the key.
func sign(ev *obieproto.Event, key ed25519.PrivateKey) *obieproto.Event {
	if err := obieproto.Sign(ev, key); err != nil {
		panic(err) // a built event always signs
	}
	return ev
}
