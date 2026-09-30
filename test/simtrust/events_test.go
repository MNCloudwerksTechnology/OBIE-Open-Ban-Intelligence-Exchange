package simtrust

import (
	"math/rand/v2"
	"net/netip"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// TestEventsAreValid checks that the simulated verdicts and revocations
// are valid obie/0.1 events once signed, though the simulation never signs
// them.
func TestEventsAreValid(t *testing.T) {
	k := NewKey(7, "publisher")
	ids := idSource{rng: rand.New(rand.NewPCG(7, 7))} // #nosec G404 -- reproducible test data.
	at := testStart.Add(90 * time.Second)
	v := newVerdict(ids.next(at), publisherOf(k.PeerID, 64512), netip.MustParseAddr(victim), at, 0.8, time.Hour)
	r := newRevocation(ids.next(at.Add(time.Minute)), v, at.Add(time.Minute))
	for _, ev := range []*obieproto.Event{v, r} {
		if err := k.Sign(ev); err != nil {
			t.Fatal(err)
		}
		if err := ev.Validate(obieproto.AllowDocumentationRanges(), obieproto.WithClock(func() time.Time { return at.Add(time.Hour) })); err != nil {
			t.Errorf("%s event: %v", ev.Type, err)
		}
		if err := obieproto.Verify(ev); err != nil {
			t.Errorf("%s event: %v", ev.Type, err)
		}
		if got, ok := obieproto.IDTime(ev.ID); !ok || !got.Equal(ev.IssuedAt.Time) {
			t.Errorf("ID %s carries %s, %v; want the issue time %s", ev.ID, got, ok, ev.IssuedAt.Time)
		}
	}
}

func TestKeysAreReproducible(t *testing.T) {
	a := NewKey(1, "a").PeerID
	if again := NewKey(1, "a").PeerID; again != a {
		t.Errorf("the same seed and label gave %s and %s", a, again)
	}
	if a == NewKey(2, "a").PeerID || a == NewKey(1, "b").PeerID {
		t.Error("different seeds or labels gave the same key")
	}
}
