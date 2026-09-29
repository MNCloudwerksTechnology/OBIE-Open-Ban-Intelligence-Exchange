package mesh

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
)

// backoff yields exponentially growing delays from min up to max, with
// equal jitter: a delay d is returned as a random value in [d/2, d).
type backoff struct {
	min, max time.Duration
	next     time.Duration
	// randN returns a random value in [0, n); rand.Int64N when nil.
	randN func(n int64) int64
}

func newBackoff(minDelay, maxDelay time.Duration) *backoff {
	return &backoff{min: minDelay, max: maxDelay, next: minDelay}
}

// Next returns the delay before the next attempt and doubles the one
// after it, up to max.
func (b *backoff) Next() time.Duration {
	d := b.next
	b.next = min(2*d, b.max)
	half := d / 2
	if half <= 0 {
		return d
	}
	randN := b.randN
	if randN == nil {
		randN = rand.Int64N // #nosec G404 -- jitter needs no cryptographic randomness.
	}
	return half + time.Duration(randN(int64(d-half)))
}

// Reset starts over at min.
func (b *backoff) Reset() { b.next = b.min }

// unreachableNext is the next step when a bootstrap peer cannot be
// dialed, logged with the failure.
const unreachableNext = "check that the peer's node runs, that TCP and UDP port 4001 are open between both servers " +
	"and that the peer ID at the end of its address is the one its node shows; sudo obied self-check tests it"

// keepConnected dials the bootstrap peer pi until ctx is canceled: at
// start, after a failed dial with backoff, and after a disconnect one
// backoff step later. The backoff resets once a connection stayed up for
// MaxBackoff. changed receives a value whenever pi's connectedness changes.
func (m *Mesh) keepConnected(ctx context.Context, h host.Host, pi peer.AddrInfo, changed <-chan struct{}) {
	log := m.log.With("peer_id", pi.ID.String())
	b := newBackoff(m.opts.InitialBackoff, m.opts.MaxBackoff)
	for {
		if h.Network().Connectedness(pi.ID) != network.Connected {
			if err := m.dial(ctx, h, pi); err != nil {
				if ctx.Err() != nil {
					return
				}
				m.dialFailed(pi.ID, err)
				delay := b.Next()
				log.Warn("bootstrap peer unreachable", "error", err, "retry_in", delay.String(), "next", unreachableNext)
				if !sleep(ctx, delay) {
					return
				}
				continue
			}
			log.Info("connected to bootstrap peer")
		}

		connectedAt := time.Now()
		for h.Network().Connectedness(pi.ID) == network.Connected {
			select {
			case <-ctx.Done():
				return
			case <-changed:
			}
		}
		if time.Since(connectedAt) >= m.opts.MaxBackoff {
			b.Reset()
		}
		delay := b.Next()
		log.Warn("bootstrap peer disconnected", "redial_in", delay.String())
		if !sleep(ctx, delay) {
			return
		}
	}
}

// dial connects to pi within DialTimeout. The mesh schedules retries
// itself, so go-libp2p's own dial backoff for pi is cleared first.
func (m *Mesh) dial(ctx context.Context, h host.Host, pi peer.AddrInfo) error {
	if sw, ok := h.Network().(*swarm.Swarm); ok {
		sw.Backoff().Clear(pi.ID)
	}
	ctx, cancel := context.WithTimeout(ctx, m.opts.DialTimeout)
	defer cancel()
	return h.Connect(ctx, pi)
}

// sleep waits for d and reports false when ctx was canceled first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// watchConnections logs and reports peers connecting and disconnecting,
// counts the connected peers, pings new peers, records when configured
// peers were last seen, and forwards connectedness changes of bootstrap
// peers to changed.
func (m *Mesh) watchConnections(ctx context.Context, h host.Host, sub event.Subscription, changed map[peer.ID]chan struct{}) {
	defer func() { _ = sub.Close() }()
	for {
		var e event.EvtPeerConnectednessChanged
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Out():
			if !ok {
				return
			}
			e = ev.(event.EvtPeerConnectednessChanged)
		}
		switch e.Connectedness {
		case network.Connected:
			m.log.Info("peer connected", "peer_id", e.Peer.String())
			m.connectedTo(e.Peer)
			m.reportConnection(e.Peer, true)
			m.wg.Go(func() { m.ping(ctx, h, e.Peer) })
		case network.NotConnected:
			m.log.Info("peer disconnected", "peer_id", e.Peer.String())
			m.disconnectedFrom(e.Peer)
			m.reportConnection(e.Peer, false)
		}
		peersConnected.Set(float64(len(h.Network().Peers())))
		if ch, ok := changed[e.Peer]; ok {
			select {
			case ch <- struct{}{}:
			default: // a change is already pending
			}
		}
	}
}

// pingLoop measures the latency of every connected peer each PingInterval.
func (m *Mesh) pingLoop(ctx context.Context, h host.Host) {
	t := time.NewTicker(m.opts.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		var wg sync.WaitGroup
		for _, p := range h.Network().Peers() {
			wg.Go(func() { m.ping(ctx, h, p) })
		}
		wg.Wait()
	}
}

// ping pings p once; ping.Ping records the round-trip time in the
// peerstore's latency estimate.
func (m *Mesh) ping(ctx context.Context, h host.Host, p peer.ID) {
	ctx, cancel := context.WithTimeout(ctx, m.opts.PingInterval)
	defer cancel()
	if res := <-ping.Ping(ctx, h, p); res.Error != nil && ctx.Err() == nil {
		m.log.Debug("ping failed", "peer_id", p.String(), "error", res.Error)
	}
}
