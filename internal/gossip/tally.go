package gossip

import (
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TallyWindow is how far back a Tally counts, in buckets of tallyBucket:
// a count covers the current bucket and the ones before it, between
// TallyWindow - tallyBucket and TallyWindow (ADR 0021).
const (
	TallyWindow = time.Hour
	tallyBucket = 5 * time.Minute
	tallySlots  = int64(TallyWindow / tallyBucket)
)

// MaxTalliedPeers bounds the peers a Tally counts at once. A peer beyond
// it is not counted until others have sent nothing for a window.
const MaxTalliedPeers = 4096

// Tally counts the outcomes of the messages each peer sent over the last
// TallyWindow. It implements Metrics and is safe for concurrent use. Peers
// that sent nothing within the window are dropped.
type Tally struct {
	now func() time.Time

	mu    sync.Mutex
	peers map[peer.ID]*tallyRing
	// swept is the slot in which the peers without messages in the window
	// were last dropped.
	swept int64
}

// tallyRing holds the buckets of one peer, indexed by slot.
type tallyRing [tallySlots]tallyCounts

// tallyCounts counts the outcomes of one bucket.
type tallyCounts struct {
	// slot numbers the bucket's tallyBucket since the Unix epoch.
	slot   int64
	counts [len(Outcomes)]uint32
}

// NewTally returns an empty tally on the clock now; time.Now when nil.
func NewTally(now func() time.Time) *Tally {
	if now == nil {
		now = time.Now
	}
	return &Tally{now: now, peers: map[peer.ID]*tallyRing{}}
}

// Observe counts a message with outcome o that from sent.
func (t *Tally) Observe(from peer.ID, o Outcome) {
	i := slices.Index(Outcomes[:], o)
	if i < 0 {
		return
	}
	slot := t.slot()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(slot)
	buckets, ok := t.peers[from]
	if !ok {
		if len(t.peers) >= MaxTalliedPeers {
			return
		}
		buckets = new(tallyRing)
		t.peers[from] = buckets
	}
	b := &buckets[slot%tallySlots]
	if b.slot != slot {
		*b = tallyCounts{slot: slot}
	}
	if b.counts[i] < ^uint32(0) {
		b.counts[i]++
	}
}

// Counts returns how many messages with each outcome p sent within the
// window; nil if none.
func (t *Tally) Counts(p peer.ID) map[Outcome]int {
	slot := t.slot()
	t.mu.Lock()
	defer t.mu.Unlock()
	buckets, ok := t.peers[p]
	if !ok {
		return nil
	}
	var out map[Outcome]int
	for _, b := range buckets {
		if !inWindow(b.slot, slot) {
			continue
		}
		for i, n := range b.counts {
			if n == 0 {
				continue
			}
			if out == nil {
				out = make(map[Outcome]int, len(Outcomes))
			}
			out[Outcomes[i]] += int(n)
		}
	}
	return out
}

// slot returns the number of the current bucket.
func (t *Tally) slot() int64 {
	return t.now().UnixNano() / int64(tallyBucket)
}

// inWindow reports whether the bucket numbered slot lies in the window
// that ends with the bucket now.
func inWindow(slot, now int64) bool {
	return slot > now-tallySlots && slot <= now
}

// sweep drops the peers without messages in the window, at most once per
// slot. Callers hold t.mu.
func (t *Tally) sweep(slot int64) {
	if slot <= t.swept {
		return
	}
	t.swept = slot
	for p, buckets := range t.peers {
		if !slices.ContainsFunc(buckets[:], func(b tallyCounts) bool { return inWindow(b.slot, slot) }) {
			delete(t.peers, p)
		}
	}
}
