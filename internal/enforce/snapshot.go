package enforce

import (
	"cmp"
	"encoding/binary"
	"maps"
	"net/netip"
	"slices"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// Snapshot is what the last successful pass left (ADR 0022): what the
// backend holds and how the decided blocks came to it. It is immutable;
// the next successful pass replaces it. Only the Reconciler makes one.
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

	// held indexes Entries (see index).
	held *EntryIndex
}

// EntryIndex finds the entry that holds a range among disjoint entries,
// such as those a backend holds, in O(log entries): it searches their
// first and last addresses as integers, IPv4 and IPv6 apart, since the
// decisions list asks it for every decision (ADR 0022). A nil
// EntryIndex holds nothing.
type EntryIndex struct {
	e4, e6 []Entry
	s4     []span[uint32]
	s6     []span[u128]
}

// NewEntryIndex indexes entries, which must be disjoint.
func NewEntryIndex(entries []Entry) *EntryIndex {
	x := &EntryIndex{}
	for _, e := range entries {
		if e.Prefix.Addr().Is4() {
			x.e4 = append(x.e4, e)
		} else {
			x.e6 = append(x.e6, e)
		}
	}
	sortEntries(x.e4)
	sortEntries(x.e6)
	for _, e := range x.e4 {
		x.s4 = append(x.s4, span4(e.Prefix))
	}
	for _, e := range x.e6 {
		x.s6 = append(x.s6, span6(e.Prefix))
	}
	return x
}

// Holder returns the entry that holds p: its own or a wider one. The
// entries being disjoint, it is the last one starting at or before p, if
// it ends at or after p's end.
func (x *EntryIndex) Holder(p netip.Prefix) (Entry, bool) {
	if x == nil || !p.IsValid() {
		return Entry{}, false
	}
	p = p.Masked()
	if p.Addr().Is4() {
		if i, ok := holding(x.s4, span4(p), cmp.Compare[uint32]); ok {
			return x.e4[i], true
		}
	} else if i, ok := holding(x.s6, span6(p), compareU128); ok {
		return x.e6[i], true
	}
	return Entry{}, false
}

// Holds reports whether an entry holds p: its own or a wider one.
func (x *EntryIndex) Holds(p netip.Prefix) bool {
	_, ok := x.Holder(p)
	return ok
}

// span is the first and the last address of a range, as integers.
type span[T any] struct{ first, last T }

// u128 is an IPv6 address as an integer.
type u128 struct{ hi, lo uint64 }

func compareU128(a, b u128) int { return cmp.Or(cmp.Compare(a.hi, b.hi), cmp.Compare(a.lo, b.lo)) }

// span4 returns the first and last address of the IPv4 range p.
func span4(p netip.Prefix) span[uint32] {
	a := p.Addr().As4()
	first := binary.BigEndian.Uint32(a[:])
	return span[uint32]{first, first | ^uint32(0)>>p.Bits()}
}

// span6 returns the first and last address of the IPv6 range p.
func span6(p netip.Prefix) span[u128] {
	a := p.Addr().As16()
	first := u128{binary.BigEndian.Uint64(a[:8]), binary.BigEndian.Uint64(a[8:])}
	last := first
	if bits := p.Bits(); bits < 64 {
		last.hi |= ^uint64(0) >> bits
		last.lo = ^uint64(0)
	} else {
		last.lo |= ^uint64(0) >> (bits - 64)
	}
	return span[u128]{first, last}
}

// index indexes the entries for holder. The reconciler calls it before
// it publishes the snapshot.
func (s *Snapshot) index() {
	s.held = NewEntryIndex(s.Entries)
}

// holding returns the index of the span in spans, ordered and disjoint,
// that holds r: the last one starting at or before r, if it ends at or
// after r's end.
func holding[T any](spans []span[T], r span[T], compare func(a, b T) int) (int, bool) {
	i, j := 0, len(spans)
	for i < j {
		if h := int(uint(i+j) >> 1); compare(spans[h].first, r.first) <= 0 {
			i = h + 1
		} else {
			j = h
		}
	}
	return i - 1, i > 0 && compare(spans[i-1].last, r.last) >= 0
}

// Coverage is how the last successful pass left a range.
type Coverage struct {
	// Applied is set if an entry holds the range: Entry, its own if
	// Entry.Prefix is the range, else a wider one.
	Applied bool
	Entry   Entry
	// Skipped is the reason the pass did not apply the range Within: the
	// range itself, or a wider block the cap left out, which ranges
	// inside it are left out with. The allow-list refuses ranges one by
	// one.
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
	if reason, ok := s.Skipped[p]; ok {
		return Coverage{Skipped: reason, Within: p}
	}
	for bits := p.Bits() - 1; bits >= 0; bits-- {
		if w := netip.PrefixFrom(p.Addr(), bits).Masked(); s.Skipped[w] == SkipMaxEntries {
			return Coverage{Skipped: SkipMaxEntries, Within: w}
		}
	}
	_, deferred := slices.BinarySearchFunc(s.Deferred, p, comparePrefix)
	return Coverage{Deferred: deferred}
}

// holder returns the entry holding p.
func (s *Snapshot) holder(p netip.Prefix) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	return s.held.Holder(p)
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
