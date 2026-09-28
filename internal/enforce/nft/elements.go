package nft

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
)

// element is one kernel element of an interval set. A prefix is the
// half-open range [start, end): a start element keyed by its first
// address and an interval-end element keyed by the address after its
// last one. A range reaching the top of the address space has no end
// element.
type element struct {
	key []byte
	end bool
	// comment of a start element is its prefix. The kernel keeps the end
	// of an expired range until the next change of the set, so pairing
	// starts with ends can be ambiguous; the comment is not.
	comment string
	// timeout is the lifetime the element is added with; zero in listed
	// elements without a timeout.
	timeout time.Duration
	// expires is the lifetime left of a listed element.
	expires time.Duration
}

// errExpired rejects an entry without time left: a zero timeout would add
// it without one, blocking forever.
var errExpired = errors.New("entry has no time left")

// toElements maps e to the elements of its prefix. The start element
// carries e's remaining timeout at now, rounded up to the kernel's
// millisecond resolution; the kernel refuses a timeout on an interval end
// and removes the end together with its expired start.
func toElements(e enforce.Entry, now time.Time) ([]element, error) {
	p := e.Prefix.Masked()
	if !p.IsValid() {
		return nil, fmt.Errorf("invalid prefix %v", e.Prefix)
	}
	left := e.Expires.Sub(now)
	if left <= 0 {
		return nil, fmt.Errorf("%v: %w", p, errExpired)
	}
	timeout := left.Truncate(time.Millisecond)
	if timeout < left {
		timeout += time.Millisecond
	}
	out := []element{{key: p.Addr().AsSlice(), comment: p.String(), timeout: timeout}}
	if end, ok := rangeEnd(p); ok {
		out = append(out, element{key: end.AsSlice(), end: true})
	}
	return out, nil
}

// keys maps prefixes to the elements that delete them; deletion needs no
// timeout.
func keys(p netip.Prefix) []element {
	p = p.Masked()
	out := []element{{key: p.Addr().AsSlice()}}
	if end, ok := rangeEnd(p); ok {
		out = append(out, element{key: end.AsSlice(), end: true})
	}
	return out
}

// rangeEnd returns the address after the last one of p; ok is false when
// p reaches the top of the address space.
func rangeEnd(p netip.Prefix) (netip.Addr, bool) {
	b := p.Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	last, _ := netip.AddrFromSlice(b)
	next := last.Next()
	return next, next.IsValid()
}

// fromElements maps the listed elements of one set back to entries,
// ordered by prefix. A start element's prefix is its comment; a start
// without one (not added by obied) is paired with the lowest interval end
// above it. Entries with less than minLeft to live are left out, and one
// without a timeout expires at never. A range that is not a single prefix
// is an error: obied never adds one.
func fromElements(elems []element, now time.Time, minLeft time.Duration, never time.Time) ([]enforce.Entry, error) {
	var starts []element
	var ends []netip.Addr
	for _, el := range elems {
		addr, ok := netip.AddrFromSlice(el.key)
		if !ok {
			return nil, fmt.Errorf("element key of %d bytes is no address", len(el.key))
		}
		if el.end {
			ends = append(ends, addr)
		} else {
			starts = append(starts, el)
		}
	}
	slices.SortFunc(ends, netip.Addr.Compare)
	out := make([]enforce.Entry, 0, len(starts))
	for _, el := range starts {
		start, _ := netip.AddrFromSlice(el.key)
		p, err := netip.ParsePrefix(el.comment)
		if err != nil || p.Addr() != start || p != p.Masked() {
			if p, err = pair(start, ends); err != nil {
				return nil, err
			}
		}
		expires := never
		if el.timeout > 0 {
			if el.expires < minLeft {
				continue
			}
			expires = now.Add(el.expires)
		}
		out = append(out, enforce.Entry{Prefix: p, Expires: expires})
	}
	slices.SortFunc(out, func(a, b enforce.Entry) int {
		if c := a.Prefix.Addr().Compare(b.Prefix.Addr()); c != 0 {
			return c
		}
		return a.Prefix.Bits() - b.Prefix.Bits()
	})
	return out, nil
}

// pair returns the prefix from start to the lowest of the sorted ends
// above it.
func pair(start netip.Addr, ends []netip.Addr) (netip.Prefix, error) {
	i, found := slices.BinarySearchFunc(ends, start, netip.Addr.Compare)
	if found {
		i++ // an adjacent range ends where this one starts
	}
	var end netip.Addr
	if i < len(ends) {
		end = ends[i]
	}
	return toPrefix(start, end)
}

// toPrefix returns the prefix covering exactly [start, end); an invalid
// end means up to the top of the address space.
func toPrefix(start, end netip.Addr) (netip.Prefix, error) {
	for bits := start.BitLen(); bits >= 0; bits-- {
		p := netip.PrefixFrom(start, bits)
		if p.Masked().Addr() != start {
			break
		}
		if e, ok := rangeEnd(p); ok == end.IsValid() && (!ok || e == end) {
			return p, nil
		}
	}
	if !end.IsValid() {
		return netip.Prefix{}, fmt.Errorf("range %v- is not a single prefix", start)
	}
	return netip.Prefix{}, fmt.Errorf("range %v-%v is not a single prefix", start, end.Prev())
}
