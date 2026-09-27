package gossip

import (
	"context"
	"strings"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

const (
	selfPeer  = peer.ID("self")
	peerA     = peer.ID("peer-a")
	peerB     = peer.ID("peer-b")
	testBurst = 3
)

func newValidator(t *testing.T, st store.Store, now time.Time) (*validator, *countingMetrics) {
	t.Helper()
	m := &countingMetrics{}
	return &validator{
		self:       selfPeer,
		store:      st,
		metrics:    m,
		log:        discardLogger(),
		now:        func() time.Time { return now },
		publishers: newLimiter(1, testBurst),
		peers:      newLimiter(1, 2*testBurst),
	}, m
}

func TestCheck(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	pubA := newPublisher(t)
	valid := pubA.verdict(t, now, 3600)
	tampered := strings.Replace(string(marshal(t, valid)), `"events":47`, `"events":48`, 1)
	private := pubA.verdict(t, now, 3600)
	private.Indicator = obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "10.0.0.1", Scope: "/32"}
	pubA.sign(t, private)
	wrongKey := pubA.verdict(t, now, 3600)
	wrongKey.Publisher.PeerID = newPublisher(t).peerID

	tests := []struct {
		name    string
		data    []byte
		outcome Outcome
		result  pubsub.ValidationResult
	}{
		{"valid", marshal(t, valid), Accepted, pubsub.ValidationAccept},
		{"too large", []byte(strings.Repeat(" ", obieproto.MaxEventSize) + "{}"), TooLarge, pubsub.ValidationReject},
		{"not json", []byte("hello"), InvalidSchema, pubsub.ValidationReject},
		{"unknown field", []byte(strings.Replace(string(marshal(t, valid)), `"protocol"`, `"extra":1,"protocol"`, 1)),
			InvalidSchema, pubsub.ValidationReject},
		{"private indicator", marshal(t, private), InvalidSchema, pubsub.ValidationReject},
		{"tampered", []byte(tampered), InvalidSignature, pubsub.ValidationReject},
		{"signed by another key", marshal(t, wrongKey), InvalidSignature, pubsub.ValidationReject},
		{"unsigned", func() []byte {
			ev := pubA.verdict(t, now, 3600)
			ev.Publisher.Signature = ""
			return marshal(t, ev)
		}(), InvalidSignature, pubsub.ValidationReject},
		{"dated too far ahead", marshal(t, pubA.verdict(t, now.Add(obieproto.MaxClockSkew+time.Second), 3600)),
			Expired, pubsub.ValidationIgnore},
		{"just expired", marshal(t, pubA.verdict(t, now.Add(-62*time.Second), 60)), Expired, pubsub.ValidationIgnore},
		{"expired beyond the clock tolerance", marshal(t, pubA.verdict(t, now.Add(-time.Hour), 60)),
			Expired, pubsub.ValidationReject},
		{"tampered and expired", []byte(strings.Replace(string(marshal(t, pubA.verdict(t, now.Add(-time.Hour), 60))),
			`"events":47`, `"events":48`, 1)), InvalidSignature, pubsub.ValidationReject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, m := newValidator(t, newStore(t), now)
			result := v.validate(context.Background(), peerA, &pubsub.Message{Message: &pb.Message{Data: tt.data}})
			if result != tt.result || m.count(tt.outcome) != 1 {
				t.Errorf("validate() = %v with outcomes %v; want %v with one %s", result, m.counts, tt.result, tt.outcome)
			}
		})
	}
}

func TestCheckStoresAcceptedEvents(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	st := newStore(t)
	v, _ := newValidator(t, st, now)
	p := newPublisher(t)
	ev := p.verdict(t, now, 3600)

	if outcome, _ := v.check(peerA, marshal(t, ev)); outcome != Accepted {
		t.Fatalf("check() = %s, want accepted", outcome)
	}
	if _, err := st.Get(ev.ID); err != nil {
		t.Fatalf("accepted event not stored: %v", err)
	}
	if outcome, result := v.check(peerB, marshal(t, ev)); outcome != Duplicate || result != pubsub.ValidationIgnore {
		t.Errorf("second copy: check() = %s, %v; want duplicate, ignore", outcome, result)
	}
}

