package mesh

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
)

// knownPeers returns m's known peers by peer ID, checking that they are
// sorted by it.
func knownPeers(t *testing.T, m *Mesh) map[string]KnownPeer {
	t.Helper()
	list := m.KnownPeers()
	out := make(map[string]KnownPeer, len(list))
	for i, k := range list {
		if i > 0 && list[i-1].ID >= k.ID {
			t.Errorf("KnownPeers not sorted by peer ID: %s before %s", list[i-1].ID, k.ID)
		}
		out[k.ID] = k
	}
	return out
}

// closedPort returns a loopback TCP port nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// TestKnownPeersBeforeStart: the configured peers — bootstrap peers and
// trusted publishers, without this node — are known before any connects.
func TestKnownPeersBeforeStart(t *testing.T) {
	self, boot, pub := newIdentity(t), newIdentity(t), newIdentity(t)
	m, err := New(self, Options{
		Store:     newStore(t),
		Bootstrap: []string{"/ip4/192.0.2.1/tcp/4001/p2p/" + boot.PeerID(), "/ip4/192.0.2.1/udp/4001/quic-v1/p2p/" + boot.PeerID()},
		Trust: config.Trust{Publishers: []config.Publisher{
			{PeerID: pub.PeerID(), Name: "pub", Weight: 0.5},
			{PeerID: self.PeerID(), Name: "me", Weight: 1},
		}, DefaultWeight: 0.1},
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := knownPeers(t, m)
	want := map[string]KnownPeer{
		boot.PeerID(): {Peer: Peer{ID: boot.PeerID(), Addrs: []string{"/ip4/192.0.2.1/tcp/4001", "/ip4/192.0.2.1/udp/4001/quic-v1"},
			TrustWeight: 0.1, Bootstrap: true}},
		pub.PeerID(): {Peer: Peer{ID: pub.PeerID(), Name: "pub", TrustWeight: 0.5}, Publisher: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("KnownPeers() = %+v\nwant %+v", got, want)
	}
	if w := m.DefaultWeight(); w != 0.1 {
		t.Errorf("DefaultWeight() = %v, want 0.1", w)
	}

	// A reload that trusts the bootstrap peer makes it a publisher too.
	if err := m.SetTrust(config.Trust{Publishers: []config.Publisher{{PeerID: boot.PeerID(), Name: "boot", Weight: 0.9}}}); err != nil {
		t.Fatal(err)
	}
	got = knownPeers(t, m)
	if k := got[boot.PeerID()]; len(got) != 1 || !k.Publisher || !k.Bootstrap || k.Name != "boot" || k.TrustWeight != 0.9 {
		t.Errorf("after the reload: KnownPeers() = %+v, want only the bootstrap peer, trusted", got)
	}
	if w := m.DefaultWeight(); w != 0 {
		t.Errorf("DefaultWeight() after the reload = %v, want 0", w)
	}
}

// TestKnownPeersRecordFailedDials: an unreachable bootstrap peer is shown
// disconnected with why its last dial failed.
func TestKnownPeersRecordFailedDials(t *testing.T) {
	boot := newIdentity(t)
	addr := "/ip4/127.0.0.1/tcp/" + strconv.Itoa(closedPort(t))
	started := time.Now()
	m := startMesh(t, newIdentity(t), Options{
		Listen: []string{"/ip4/127.0.0.1/tcp/0"}, Bootstrap: []string{addr + "/p2p/" + boot.PeerID()},
		InitialBackoff: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, DialTimeout: time.Second,
	})
	waitFor(t, 10*time.Second, "a failed dial", func() bool { return knownPeers(t, m)[boot.PeerID()].DialError != "" })
	k := knownPeers(t, m)[boot.PeerID()]
	if k.Connected || !k.Bootstrap || !k.LastSeen.IsZero() || !reflect.DeepEqual(k.Addrs, []string{addr}) {
		t.Errorf("unreachable bootstrap peer = %+v", k)
	}
	if k.DialFailedAt.Before(started) || k.DialFailedAt.After(time.Now()) || strings.Contains(k.DialError, "\n") {
		t.Errorf("dial failure %q at %v, want one line since the start", k.DialError, k.DialFailedAt)
	}
}

func TestDialErrorIsBounded(t *testing.T) {
	m, err := New(newIdentity(t), Options{Store: newStore(t)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	id := newIdentity(t)
	pid, err := peer.Decode(id.PeerID())
	if err != nil {
		t.Fatal(err)
	}
	m.dialFailed(pid, errors.New(strings.Repeat("all dials failed\n  * [/ip4/192.0.2.1/tcp/4001] refused ", 100)))
	_, msg, _ := m.history(pid)
	if len(msg) > maxDialError+len("…") || strings.Contains(msg, "\n") || !strings.HasSuffix(msg, "…") {
		t.Errorf("dial error of %d bytes: %q", len(msg), msg)
	}
}

// TestKnownPeersConnected: connected peers, configured or not, their
// events, and when a configured one was last seen after it went away.
func TestKnownPeersConnected(t *testing.T) {
	idA, idB, idC := newIdentity(t), newIdentity(t), newIdentity(t)
	tuning := func(o Options) Options {
		o.InitialBackoff, o.MaxBackoff, o.DialTimeout = 50*time.Millisecond, 200*time.Millisecond, time.Second
		o.ScoreInspectInterval = 20 * time.Millisecond
		return o
	}
	a := startMesh(t, idA, tuning(Options{Listen: []string{"/ip4/127.0.0.1/tcp/0"}}))
	tcpA := listenAddr(t, a, ma.P_TCP)
	accepted := counter{accepted: make(chan struct{}, 1)}
	b := startMesh(t, idB, tuning(Options{
		Listen: []string{"/ip4/127.0.0.1/tcp/0"}, Bootstrap: []string{tcpA + "/p2p/" + idA.PeerID()},
		Trust: config.Trust{Publishers: []config.Publisher{
			{PeerID: idA.PeerID(), Name: "alpha", Weight: 0.7},
			{PeerID: idC.PeerID(), Name: "charlie", Weight: 0},
		}},
		GossipMetrics: accepted,
	}))
	waitFor(t, 10*time.Second, "B to connect to A", func() bool { return knownPeers(t, b)[idA.PeerID()].Connected })

	got := knownPeers(t, b)
	if k := got[idA.PeerID()]; !k.Bootstrap || !k.Publisher || k.Name != "alpha" || k.TrustWeight != 0.7 ||
		k.ConnectedSince.IsZero() || !reflect.DeepEqual(k.Addrs, []string{tcpA}) || !k.LastSeen.IsZero() || k.DialError != "" {
		t.Errorf("B's view of A = %+v", k)
	}
	if k := got[idC.PeerID()]; k.Connected || k.Bootstrap || !k.Publisher || k.Name != "charlie" || len(k.Addrs) != 0 {
		t.Errorf("B's view of the absent publisher C = %+v", k)
	}
	if k := knownPeers(t, a)[idB.PeerID()]; !k.Connected || k.Bootstrap || k.Publisher || k.TrustWeight != 0 {
		t.Errorf("A's view of B = %+v, want connected and not configured", k)
	}

	// B counts the events A sends.
	deadline := time.After(5 * time.Second)
	for n := 0; ; n++ {
		if err := a.Publish(context.Background(), signedVerdict(t, idA, n)); err != nil {
			t.Fatal(err)
		}
		select {
		case <-accepted.accepted:
		case <-time.After(200 * time.Millisecond):
			continue
		case <-deadline:
			t.Fatal("no event from A reached B")
		}
		break
	}
	if n := knownPeers(t, b)[idA.PeerID()].Events[gossip.Accepted]; n < 1 {
		t.Errorf("B counted %d accepted events from A, want at least 1", n)
	}
	// B reads A's GossipSub score, which grows with A's first deliveries.
	waitFor(t, 5*time.Second, "B to score A's first deliveries", func() bool {
		s := knownPeers(t, b)[idA.PeerID()].GossipScore
		return s != nil && s.FirstMessageDeliveries >= 1 && s.Score > 0
	})
	if p := peerIDs(b)[idA.PeerID()]; p.GossipScore == nil || p.GossipScore.Score <= 0 {
		t.Errorf("B's connected peer A has the score %+v, want one above 0", p.GossipScore)
	}

	stopped := time.Now()
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "B to notice A is gone", func() bool { return !knownPeers(t, b)[idA.PeerID()].Connected })
	k := knownPeers(t, b)[idA.PeerID()]
	if k.LastSeen.Before(stopped) || k.LastSeen.After(time.Now()) || !k.ConnectedSince.IsZero() ||
		!reflect.DeepEqual(k.Addrs, []string{tcpA}) || k.Events[gossip.Accepted] < 1 {
		t.Errorf("B's view of A after it stopped = %+v", k)
	}
	// GossipSub keeps the score of a peer that left only if it is not
	// positive (RetainScore); A left with a positive score, so B drops it
	// at the next inspection.
	waitFor(t, 5*time.Second, "B to drop A's positive score as A left", func() bool {
		return knownPeers(t, b)[idA.PeerID()].GossipScore == nil
	})
	waitFor(t, 5*time.Second, "B's redial of A to fail", func() bool { return knownPeers(t, b)[idA.PeerID()].DialError != "" })
}

// TestKnownPeersAfterStop: a configured peer that was connected when the
// mesh stopped was last seen then.
func TestKnownPeersAfterStop(t *testing.T) {
	idA, idB := newIdentity(t), newIdentity(t)
	a := startMesh(t, idA, Options{Listen: []string{"/ip4/127.0.0.1/tcp/0"}})
	b := startMesh(t, idB, Options{Listen: []string{"/ip4/127.0.0.1/tcp/0"},
		Bootstrap: []string{listenAddr(t, a, ma.P_TCP) + "/p2p/" + idA.PeerID()}})
	waitFor(t, 10*time.Second, "B to connect to A", func() bool { return knownPeers(t, b)[idA.PeerID()].Connected })
	stopped := time.Now()
	if err := b.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k := knownPeers(t, b)[idA.PeerID()]; k.Connected || k.LastSeen.Before(stopped) || k.LastSeen.After(time.Now()) {
		t.Errorf("B's view of A after B stopped = %+v, want last seen when B stopped", k)
	}
}
