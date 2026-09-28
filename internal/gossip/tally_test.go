package gossip

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
)

// fakeClock is a clock tests move by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newTallyClock returns a clock at the start of a tally bucket.
func newTallyClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
}

func TestTallyCountsPerPeerAndOutcome(t *testing.T) {
	clk := newTallyClock()
	tally := NewTally(clk.Now, nil)
	for range 3 {
		tally.Observe(peerA, Accepted)
	}
	tally.Observe(peerA, InvalidSignature)
	tally.Observe(peerB, Duplicate)
	tally.Observe(peerB, Outcome("no such outcome"))

	if got, want := tally.Counts(peerA), map[Outcome]int{Accepted: 3, InvalidSignature: 1}; !maps.Equal(got, want) {
		t.Errorf("Counts(A) = %v, want %v", got, want)
	}
	if got, want := tally.Counts(peerB), map[Outcome]int{Duplicate: 1}; !maps.Equal(got, want) {
		t.Errorf("Counts(B) = %v, want %v", got, want)
	}
	if got := tally.Counts(peer.ID("silent")); got != nil {
		t.Errorf("Counts of a silent peer = %v, want nil", got)
	}
	var _ Metrics = tally
}

// TestTallyCountsTheLastHour: a message counts for a window of an hour,
// in 5-minute buckets, then drops out; buckets are reused.
func TestTallyCountsTheLastHour(t *testing.T) {
	clk := newTallyClock()
	tally := NewTally(clk.Now, nil)
	tally.Observe(peerA, Accepted)
	clk.Add(30 * time.Minute)
	tally.Observe(peerA, Expired)
	clk.Add(29*time.Minute + 59*time.Second) // the first message's bucket is the oldest in the window
	if got, want := tally.Counts(peerA), map[Outcome]int{Accepted: 1, Expired: 1}; !maps.Equal(got, want) {
		t.Errorf("after 59:59: Counts = %v, want %v", got, want)
	}
	clk.Add(time.Second)
	if got, want := tally.Counts(peerA), map[Outcome]int{Expired: 1}; !maps.Equal(got, want) {
		t.Errorf("after an hour: Counts = %v, want %v", got, want)
	}
	tally.Observe(peerA, TooLarge) // reuses the first message's bucket
	if got, want := tally.Counts(peerA), map[Outcome]int{Expired: 1, TooLarge: 1}; !maps.Equal(got, want) {
		t.Errorf("after reusing a bucket: Counts = %v, want %v", got, want)
	}
	clk.Add(TallyWindow)
	if got := tally.Counts(peerA); got != nil {
		t.Errorf("an hour after the last message: Counts = %v, want nil", got)
	}
}

// TestTallyDropsSilentPeers keeps its memory bounded: peers without
// messages in the window are dropped, and at most MaxTalliedPeers are
// counted at once.
func TestTallyDropsSilentPeers(t *testing.T) {
	clk := newTallyClock()
	tally := NewTally(clk.Now, nil)
	for i := range MaxTalliedPeers {
		tally.Observe(peer.ID(fmt.Sprintf("peer-%d", i)), Accepted)
	}
	tally.Observe(peerA, Accepted) // one too many
	if got := tally.Counts(peerA); got != nil {
		t.Errorf("a peer beyond MaxTalliedPeers was counted: %v", got)
	}
	if n := tally.size(); n != MaxTalliedPeers {
		t.Errorf("tallied peers = %d, want %d", n, MaxTalliedPeers)
	}

	clk.Add(TallyWindow)
	tally.Observe(peerA, Accepted)
	if n := tally.size(); n != 1 {
		t.Errorf("after a silent hour: tallied peers = %d, want only the new one", n)
	}
	if got, want := tally.Counts(peerA), map[Outcome]int{Accepted: 1}; !maps.Equal(got, want) {
		t.Errorf("Counts(A) = %v, want %v", got, want)
	}
}

// TestTallyKeepsConfiguredPeers: peers the tally always keeps are counted
// even when throwaway peers filled it.
func TestTallyKeepsConfiguredPeers(t *testing.T) {
	tally := NewTally(newTallyClock().Now, func(p peer.ID) bool { return p == peerB })
	for i := range MaxTalliedPeers {
		tally.Observe(peer.ID(fmt.Sprintf("throwaway-%d", i)), InvalidSchema)
	}
	tally.Observe(peerA, Accepted)
	tally.Observe(peerB, Accepted)
	if got := tally.Counts(peerA); got != nil {
		t.Errorf("an unkept peer beyond the cap was counted: %v", got)
	}
	if got, want := tally.Counts(peerB), map[Outcome]int{Accepted: 1}; !maps.Equal(got, want) {
		t.Errorf("Counts of the kept peer = %v, want %v", got, want)
	}
}

// TestTallyToleratesAClockGoingBack: a clock set back counts into its
// current bucket and never into the future.
func TestTallyToleratesAClockGoingBack(t *testing.T) {
	clk := newTallyClock()
	tally := NewTally(clk.Now, nil)
	tally.Observe(peerA, Accepted)
	clk.Add(-2 * time.Hour)
	tally.Observe(peerA, Duplicate)
	if got, want := tally.Counts(peerA), map[Outcome]int{Duplicate: 1}; !maps.Equal(got, want) {
		t.Errorf("Counts after the clock went back = %v, want %v", got, want)
	}
}

func TestTallyIsSafeForConcurrentUse(t *testing.T) {
	tally := NewTally(nil, nil)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for range 100 {
				tally.Observe(peer.ID(fmt.Sprintf("peer-%d", i%2)), Accepted)
				_ = tally.Counts(peerA)
			}
		})
	}
	wg.Wait()
	if got := tally.Counts("peer-0")[Accepted] + tally.Counts("peer-1")[Accepted]; got != 800 {
		t.Errorf("counted %d messages, want 800", got)
	}
}

// TestValidateObservesTheSendingPeer: the observer learns which peer sent
// each message; the node's own publications are not observed.
func TestValidateObservesTheSendingPeer(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	v, m := newValidator(t, newStore(t), now)
	ev := newPublisher(t).verdict(t, now, 3600)
	msg := func(data []byte) *pubsub.Message { return &pubsub.Message{Message: &pb.Message{Data: data}} }
	v.validate(context.Background(), peerA, msg(marshal(t, ev)))
	v.validate(context.Background(), peerB, msg(marshal(t, ev)))
	v.validate(context.Background(), peerA, msg([]byte("hello")))
	v.validate(context.Background(), selfPeer, msg(marshal(t, ev)))

	if got, want := m.of(peerA), map[Outcome]int{Accepted: 1, InvalidSchema: 1}; !maps.Equal(got, want) {
		t.Errorf("observed from A: %v, want %v", got, want)
	}
	if got, want := m.of(peerB), map[Outcome]int{Duplicate: 1}; !maps.Equal(got, want) {
		t.Errorf("observed from B: %v, want %v", got, want)
	}
	if got := m.of(selfPeer); got != nil {
		t.Errorf("observed the node's own message: %v", got)
	}
}

// size returns the number of peers t counts.
func (t *Tally) size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.peers)
}