func TestCheckRelaysEventsTheStoreIgnores(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	st := newStore(t)
	v, _ := newValidator(t, st, now)
	p := newPublisher(t)
	current := p.verdict(t, now, 3600)
	older := p.verdict(t, now.Add(-time.Minute), 3600)
	older.Indicator = current.Indicator
	p.sign(t, older)
	if _, err := st.Put(current); err != nil {
		t.Fatal(err)
	}

	if outcome, result := v.check(peerA, marshal(t, older)); outcome != Accepted || result != pubsub.ValidationAccept {
		t.Errorf("check(older verdict) = %s, %v; want accepted, accept", outcome, result)
	}
	active, err := st.ActiveVerdicts(current.Key(), now)
	if err != nil || len(active) != 1 || active[0].ID != current.ID {
		t.Errorf("ActiveVerdicts = %v, %v; want only the current verdict", active, err)
	}
}

func TestCheckRateLimitsPublishers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	v, _ := newValidator(t, newStore(t), now)
	flooder, other := newPublisher(t), newPublisher(t)

	// The publisher limit applies whichever peer forwards the events.
	for i := range testBurst {
		from := []peer.ID{peerA, peerB}[i%2]
		if outcome, _ := v.check(from, marshal(t, flooder.verdict(t, now, 3600))); outcome != Accepted {
			t.Fatalf("event %d within the burst: %s", i+1, outcome)
		}
	}
	if outcome, result := v.check(peerB, marshal(t, flooder.verdict(t, now, 3600))); outcome != RateLimited || result != pubsub.ValidationIgnore {
		t.Errorf("event beyond the burst: check() = %s, %v; want rate_limited, ignore", outcome, result)
	}
	if outcome, _ := v.check(peerB, marshal(t, other.verdict(t, now, 3600))); outcome != Accepted {
		t.Errorf("another publisher's event: %s, want accepted", outcome)
	}
}

func TestCheckRateLimitsForwardingPeers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	v, _ := newValidator(t, newStore(t), now)

	// Many publishers, one peer: the peer limit (2 × testBurst) applies.
	for i := range 2 * testBurst {
		if outcome, _ := v.check(peerA, marshal(t, newPublisher(t).verdict(t, now, 3600))); outcome != Accepted {
			t.Fatalf("event %d within the peer's burst: %s", i+1, outcome)
		}
	}
	if outcome, _ := v.check(peerA, marshal(t, newPublisher(t).verdict(t, now, 3600))); outcome != RateLimited {
		t.Errorf("event beyond the peer's burst: %s, want rate_limited", outcome)
	}
	if outcome, _ := v.check(peerB, marshal(t, newPublisher(t).verdict(t, now, 3600))); outcome != Accepted {
		t.Errorf("event from another peer: %s, want accepted", outcome)
	}
}

func TestValidateAcceptsLocalPublications(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	st := newStore(t)
	v, m := newValidator(t, st, now)
	ev := newPublisher(t).verdict(t, now, 3600)
	if _, err := st.Put(ev); err != nil {
		t.Fatal(err)
	}
	msg := &pubsub.Message{Message: &pb.Message{Data: marshal(t, ev)}}
	if got := v.validate(context.Background(), selfPeer, msg); got != pubsub.ValidationAccept {
		t.Errorf("validate(own publication) = %v, want accept", got)
	}
	if len(m.counts) != 0 {
		t.Errorf("own publication observed: %v", m.counts)
	}
}

func TestMessageID(t *testing.T) {
	ev := newPublisher(t).verdict(t, time.Now(), 3600)
	data := marshal(t, ev)
	if got := messageID(&pb.Message{Data: data}); got != ev.ID {
		t.Errorf("messageID(event) = %q, want the event ID %q", got, ev.ID)
	}
	// The ID does not depend on the rest of the message, so all nodes agree.
	tampered := []byte(strings.Replace(string(data), `"events":47`, `"events":48`, 1))
	if got := messageID(&pb.Message{Data: tampered}); got != ev.ID {
		t.Errorf("messageID(tampered event) = %q, want %q", got, ev.ID)
	}
	for name, data := range map[string][]byte{
		"not json":  []byte("hello"),
		"no id":     []byte(`{"spec":"obie/0.1"}`),
		"too large": []byte(`{"id":"` + ev.ID + `","x":"` + strings.Repeat("a", obieproto.MaxEventSize) + `"}`),
	} {
		got := messageID(&pb.Message{Data: data})
		if !strings.HasPrefix(got, "sha256:") || got == messageID(&pb.Message{Data: append(data, ' ')}) {
			t.Errorf("messageID(%s) = %q, want a content hash", name, got)
		}
	}
}
