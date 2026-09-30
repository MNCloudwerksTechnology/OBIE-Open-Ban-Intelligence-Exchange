package mesh

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/eventtrace"
	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// counter counts accepted events.
type counter struct{ accepted chan struct{} }

func (c counter) Observe(_ peer.ID, o gossip.Outcome) {
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
			// Events published before B saw A on the topic were held and
			// follow in order (ADR 0026); A may have accepted one of those.
			waitFor(t, 5*time.Second, "A to store the event", func() bool {
				_, err := storeA.Get(ev.ID)
				return err == nil
			})
			if b.Held(ev.ID) || b.TopicPeers() != 1 {
				t.Errorf("Held = %v, TopicPeers = %d after A received the event", b.Held(ev.ID), b.TopicPeers())
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
	if events, wait := m.Backlog(); m.Held(signedVerdict(t, id, 0).ID) || m.TopicPeers() != 0 || events != 0 || wait != 0 {
		t.Error("Held, TopicPeers or Backlog report something before Start")
	}
}

// TestMeshTracesEvents: with a trace path, the mesh opens the file at
// Start, the node's own publications and the events it receives are
// traced, and Stop closes it; a trace path in a missing directory fails
// the start (ADR 0032).
func TestMeshTracesEvents(t *testing.T) {
	dir := t.TempDir()
	idA, idB := newIdentity(t), newIdentity(t)
	storeA := newStore(t)
	traceA := filepath.Join(dir, "a.jsonl")
	a := startMesh(t, idA, Options{Listen: []string{"/ip4/127.0.0.1/tcp/0"}, Store: storeA, TracePath: traceA})
	b := startMesh(t, idB, Options{
		Listen:    []string{"/ip4/127.0.0.1/tcp/0"},
		Bootstrap: []string{listenAddr(t, a, 6) + "/p2p/" + idA.PeerID()},
		TracePath: filepath.Join(dir, "b.jsonl"),
	})
	waitFor(t, 10*time.Second, "B to connect to A", func() bool { return len(a.Peers()) == 1 })
	var ev *obieproto.Event
	waitFor(t, 10*time.Second, "an event of B on A", func() bool {
		ev = signedVerdict(t, idB, int(time.Now().UnixNano()%1e9))
		if err := b.Publish(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		_, err := storeA.Get(ev.ID)
		return err == nil
	})
	// Stopping B flushes its trace; A's reaches the file within a second.
	if err := b.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "B's publication and A's acceptance in the traces", func() bool {
		recs, err := eventtrace.ReadFiles(traceA, filepath.Join(dir, "b.jsonl"))
		if err != nil {
			return false // not written yet
		}
		var published, accepted bool
		for _, r := range recs {
			published = published || (r.Event == ev.ID && r.Node == idB.PeerID() && r.From == idB.PeerID() &&
				r.Outcome == eventtrace.Published)
			accepted = accepted || (r.Event == ev.ID && r.Node == idA.PeerID() && r.From == idB.PeerID() &&
				r.Outcome == eventtrace.Accepted)
		}
		return published && accepted
	})

	m, err := New(newIdentity(t), Options{Store: newStore(t), Listen: []string{"/ip4/127.0.0.1/tcp/0"},
		TracePath: filepath.Join(dir, "missing", "trace.jsonl")}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "mesh.trace_path") {
		_ = m.Stop(context.Background())
		t.Errorf("Start with a trace in a missing directory = %v, want an error naming mesh.trace_path", err)
	}
}
