package mesh

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// counter counts accepted events.
type counter struct{ accepted chan struct{} }

func (c counter) Observe(o gossip.Outcome) {
	if o != gossip.Accepted {
		return
	}
	select {
	case c.accepted <- struct{}{}:
	default:
	}
}

// signedVerdict returns the verdict number n signed by id.
func signedVerdict(t *testing.T, id identity.Identity, n int) *obieproto.Event {
	t.Helper()
	ev := &obieproto.Event{
		ID:        fmt.Sprintf("0199a1b2-c3d4-7e5f-8a6b-%012x", n),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(time.Now()),
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "85.10.20.30", Scope: "/32"},
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 47, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.9, TTLSeconds: 3600},
		Publisher: obieproto.Publisher{PeerID: id.PeerID()},
	}
	msg, err := obieproto.CanonicalBytes(ev)
	if err != nil {
		t.Fatal(err)
	}
	ev.Publisher.Signature = "ed25519:" + base64.RawURLEncoding.EncodeToString(id.Sign(msg))
	return ev
}

// TestMeshGossip publishes through the mesh of two nodes connected by
// bootstrap: the events reach the other node's store.
func TestMeshGossip(t *testing.T) {
	idA, idB := newIdentity(t), newIdentity(t)
	storeA := newStore(t)
	accepted := counter{accepted: make(chan struct{}, 1)}
	a := startMesh(t, idA, Options{Listen: []string{"/ip4/127.0.0.1/tcp/0"}, Store: storeA, GossipMetrics: accepted})
	b := startMesh(t, idB, Options{
		Listen:    []string{"/ip4/127.0.0.1/tcp/0"},
		Bootstrap: []string{listenAddr(t, a, 6) + "/p2p/" + idA.PeerID()}, // 6: TCP
	})
	waitFor(t, 10*time.Second, "B to connect to A", func() bool { return len(a.Peers()) == 1 })

	deadline := time.After(5 * time.Second)
	// Publish new events until B has learned that A joined the topic.
	for n := 0; ; n++ {
		ev := signedVerdict(t, idB, n)
		if err := b.Publish(context.Background(), ev); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		select {
		case <-accepted.accepted:
			if _, err := storeA.Get(ev.ID); err != nil {
				t.Fatalf("A accepted the event but did not store it: %v", err)
			}
			return
		case <-time.After(200 * time.Millisecond):
		case <-deadline:
			t.Fatal("the event did not reach A")
		}
	}
}

func TestPublishBeforeStart(t *testing.T) {
	id := newIdentity(t)
	m, err := New(id, Options{Store: newStore(t)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Publish(context.Background(), signedVerdict(t, id, 0)); err == nil {
		t.Error("Publish succeeded before Start")
	}
}
