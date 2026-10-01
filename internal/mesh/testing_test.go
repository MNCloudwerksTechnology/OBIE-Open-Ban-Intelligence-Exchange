package mesh

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
)

// managedHost is a mocknet host that uses the mesh's connection manager.
type managedHost struct {
	host.Host
	cm connmgr.ConnManager
}

func (h managedHost) ConnManager() connmgr.ConnManager { return h.cm }

func (h managedHost) Close() error {
	_ = h.cm.Close()
	return h.Host.Close()
}

// mocknetHost returns a Testing.NewHost that adds the node to mn at addr
// and reports the connection manager it got.
func mocknetHost(mn mocknet.Mocknet, addr string, got *connmgr.ConnManager) func(crypto.PrivKey, connmgr.ConnManager) (host.Host, error) {
	return func(key crypto.PrivKey, cm connmgr.ConnManager) (host.Host, error) {
		a, err := ma.NewMultiaddr(addr)
		if err != nil {
			return nil, err
		}
		h, err := mn.AddPeer(key, a)
		if err != nil {
			return nil, err
		}
		h.Network().Notify(cm.Notifee())
		*got = cm
		return managedHost{Host: h, cm: cm}, nil
	}
}

// TestMeshOnInjectedHost runs two meshes on a mocknet in virtual time: B
// dials its bootstrap peer A until a link exists, protects it in the
// connection manager it was given, and gossips an event that A accepts;
// A's gossip hooks see it.
func TestMeshOnInjectedHost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mn := mocknet.New()
		defer func() { _ = mn.Close() }()
		idA, idB := newIdentity(t), newIdentity(t)
		var cmA, cmB connmgr.ConnManager
		var mu sync.Mutex
		var observed []gossip.Outcome
		storeA := newStore(t)
		a := startMesh(t, idA, Options{Store: storeA, Testing: Testing{
			NewHost: mocknetHost(mn, "/ip4/10.0.0.1/tcp/4001", &cmA),
			Gossip: gossip.Testing{Observe: func(_ string, _ peer.ID, o gossip.Outcome) {
				mu.Lock()
				defer mu.Unlock()
				observed = append(observed, o)
			}},
		}})
		b := startMesh(t, idB, Options{
			Bootstrap: []string{"/ip4/10.0.0.1/tcp/4001/p2p/" + idA.PeerID()},
			Testing:   Testing{NewHost: mocknetHost(mn, "/ip4/10.0.0.2/tcp/4001", &cmB)},
		})
		time.Sleep(time.Second) // B's first dial fails: there is no link yet
		if len(a.Peers()) != 0 {
			t.Fatal("A has a peer before any link exists")
		}
		if err := mn.LinkAll(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * DefaultInitialBackoff)
		if len(a.Peers()) != 1 || len(b.Peers()) != 1 {
			t.Fatalf("peers: A %d, B %d; want each connected to the other", len(a.Peers()), len(b.Peers()))
		}
		pidA, err := peer.Decode(idA.PeerID())
		if err != nil {
			t.Fatal(err)
		}
		if !cmB.IsProtected(pidA, bootstrapTag) {
			t.Error("B's connection manager does not protect its bootstrap peer")
		}
		if cmA == nil {
			t.Error("A's host did not get a connection manager")
		}

		time.Sleep(2 * time.Second) // the topic mesh forms in a heartbeat
		ev := signedVerdict(t, idB, 1)
		if err := b.Publish(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		if _, err := storeA.Get(ev.ID); err != nil {
			t.Fatalf("A did not store B's event: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(observed) != 1 || observed[0] != gossip.Accepted {
			t.Errorf("A's Observe hook saw %v, want one accepted event", observed)
		}
	})
}
