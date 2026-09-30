package sim

import (
	"testing"
	"testing/synctest"
	"time"
)

// TestMemStore: the store accepts an unexpired event once and remembers
// it until it expires, like store.DB's Put and Seen.
func TestMemStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newMemStore()
		ev := newVerdict(testIdentity(t, 2), time.Now(), 60, 1, 0)
		if seen, _ := s.Seen(ev.ID); seen {
			t.Error("Seen before Put")
		}
		if ok, err := s.Put(ev); !ok || err != nil {
			t.Errorf("first Put = %v, %v; want true, nil", ok, err)
		}
		if ok, _ := s.Put(ev); ok {
			t.Error("second Put accepted the event again")
		}
		if seen, _ := s.Seen(ev.ID); !seen {
			t.Error("not Seen after Put")
		}
		time.Sleep(61 * time.Second)
		if seen, _ := s.Seen(ev.ID); seen {
			t.Error("Seen after the event expired")
		}
		if ok, _ := s.Put(ev); ok {
			t.Error("Put accepted an expired event")
		}
	})
}
