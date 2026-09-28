// Package mesh runs the node's libp2p host: an encrypted, authenticated
// connection to every configured bootstrap peer, kept alive with
// exponential backoff (ADR 0007), over which events are gossiped
// (internal/gossip, ADR 0009).
//
// The host uses the node identity, listens on TCP and QUIC, secures
// connections with Noise (QUIC with its libp2p TLS handshake) and has no
// peer discovery: no DHT, no mDNS, no relay and no hole punching.
package mesh

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Name is the subsystem name of the mesh.
const Name = "mesh"

// Connection manager water marks: above ConnsHigh connections the host
// trims down to ConnsLow, never trimming bootstrap peers.
const (
	ConnsLow        = 32
	ConnsHigh       = 128
	connGracePeriod = time.Minute
)

// Defaults used when Options leaves a value zero.
const (
	DefaultInitialBackoff = time.Second
	DefaultMaxBackoff     = 5 * time.Minute
	DefaultDialTimeout    = 30 * time.Second
	DefaultPingInterval   = 15 * time.Second
)

// bootstrapTag protects bootstrap peers in the connection manager.
const bootstrapTag = "obie-bootstrap"

// Options configures a Mesh.
type Options struct {
	// Listen are the multiaddrs to listen on (mesh.listen).
	Listen []string
	// Bootstrap are the peers to stay connected to (mesh.bootstrap).
	Bootstrap []string
	// Trust names peers and assigns their trust weight in Peers.
	Trust config.Trust
	// UserAgent is announced to peers.
	UserAgent string
	// Store holds the events received from and published to the mesh; it
	// must be started before the mesh.
	Store store.Store
	// RateLimit bounds the events accepted per publisher and per peer
	// (mesh.rate_limit); zero buckets take the defaults.
	RateLimit config.RateLimit
	// GossipMetrics observes the outcome of every received event; nil for
	// none.
	GossipMetrics gossip.Metrics

	// InitialBackoff and MaxBackoff bound the delay between dials of a
	// disconnected bootstrap peer.
	InitialBackoff, MaxBackoff time.Duration
	// DialTimeout bounds one dial.
	DialTimeout time.Duration
	// PingInterval is how often connected peers are pinged for latency.
	PingInterval time.Duration
}

// Mesh is the libp2p host as a lifecycle subsystem. It implements
// lifecycle.Subsystem, lifecycle.ReadinessChecker and
// lifecycle.DetailReporter.
type Mesh struct {
	id        identity.Identity
	opts      Options
	log       *slog.Logger
	listen    []ma.Multiaddr
	bootstrap []peer.AddrInfo

	// trustMu guards the trust settings, which SetTrust replaces.
	trustMu       sync.RWMutex
	publishers    map[peer.ID]config.Publisher
	defaultWeight float64

	mu     sync.Mutex
	host   host.Host
	gossip *gossip.Gossip
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New returns the mesh of the node with identity id. It fails when an
// address or peer ID in opts is invalid; config validation already rules
// that out for a loaded configuration.
func New(id identity.Identity, opts Options, log *slog.Logger) (*Mesh, error) {
	if opts.Store == nil {
		return nil, errors.New("no store")
	}
	opts = withDefaults(opts)
	m := &Mesh{id: id, opts: opts, log: log}

	for _, s := range opts.Listen {
		addr, err := ma.NewMultiaddr(s)
		if err != nil {
			return nil, fmt.Errorf("listen address %q: %w", s, err)
		}
		m.listen = append(m.listen, addr)
	}

	bootstrap, err := bootstrapPeers(opts.Bootstrap)
	if err != nil {
		return nil, err
	}
	for _, pi := range bootstrap {
		if pi.ID.String() == id.PeerID() {
			log.Warn("ignoring bootstrap peer: it is this node", "peer_id", pi.ID.String())
			continue
		}
		m.bootstrap = append(m.bootstrap, pi)
	}

	if err := m.SetTrust(opts.Trust); err != nil {
		return nil, err
	}
	return m, nil
}

// SetTrust replaces the publisher names and trust weights that Peers
// reports, e.g. after a configuration reload. It fails, changing nothing,
// when a peer ID is invalid.
func (m *Mesh) SetTrust(trust config.Trust) error {
	publishers := make(map[peer.ID]config.Publisher, len(trust.Publishers))
	for _, p := range trust.Publishers {
		pid, err := peer.Decode(p.PeerID)
		if err != nil {
			return fmt.Errorf("trusted publisher %q: %w", p.PeerID, err)
		}
		publishers[pid] = p
	}
	m.trustMu.Lock()
	defer m.trustMu.Unlock()
	m.publishers, m.defaultWeight = publishers, trust.DefaultWeight
	return nil
}

// trustOf returns the name and trust weight of peer id.
func (m *Mesh) trustOf(id peer.ID) (name string, weight float64) {
	m.trustMu.RLock()
	defer m.trustMu.RUnlock()
	if pub, ok := m.publishers[id]; ok {
		return pub.Name, pub.Weight
	}
	return "", m.defaultWeight
}

func withDefaults(opts Options) Options {
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = DefaultInitialBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = DefaultMaxBackoff
	}
	opts.MaxBackoff = max(opts.MaxBackoff, opts.InitialBackoff)
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = DefaultDialTimeout
	}
	if opts.PingInterval <= 0 {
		opts.PingInterval = DefaultPingInterval
	}
	return opts
}

