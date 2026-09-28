package decision

import (
	"cmp"
	"slices"
	"strings"
	"unique"
)

// VerdictQuery selects the active verdicts of the kept decisions for
// Verdicts (ADR 0023). The zero value lists the first page of every active
// verdict.
type VerdictQuery struct {
	// Publisher, if set, keeps the verdicts of the publisher with that peer
	// ID; Except, if set, leaves out those of the publisher with that peer
	// ID, e.g. this node's own.
	Publisher, Except string
	// Category, if set, keeps the verdicts of that category (see Category).
	Category string
	// Key, if set, keeps the verdicts on the indicator with that key; it is
	// looked up, not scanned for.
	Key string
	// After selects the page after the verdict with that cursor; otherwise
	// the first page.
	After string
	// Limit is the page size: DefaultBrowseLimit if 0, at most
	// MaxBrowseLimit.
	Limit int
}

// ActiveVerdict is an active verdict of a kept decision.
type ActiveVerdict struct {
	// Key is the key of its indicator, Publisher the peer ID of its
	// publisher and Category what it is about (see Category).
	Key, Publisher, Category string
	// Counts is set if it counts in the decision: a ban verdict of a
	// publisher whose trust weight is above 0.
	Counts bool
	// State is the state of the decision.
	State State
	// Cursor is its place in the order, for VerdictQuery.After: its key and
	// its publisher, separated by a comma, which neither holds.
	Cursor string
}

// VerdictPage is a page of the active verdicts.
type VerdictPage struct {
	// Verdicts are ordered by indicator key, then by publisher.
	Verdicts []ActiveVerdict
	// Total counts the verdicts the query selects; Offset is the position
	// of the first of the page among them.
	Total, Offset int
}

// Verdicts selects and pages the active verdicts of the kept decisions,
// ordered by indicator key and publisher like the store's keys, in one pass
// under the engine's read lock that keeps only the page in a bounded
// selection: O(n log limit) time, O(limit) memory (ADR 0023).
func (e *Engine) Verdicts(q VerdictQuery) VerdictPage {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultBrowseLimit
	}
	limit = min(limit, MaxBrowseLimit)
	vb := verdictBrowser{sel: verdictSelection{limit: limit, items: make([]verdictSelected, 0, limit)}}
	if q.After != "" {
		key, rest, _ := strings.Cut(q.After, ",")
		publisher, _, _ := strings.Cut(rest, ",")
		vb.after, vb.hasAfter = verdictSelected{key: key, publisher: publisher}, true
	}
	for _, f := range []struct {
		value string
		h     *unique.Handle[string]
	}{{q.Publisher, &vb.publisher}, {q.Except, &vb.except}, {q.Category, &vb.category}} {
		if f.value != "" {
			*f.h = unique.Make(f.value)
		}
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	if q.Key != "" {
		if k, ok := e.kept.get(q.Key); ok {
			vb.visit(k)
		}
	} else {
		for i := range e.kept.items {
			vb.visit(&e.kept.items[i])
		}
	}
	page := VerdictPage{Total: vb.total, Offset: vb.preceding, Verdicts: make([]ActiveVerdict, len(vb.sel.items))}
	slices.SortFunc(vb.sel.items, compareVerdicts)
	for i, s := range vb.sel.items {
		page.Verdicts[i] = ActiveVerdict{Key: s.key, Publisher: s.publisher, Category: s.h.category.Value(), Counts: s.h.counts,
			State: s.state, Cursor: s.key + "," + s.publisher}
	}
	return page
}

// verdictBrowser is one pass of Verdicts over the kept decisions.
type verdictBrowser struct {
	// publisher, except and category are the query's filters; a zero handle
	// is none.
	publisher, except, category unique.Handle[string]
	// after is the cursor's place, if hasAfter.
	after    verdictSelected
	hasAfter bool
	sel      verdictSelection
	// total counts the matches, preceding those before the page.
	total, preceding int
}

// visit looks at the active verdicts of the kept decision k. Callers hold
// the engine's mu. Most decisions lie wholly before the cursor or beyond
// the selection: their verdicts are only counted.
func (vb *verdictBrowser) visit(k *keptDecision) {
	before := vb.hasAfter && k.key < vb.after.key
	beyond := !before && len(vb.sel.items) == vb.sel.limit && k.key > vb.sel.items[0].key
	for _, h := range k.held {
		if !vb.matches(h) {
			continue
		}
		vb.total++
		if before {
			vb.preceding++
			continue
		}
		if beyond {
			continue
		}
		s := verdictSelected{key: k.key, publisher: h.publisher.Value(), h: h, state: k.d.State}
		if vb.hasAfter && compareVerdicts(s, vb.after) <= 0 {
			vb.preceding++
			continue
		}
		vb.sel.add(s)
	}
}

// matches reports whether the query selects the held verdict h.
func (vb *verdictBrowser) matches(h heldVerdict) bool {
	var zero unique.Handle[string]
	return (vb.publisher == zero || h.publisher == vb.publisher) && (vb.except == zero || h.publisher != vb.except) &&
		(vb.category == zero || h.category == vb.category)
}

// verdictSelected is an active verdict a selection keeps.
type verdictSelected struct {
	key, publisher string
	h              heldVerdict
	state          State
}

// compareVerdicts orders by indicator key, then by publisher.
func compareVerdicts(a, b verdictSelected) int {
	return cmp.Or(strings.Compare(a.key, b.key), strings.Compare(a.publisher, b.publisher))
}

// verdictSelection keeps the limit smallest verdicts of those added, in a
// heap whose root is the largest: O(log limit) per verdict.
type verdictSelection struct {
	limit int
	items []verdictSelected
}

// add offers s to the selection.
func (sel *verdictSelection) add(s verdictSelected) {
	if len(sel.items) < sel.limit {
		sel.items = append(sel.items, s)
		for i := len(sel.items) - 1; i > 0; {
			parent := (i - 1) / 2
			if compareVerdicts(sel.items[i], sel.items[parent]) <= 0 {
				return
			}
			sel.items[i], sel.items[parent] = sel.items[parent], sel.items[i]
			i = parent
		}
		return
	}
	if compareVerdicts(s, sel.items[0]) >= 0 {
		return
	}
	sel.items[0] = s
	for i := 0; ; {
		largest := i
		if l := 2*i + 1; l < len(sel.items) && compareVerdicts(sel.items[l], sel.items[largest]) > 0 {
			largest = l
		}
		if r := 2*i + 2; r < len(sel.items) && compareVerdicts(sel.items[r], sel.items[largest]) > 0 {
			largest = r
		}
		if largest == i {
			return
		}
		sel.items[i], sel.items[largest] = sel.items[largest], sel.items[i]
		i = largest
	}
}
