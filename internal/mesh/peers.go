package mesh

import (
	"slices"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
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
		conns := h.Network().ConnsToPeer(id)
		if len(conns) == 0 {
			continue // disconnected meanwhile
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
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Peer) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func (m *Mesh) isBootstrap(id peer.ID) bool {
	return slices.ContainsFunc(m.bootstrap, func(pi peer.AddrInfo) bool { return pi.ID == id })
}