// bootstrapPeers parses /…/p2p/<peer-id> addresses, merging the addresses
// of one peer.
func bootstrapPeers(addrs []string) ([]peer.AddrInfo, error) {
	parsed := make([]ma.Multiaddr, 0, len(addrs))
	for _, s := range addrs {
		addr, err := ma.NewMultiaddr(s)
		if err != nil {
			return nil, fmt.Errorf("bootstrap address %q: %w", s, err)
		}
		parsed = append(parsed, addr)
	}
	peers, err := peer.AddrInfosFromP2pAddrs(parsed...)
	if err != nil {
		return nil, fmt.Errorf("bootstrap addresses: %w", err)
	}
	return peers, nil
}

// Name returns the subsystem name.
func (m *Mesh) Name() string { return Name }

// Start creates the host, which listens once Start returns, joins the
// gossip topic and starts dialing the bootstrap peers in the background.
func (m *Mesh) Start(context.Context) error {
	h, err := m.newHost()
	if err != nil {
		return err
	}
	g, err := gossip.New(h, gossip.Options{
		Store:          m.opts.Store,
		PublisherLimit: m.opts.RateLimit.Publisher,
		PeerLimit:      m.opts.RateLimit.Peer,
		Metrics:        m.opts.GossipMetrics,
	}, m.log)
	if err != nil {
		_ = h.Close()
		return fmt.Errorf("gossip: %w", err)
	}
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerConnectednessChanged))
	if err != nil {
		g.Close()
		_ = h.Close()
		return fmt.Errorf("subscribe to connection events: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.host, m.gossip, m.cancel = h, g, cancel
	m.mu.Unlock()

	changed := make(map[peer.ID]chan struct{}, len(m.bootstrap))
	for _, pi := range m.bootstrap {
		h.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstore.PermanentAddrTTL)
		h.ConnManager().Protect(pi.ID, bootstrapTag)
		changed[pi.ID] = make(chan struct{}, 1)
	}
	m.wg.Go(func() { m.watchConnections(ctx, h, sub, changed) })
	m.wg.Go(func() { m.pingLoop(ctx, h) })
	for _, pi := range m.bootstrap {
		m.wg.Go(func() { m.keepConnected(ctx, h, pi, changed[pi.ID]) })
	}

	m.log.Info("mesh listening", "peer_id", h.ID().String(), "addrs", addrStrings(h.Network().ListenAddresses()),
		"bootstrap_peers", len(m.bootstrap))
	return nil
}

