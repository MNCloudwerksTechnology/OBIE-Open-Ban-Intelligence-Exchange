package mesh

import (
	"slices"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
)

// Peer describes a connected peer.
type Peer struct {
	ID string
	// Name is the peer's name in trust.publishers; empty if not listed.
	Name string
	// Addrs are the remote addresses of the open connections.
	Addrs []string
	// ConnectedSince is when the oldest open connection was opened.
	ConnectedSince time.Time
	// Latency is the smoothed ping round-trip time; 0 while unmeasured.
	Latency time.Duration
	// TrustWeight is the weight from trust.publishers, else
	// trust.default_weight.
	TrustWeight float64
	// Bootstrap is set for peers listed in mesh.bootstrap.
	Bootstrap bool
}

// Peers returns the connected peers sorted by peer ID; nil before Start.
func (m *Mesh) Peers() []Peer {
	h := m.currentHost()
	if h == nil {
		return nil
	}
	ids := h.Network().Peers()
	out := make([]Peer, 0, len(ids))
	for _, id := range ids {
		if p, ok := m.peerOf(h, id); ok {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Peer) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// peerOf describes the connected peer id; false if it disconnected
// meanwhile.
func (m *Mesh) peerOf(h host.Host, id peer.ID) (Peer, bool) {
	conns := h.Network().ConnsToPeer(id)
	if len(conns) == 0 {
		return Peer{}, false
	}
	p := Peer{
		ID:        id.String(),
		Latency:   h.Peerstore().LatencyEWMA(id),
		Bootstrap: m.isBootstrap(id),
	}
	p.Name, p.TrustWeight = m.trustOf(id)
	for _, c := range conns {
		p.Addrs = append(p.Addrs, c.RemoteMultiaddr().String())
		if opened := c.Stat().Opened; p.ConnectedSince.IsZero() || opened.Before(p.ConnectedSince) {
			p.ConnectedSince = opened
		}
	}
	slices.Sort(p.Addrs)
	return p, true
}

func (m *Mesh) isBootstrap(id peer.ID) bool {
	return slices.ContainsFunc(m.bootstrap, func(pi peer.AddrInfo) bool { return pi.ID == id })
}

// KnownPeer is a peer the mesh knows: a configured one — listed in
// mesh.bootstrap or trust.publishers — or a connected one (ADR 0021).
type KnownPeer struct {
	// Peer describes the peer. While it is not connected, its connection
	// fields are zero and Addrs are its mesh.bootstrap addresses, if any.
	Peer
	Connected bool
	// Publisher is set for peers listed in trust.publishers.
	Publisher bool
	// LastSeen is when a configured peer that is not connected was last
	// connected; zero if it was not since obied started.
	LastSeen time.Time
	// DialError is why the last dial of a bootstrap peer that is not
	// connected failed, and DialFailedAt when; empty if none failed since
	// it was last connected.
	DialError    string
	DialFailedAt time.Time
	// Events counts the outcomes of the events the peer sent over the last
	// gossip.TallyWindow; nil if it sent none.
	Events map[gossip.Outcome]int
}

// dialFailure is the last failed dial of a bootstrap peer.
type dialFailure struct {
	err string
	at  time.Time
}

// maxDialError bounds the dial error kept per bootstrap peer: go-libp2p
// names every address it tried.
const maxDialError = 1024

// KnownPeers returns the configured and the connected peers, without this
// node, sorted by peer ID. Before Start every configured peer is shown not
// connected.
func (m *Mesh) KnownPeers() []KnownPeer {
	known := map[peer.ID]KnownPeer{}
	if h := m.currentHost(); h != nil {
		for _, id := range h.Network().Peers() {
			if p, ok := m.peerOf(h, id); ok {
				known[id] = KnownPeer{Peer: p, Connected: true}
			}
		}
	}
	for _, pi := range m.bootstrap {
		if _, ok := known[pi.ID]; !ok {
			known[pi.ID] = KnownPeer{Peer: m.configuredPeer(pi.ID, addrStrings(pi.Addrs))}
		}
	}
	for _, id := range m.publisherIDs() {
		if _, ok := known[id]; !ok && id.String() != m.id.PeerID() {
			known[id] = KnownPeer{Peer: m.configuredPeer(id, nil)}
		}
	}
	out := make([]KnownPeer, 0, len(known))
	for id, k := range known {
		k.Publisher = m.isPublisher(id)
		k.Events = m.tally.Counts(id)
		if !k.Connected {
			k.LastSeen, k.DialError, k.DialFailedAt = m.history(id)
		}
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b KnownPeer) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// configuredPeer describes the configured peer id, which is not connected,
// with its configured addresses addrs.
func (m *Mesh) configuredPeer(id peer.ID, addrs []string) Peer {
	p := Peer{ID: id.String(), Addrs: addrs, Bootstrap: m.isBootstrap(id)}
	p.Name, p.TrustWeight = m.trustOf(id)
	return p
}

// publisherIDs returns the peers listed in trust.publishers.
func (m *Mesh) publisherIDs() []peer.ID {
	m.trustMu.RLock()
	defer m.trustMu.RUnlock()
	ids := make([]peer.ID, 0, len(m.publishers))
	for id := range m.publishers {
		ids = append(ids, id)
	}
	return ids
}

// connectedTo forgets the failed dials of peer id, which just connected.
func (m *Mesh) connectedTo(id peer.ID) {
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	delete(m.dialFailures, id)
}

// disconnectedFrom records when peer id was last seen, if it is
// configured.
func (m *Mesh) disconnectedFrom(id peer.ID) {
	if !m.isBootstrap(id) && !m.isPublisher(id) {
		return
	}
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	m.lastSeen[id] = time.Now()
}

// dialFailed records a failed dial of the bootstrap peer id, with err on
// one line.
func (m *Mesh) dialFailed(id peer.ID, err error) {
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if len(msg) > maxDialError {
		msg = strings.ToValidUTF8(msg[:maxDialError], "") + "…"
	}
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	m.dialFailures[id] = dialFailure{err: msg, at: time.Now()}
}

// history returns when peer id was last seen and its last failed dial.
func (m *Mesh) history(id peer.ID) (lastSeen time.Time, dialErr string, dialFailedAt time.Time) {
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	f := m.dialFailures[id]
	return m.lastSeen[id], f.err, f.at
}

// observers passes every outcome to each of its Metrics.
type observers []gossip.Metrics

func (o observers) Observe(from peer.ID, outcome gossip.Outcome) {
	for _, m := range o {
		m.Observe(from, outcome)
	}
}
