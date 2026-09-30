package simtrust

import (
	"cmp"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/decision"
)

// Episode is one enforced ban of an address: from the block's addition
// until its removal or its expiry, whichever comes first (ADR 0034).
type Episode struct {
	Addr       netip.Addr
	Start, End time.Time
	// Autoblock is set if the observer's own verdict alone started it.
	Autoblock bool
	// Contributors are the publishers whose verdicts counted in the ban,
	// each with the first time it counted, in that order.
	Contributors []Contribution
}

// Contribution is a publisher counting in a ban from At.
type Contribution struct {
	PeerID string
	At     time.Time
}

// openEpisode is an episode that has not ended, with the block's expiry.
type openEpisode struct {
	Episode
	expires time.Time
}

// BanLog turns the engine's block change stream into episodes. Changes
// must be recorded in the order the engine delivers them; it is safe for
// concurrent use.
type BanLog struct {
	mu   sync.Mutex
	open map[string]*openEpisode
	done []Episode
	err  error
}

// NewBanLog returns an empty log.
func NewBanLog() *BanLog {
	return &BanLog{open: map[string]*openEpisode{}}
}

// Record adds the block change c, which the engine evaluated at
// c.Decision.EvaluatedAt.
func (l *BanLog) Record(c decision.Change) {
	l.mu.Lock()
	defer l.mu.Unlock()
	at := c.Decision.EvaluatedAt
	cur, isOpen := l.open[c.Key]
	// A block that reached its expiry has ended, even if the engine only
	// notices it at its next evaluation of the address.
	if isOpen && !at.Before(cur.expires) {
		l.close(c.Key, cur.expires)
		isOpen = false
	}
	switch c.Type {
	case decision.ChangeAdded, decision.ChangeUpdated:
		if !isOpen {
			addr, err := netip.ParseAddr(c.Decision.Indicator.Value)
			if err != nil {
				l.fail(fmt.Errorf("block on %s: %w", c.Key, err))
				return
			}
			cur = &openEpisode{Episode: Episode{Addr: addr, Start: at, Autoblock: c.Decision.Autoblock}}
			l.open[c.Key] = cur
		}
		cur.expires = c.Decision.ExpiresAt
		for _, k := range c.Contributors {
			if !slices.ContainsFunc(cur.Contributors, func(x Contribution) bool { return x.PeerID == k.PeerID }) {
				cur.Contributors = append(cur.Contributors, Contribution{PeerID: k.PeerID, At: at})
			}
		}
	case decision.ChangeRemoved:
		if isOpen {
			l.close(c.Key, at)
		}
	}
}

// close ends the open episode of key at end.
func (l *BanLog) close(key string, end time.Time) {
	cur := l.open[key]
	delete(l.open, key)
	cur.End = end
	l.done = append(l.done, cur.Episode)
}

func (l *BanLog) fail(err error) {
	if l.err == nil {
		l.err = err
	}
}

// Episodes returns every episode by start and address; one still open
// ends at its expiry or at end, whichever comes first. It fails if a
// change could not be read.
func (l *BanLog) Episodes(end time.Time) ([]Episode, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	out := slices.Clone(l.done)
	for _, cur := range l.open {
		e := cur.Episode
		e.End = end
		if cur.expires.Before(end) {
			e.End = cur.expires
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Episode) int {
		return cmp.Or(a.Start.Compare(b.Start), a.Addr.Compare(b.Addr))
	})
	return out, nil
}