// newHost creates the libp2p host listening on m.listen.
func (m *Mesh) newHost() (host.Host, error) {
	key, err := newHostKey(m.id)
	if err != nil {
		return nil, err
	}
	cm, err := connmgr.NewConnManager(ConnsLow, ConnsHigh, connmgr.WithGracePeriod(connGracePeriod))
	if err != nil {
		return nil, fmt.Errorf("connection manager: %w", err)
	}
	limits := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&limits)
	rm, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limits.AutoScale()))
	if err != nil {
		_ = cm.Close()
		return nil, fmt.Errorf("resource manager: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(key),
		libp2p.ListenAddrs(m.listen...),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(quic.NewTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.ConnectionManager(cm),
		libp2p.ResourceManager(rm),
		libp2p.UserAgent(m.opts.UserAgent),
		// No relay, and no discovery: AutoRelay, hole punching, NAT port
		// mapping, the AutoNAT service and routing (DHT) stay off by not
		// enabling them; the always-present AutoNAT v1 client finds no server.
		libp2p.DisableRelay(),
		// The operator's addresses are always dialed, even if dials of
		// other UDP or IPv6 addresses failed before.
		libp2p.UDPBlackHoleSuccessCounter(nil),
		libp2p.IPv6BlackHoleSuccessCounter(nil),
	)
	if err != nil {
		_ = rm.Close()
		_ = cm.Close()
		return nil, fmt.Errorf("start libp2p host: %w", err)
	}
	if len(h.Network().ListenAddresses()) == 0 && len(m.listen) > 0 {
		_ = h.Close()
		return nil, errors.New("start libp2p host: no listen address could be bound")
	}
	return h, nil
}

// Stop leaves the gossip topic, stops dialing and closes the host with all
// its connections.
func (m *Mesh) Stop(ctx context.Context) error {
	m.mu.Lock()
	h, g, cancel := m.host, m.gossip, m.cancel
	m.host, m.gossip, m.cancel = nil, nil, nil
	m.mu.Unlock()
	if h == nil {
		return nil
	}
	cancel()
	g.Close()
	err := h.Close()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("waiting for mesh goroutines: %w", ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("close libp2p host: %w", err)
	}
	return nil
}

// Ready reports whether the host listens. Having no peers does not make
// the mesh unready; Detail reports it.
func (m *Mesh) Ready() error {
	h := m.currentHost()
	if h == nil {
		return errors.New("host not started")
	}
	if len(h.Network().ListenAddresses()) == 0 {
		return errors.New("not listening")
	}
	return nil
}

// Detail summarizes the connected peers; zero peers is a degraded state.
func (m *Mesh) Detail() string {
	h := m.currentHost()
	if h == nil {
		return ""
	}
	connected := len(h.Network().Peers())
	bootstrap := 0
	for _, pi := range m.bootstrap {
		if h.Network().Connectedness(pi.ID) == network.Connected {
			bootstrap++
		}
	}
	summary := fmt.Sprintf("%d peers connected (%d/%d bootstrap peers)", connected, bootstrap, len(m.bootstrap))
	if connected == 0 {
		return "degraded: " + summary
	}
	return summary
}

// Publish stores ev, an event signed by this node, and sends it to the
// mesh; see gossip.Gossip.Publish. It fails while the mesh is not started.
func (m *Mesh) Publish(ctx context.Context, ev *obieproto.Event) error {
	m.mu.Lock()
	g := m.gossip
	m.mu.Unlock()
	if g == nil {
		return errors.New("mesh not started")
	}
	return g.Publish(ctx, ev)
}

// ListenAddrs returns the addresses the host listens on, with the ports
// actually bound; nil before Start.
func (m *Mesh) ListenAddrs() []ma.Multiaddr {
	h := m.currentHost()
	if h == nil {
		return nil
	}
	return h.Network().ListenAddresses()
}

func (m *Mesh) currentHost() host.Host {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.host
}

func addrStrings(addrs []ma.Multiaddr) []string {
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.String()
	}
	return out
}
