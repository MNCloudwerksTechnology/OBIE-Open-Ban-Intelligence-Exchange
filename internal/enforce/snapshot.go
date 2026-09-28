package enforce

import (
	"maps"
	"net/netip"
	"slices"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// Snapshot is what the last successful pass left (ADR 0022): what the
// backend holds and how the decided blocks came to it. It is immutable;
// the next successful pass replaces it.
type Snapshot struct {
	// Mode is the mode of the pass; At is when it ended.
	Mode config.Mode
	At   time.Time
	// Entries are what the backend holds after the pass — the entries it
	// listed, less the removed, plus the added ones — ordered by prefix and
	// disjoint; none in observe mode.
	Entries []Entry
	// Skipped holds the ranges of the decided blocks the pass did not
	// apply, with the skip reason.
	Skipped map[netip.Prefix]string
	// Deferred holds the ranges, ordered by prefix, whose addition waits
	// until an entry they overlap expired.
	Deferred []netip.Prefix
	// Seq changes whenever the mode, the entries, the skipped or the
	// deferred ranges change, an entry's expiry only when it moved by more
	// than ExpiryTolerance.
	Seq uint64
}

// Coverage is how the last successful pass left a range.
type Coverage struct {
	// Applied is set if an entry holds the range: Entry, its own if
	// Entry.Prefix is the range, else a wider one.
	Applied bool
	Entry   Entry
	// Skipped is the reason the pass did not apply the range, or the
	// wider range Within lies in: a range inside a wider block the cap
	// left out is left out with it.
	Skipped string
	Within  netip.Prefix
	// Deferred is set if its addition waits until an entry it overlaps
	// expired.
	Deferred bool
}

// Applies reports whether an entry holds p: its own or a wider one. It
// costs O(log entries).
func (s *Snapshot) Applies(p netip.Prefix) bool {
	_, ok := s.holder(p)
	return ok
}

// Lookup returns how the pass left p. A range that is neither applied,
// skipped nor deferred was not part of the pass: decided after it, with
// less than MinTimeout left, or not a block.
func (s *Snapshot) Lookup(p netip.Prefix) Coverage {
	if e, ok := s.holder(p); ok {
		return Coverage{Applied: true, Entry: e}
	}
	if s == nil || !p.IsValid() {
		return Coverage{}
	}
	p = p.Masked()
	for bits := p.Bits(); bits >= 0; bits-- {
		w := netip.PrefixFrom(p.Addr(), bits).Masked()
		if reason, ok := s.Skipped[w]; ok {
			return Coverage{Skipped: reason, Within: w}
		}
	}
	_, deferred := slices.BinarySearchFunc(s.Deferred, p, comparePrefix)
	return Coverage{Deferred: deferred}
}

// holder returns the entry holding p. Entries are disjoint and ordered by
// prefix, so it is p's own or the one right before where p would be.
func (s *Snapshot) holder(p netip.Prefix) (Entry, bool) {
	if s == nil || !p.IsValid() {
		return Entry{}, false
	}
	p = p.Masked()
	i, found := slices.BinarySearchFunc(s.Entries, p, func(e Entry, p netip.Prefix) int { return comparePrefix(e.Prefix, p) })
	switch {
	case found:
		return s.Entries[i], true
	case i > 0 && s.Entries[i-1].Prefix.Bits() <= p.Bits() && s.Entries[i-1].Prefix.Contains(p.Addr()):
		return s.Entries[i-1], true
	}
	return Entry{}, false
}

// same reports whether o left the same as s, expiries within
// ExpiryTolerance.
func (s *Snapshot) same(o *Snapshot) bool {
	return s.Mode == o.Mode && maps.Equal(s.Skipped, o.Skipped) && slices.Equal(s.Deferred, o.Deferred) &&
		slices.EqualFunc(s.Entries, o.Entries, func(a, b Entry) bool {
			return a.Prefix == b.Prefix && a.Expires.Sub(b.Expires).Abs() <= ExpiryTolerance
		})
}

// afterApply returns what the backend holds once remove and then add are
// applied to have, ordered by prefix.
func afterApply(have, add, remove []Entry) []Entry {
	held := make(map[netip.Prefix]time.Time, len(have)+len(add))
	for _, e := range have {
		held[e.Prefix] = e.Expires
	}
	for _, e := range remove {
		delete(held, e.Prefix)
	}
	for _, e := range add {
		held[e.Prefix] = e.Expires
	}
	out := make([]Entry, 0, len(held))
	for p, exp := range held {
		out = append(out, Entry{Prefix: p, Expires: exp})
	}
	sortEntries(out)
	return out
}
