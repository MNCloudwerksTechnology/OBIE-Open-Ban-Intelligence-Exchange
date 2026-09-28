package decision

import (
	"net/netip"

	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// keptDecision is a decision the engine keeps, with what its passes over
// every decision need of it.
type keptDecision struct {
	key string
	// d is the decision, without Publishers.
	d Decision
	// prefix is the indicator's address range; zero if it has none.
	prefix netip.Prefix
	// held lists the decision's active verdicts (ADR 0021).
	held []heldVerdict
}

// keptSet holds the kept decisions in one slice, so that a pass over all
// of them reads memory in order instead of chasing a map's out-of-line
// values, and finds one by key through an index (ADR 0022). The zero value
// is empty. It is not safe for concurrent use.
type keptSet struct {
	items []keptDecision
	index map[string]int
}

// len returns the number of kept decisions.
func (s *keptSet) len() int { return len(s.items) }

// get returns the kept decision with key. The pointer is valid until the
// set changes.
func (s *keptSet) get(key string) (*keptDecision, bool) {
	i, ok := s.index[key]
	if !ok {
		return nil, false
	}
	return &s.items[i], true
}

// put keeps d with its held verdicts under key, replacing a kept one.
func (s *keptSet) put(key string, d Decision, held []heldVerdict) {
	if k, ok := s.get(key); ok {
		k.d, k.held = d, held
		return
	}
	if s.index == nil {
		s.index = map[string]int{}
	}
	prefix, _ := sovereignty.PrefixOf(d.Indicator)
	s.index[key] = len(s.items)
	s.items = append(s.items, keptDecision{key: key, d: d, prefix: prefix, held: held})
}

// remove drops the decision with key, moving the last one into its place.
func (s *keptSet) remove(key string) {
	i, ok := s.index[key]
	if !ok {
		return
	}
	last := len(s.items) - 1
	if i != last {
		s.items[i] = s.items[last]
		s.index[s.items[i].key] = i
	}
	s.items[last] = keptDecision{}
	s.items = s.items[:last]
	delete(s.index, key)
	// Give memory back once most decisions are gone, e.g. after a flood
	// expired.
	if c := cap(s.items); c > 1024 && len(s.items) < c/4 {
		s.items = append(make([]keptDecision, 0, 2*len(s.items)), s.items...)
	}
}
