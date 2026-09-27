package gossip

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newStore returns a started in-memory store that is stopped when the test
// ends.
func newStore(t *testing.T) *store.DB {
	t.Helper()
	db := store.NewMemory(discardLogger(), store.Options{})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db
}

// publisher is a signing key and its peer ID.
type publisher struct {
	key    ed25519.PrivateKey
	peerID string
}

func newPublisher(t *testing.T) publisher {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := obieproto.PeerIDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return publisher{key: key, peerID: id}
}

var eventCounter atomic.Uint64

// newID returns a unique UUIDv7-shaped event ID.
func newID() string {
	n := eventCounter.Add(1)
	ms := uint64(time.Now().UnixMilli()) // #nosec G115 -- a positive Unix time.
	return fmt.Sprintf("%08x-%04x-7%03x-8%03x-%012x", ms>>16, ms&0xffff, n&0xfff, 0, n)
}

// verdict returns a signed verdict by p issued at issued with a TTL of ttl
// seconds on a public IPv4 address unique to the event.
func (p publisher) verdict(t *testing.T, issued time.Time, ttl int64) *obieproto.Event {
	t.Helper()
	n := eventCounter.Load()
	ev := &obieproto.Event{
		ID:        newID(),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(issued),
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: fmt.Sprintf("85.%d.%d.%d", 10+n>>16&0xff, n>>8&0xff, n&0xff), Scope: "/32"},
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 47, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.9, TTLSeconds: ttl},
		Publisher: obieproto.Publisher{PeerID: p.peerID},
	}
	p.sign(t, ev)
	return ev
}

func (p publisher) sign(t *testing.T, ev *obieproto.Event) {
	t.Helper()
	if err := obieproto.Sign(ev, p.key); err != nil {
		t.Fatal(err)
	}
}

func marshal(t *testing.T, ev *obieproto.Event) []byte {
	t.Helper()
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// countingMetrics counts outcomes.
type countingMetrics struct {
	mu     sync.Mutex
	counts map[Outcome]int
}

func (m *countingMetrics) Observe(o Outcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counts == nil {
		m.counts = map[Outcome]int{}
	}
	m.counts[o]++
}

func (m *countingMetrics) count(o Outcome) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[o]
}
