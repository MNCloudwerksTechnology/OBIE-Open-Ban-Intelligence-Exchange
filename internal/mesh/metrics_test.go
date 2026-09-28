package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistered(t *testing.T) {
	for _, c := range []prometheus.Collector{peersConnected, peersConfigured} {
		if err := prometheus.Register(c); err == nil {
			t.Errorf("collector was not registered")
		}
	}
}

// TestPeerMetrics: the bootstrap peers are counted at start, the
// connected peers on every change, and none after Stop.
func TestPeerMetrics(t *testing.T) {
	idA, idB := newIdentity(t), newIdentity(t)
	a := startMesh(t, idA, Options{Listen: []string{"/ip4/127.0.0.1/tcp/0"}})
	b, err := New(idB, Options{
		Listen:    []string{"/ip4/127.0.0.1/tcp/0"},
		Bootstrap: []string{listenAddr(t, a, 6) + "/p2p/" + idA.PeerID()},
		Store:     newStore(t),
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.ToFloat64(peersConfigured); got != 1 {
		t.Errorf("obie_peers_configured = %v, want 1", got)
	}
	waitFor(t, 10*time.Second, "obie_peers_connected to reach 1", func() bool {
		return len(b.Peers()) == 1 && testutil.ToFloat64(peersConnected) == 1
	})
	if err := b.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "obie_peers_connected to drop to 0", func() bool {
		return testutil.ToFloat64(peersConnected) == 0
	})
}
