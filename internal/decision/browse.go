package decision

import (
	"cmp"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unique"

	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Page sizes of Browse.
const (
	DefaultBrowseLimit = 50
	MaxBrowseLimit     = 500
)

// Sort is an order of Browse (ADR 0022). Ties are ordered by indicator
// key.
type Sort string

// Orders of Browse, each in its natural direction.
const (
	// SortDecided: the last decided first (the default).
	SortDecided Sort = "decided"
	// SortAddress: IPv4 before IPv6, then by address, a network before
	// the addresses in it.
	SortAddress Sort = "address"
	// SortState: blocks, then allowed, then none.
	SortState Sort = "state"
	// SortScore: the highest score first.
	SortScore Sort = "score"
	// SortPublishers: the most contributing publishers first.
	SortPublishers Sort = "publishers"
	// SortExpires: the soonest expiry first; decisions without one last.
	SortExpires Sort = "expires"
)

// Sorts lists the orders of Browse.
var Sorts = []Sort{SortDecided, SortAddress, SortState, SortScore, SortPublishers, SortExpires}

// Query selects and orders the kept decisions for Browse. The zero value
// lists the first page of every decision, the last decided first.
type Query struct {
	// State, if set, keeps the decisions in that state only.
	State State
	// Category, if set, keeps the decisions holding an active verdict of
	// that category (see Category).
	Category string
	// Publisher, if set, keeps the decisions holding an active verdict of
	// the publisher with that peer ID.
	Publisher string
	// Overlapping, if valid, keeps the decisions whose address range
	// overlaps it: for an address, its own decision and those of the
	// networks that contain it; for a network, also everything inside it.
	Overlapping netip.Prefix
	// Where, if set, keeps the decisions whose address range it reports
	// true for, e.g. those the firewall applies. It runs under the
	// engine's read lock, so it must be fast and must not call the
	// Engine.
	Where func(p netip.Prefix) bool
	Sort  Sort
	// After selects the page after the item with that cursor, Before the
	// page before it, Last the last page; otherwise the first page. A
	// cursor of another order is ignored.
	After, Before string
	Last          bool
	// Limit is the page size: DefaultBrowseLimit if 0, at most
	// MaxBrowseLimit.
	Limit int
}

// Item is a kept decision on a page of Browse.
type Item struct {
	// Decision is the kept decision, without Publishers.
	Decision Decision
	// Categories are the categories of its active verdicts, those of the
	// most counting verdicts first.
	Categories []string
	// Verdicts counts its active verdicts.
	Verdicts int
	// Cursor is the item's place in the order, for Query.After and
	// Query.Before.
	Cursor string
}

// BrowsePage is a page of the kept decisions.
type BrowsePage struct {
	Items []Item
	// Total counts the decisions the query selects; Offset is the
	// position of the first item among them. More pages precede it if
	// Offset > 0, and follow it if Offset + len(Items) < Total.
	Total, Offset int
	// States counts the decisions the query selects without its State, by
	// state.
	States map[State]int
	// Generation is the engine's Generation when the page was read.
	Generation uint64
}

// sortKey is what an order compares of a kept decision: its key and its
// sort value, smaller first — the address range for SortAddress, the
// score negated for SortScore, n for the others. Looking at a decision
// allocates nothing; only the decisions the selection keeps are copied.
type sortKey struct {
	key    string
	prefix netip.Prefix
	score  float64
	n      int64
}

// noExpiry is the sort value of a decision without expiry: after all.
const noExpiry = math.MaxInt64

// keyOf returns the sort key of the kept decision k in the order s.
func (s Sort) keyOf(k *keptDecision) sortKey {
	sk := sortKey{key: k.key}
	switch s {
	case SortAddress:
		sk.prefix = k.prefix
	case SortState:
		sk.n = int64(stateRank(k.d.State))
	case SortScore:
		sk.score = -k.d.Score
	case SortPublishers:
		sk.n = -int64(k.d.Contributors)
	case SortExpires:
		sk.n = noExpiry
		if !k.d.ExpiresAt.IsZero() {
			sk.n = k.d.ExpiresAt.UnixNano()
		}
	default:
		sk.n = -k.d.EvaluatedAt.UnixNano()
	}
	return sk
}

// browser is one pass of Browse over the kept decisions.
type browser struct {
	q     Query
	order Sort
	// after and before are the cursors' places, if hasAfter and hasBefore.
	after, before       sortKey
	hasAfter, hasBefore bool
	category, publisher unique.Handle[string]
	sel                 selection
	// states counts the matches by state (stateRank), total those in the
	// query's state, and preceding those before the page.
	states           [3]int
	total, preceding int
}

// Browse selects, orders and pages the kept decisions in one pass under
// the engine's read lock, keeping only the page in a bounded selection:
// O(n log limit) time, O(limit) memory, whichever page (ADR 0022). An
// Overlapping address is looked up instead of scanned for.
func (e *Engine) Browse(q Query) BrowsePage {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultBrowseLimit
	}
	limit = min(limit, MaxBrowseLimit)
	order := q.Sort
	if !slices.Contains(Sorts, order) {
		order = SortDecided
	}
	br := browser{q: q, order: order}
	br.after, br.hasAfter = order.parseCursor(q.After)
	br.before, br.hasBefore = order.parseCursor(q.Before)
	// The page after a cursor and the first page keep the smallest items,
	// the page before a cursor and the last page the largest.
	br.sel = selection{limit: limit, order: order, largest: !br.hasAfter && (br.hasBefore || q.Last),
		items: make([]selected, 0, limit)}
	if q.Category != "" {
		br.category = unique.Make(q.Category)
	}
	if q.Publisher != "" {
		br.publisher = unique.Make(q.Publisher)
	}

	// An address overlaps only the ranges that contain it: look them up.
	var covering []string
	if p := q.Overlapping; p.IsValid() && p.Bits() == p.Addr().BitLen() {
		covering = coveringKeys(p)
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if covering != nil {
		for _, key := range covering {
			if k, ok := e.kept.get(key); ok {
				br.visit(k)
			}
		}
	} else {
		// New decisions are appended: with the last decided first, going
		// backwards fills the selection with the best at once, and it
		// rejects most of the rest with one comparison each.
		backwards := order == SortDecided && !br.sel.largest
		items := e.kept.items
		for i := range items {
			if backwards {
				i = len(items) - 1 - i
			}
			br.visit(&items[i])
		}
	}

	page := BrowsePage{Total: br.total, States: make(map[State]int, len(States)), Generation: e.generation.Load()}
	for _, s := range States {
		if n := br.states[stateRank(s)]; n > 0 {
			page.States[s] = n
		}
	}
	items := br.sel.sorted()
	switch {
	case br.hasAfter:
		page.Offset = br.preceding
	case br.hasBefore:
		page.Offset = br.preceding - len(items)
	case q.Last:
		page.Offset = page.Total - len(items)
	}
	page.Items = make([]Item, len(items))
	for i := range items {
		page.Items[i] = items[i].item(order)
	}
	return page
}

// visit looks at the kept decision k. Callers hold the engine's mu.
func (br *browser) visit(k *keptDecision) {
	if !k.holds(br.category, br.publisher) {
		return
	}
	if br.q.Overlapping.IsValid() && !k.prefix.Overlaps(br.q.Overlapping) {
		return
	}
	if br.q.Where != nil && !br.q.Where(k.prefix) {
		return
	}
	br.states[stateRank(k.d.State)]++
	if br.q.State != "" && k.d.State != br.q.State {
		return
	}
	br.total++
	sk := br.order.keyOf(k)
	switch {
	case br.hasAfter && br.order.compare(&sk, &br.after) <= 0:
		br.preceding++
		return
	case !br.hasAfter && br.hasBefore && br.order.compare(&sk, &br.before) >= 0:
		return
	case !br.hasAfter && br.hasBefore:
		br.preceding++
	}
	br.sel.add(&sk, k)
}

// holds reports whether k holds an active verdict of category and of
// publisher; a zero handle matches any.
func (k *keptDecision) holds(category, publisher unique.Handle[string]) bool {
	var zero unique.Handle[string]
	if category == zero && publisher == zero {
		return true
	}
	return (category == zero || slices.ContainsFunc(k.held, func(h heldVerdict) bool { return h.category == category })) &&
		(publisher == zero || slices.ContainsFunc(k.held, func(h heldVerdict) bool { return h.publisher == publisher }))
}

// item describes the selected decision for a page in order. Callers hold
// the engine's mu.
func (s *selected) item(order Sort) Item {
	held := s.k.held
	it := Item{Decision: s.k.d, Verdicts: len(held), Cursor: order.cursor(&s.key)}
	type tally struct {
		name             string
		counting, active int
	}
	var tallies []tally
	for _, h := range held {
		i := slices.IndexFunc(tallies, func(t tally) bool { return t.name == h.category.Value() })
		if i < 0 {
			tallies = append(tallies, tally{name: h.category.Value()})
			i = len(tallies) - 1
		}
		tallies[i].active++
		if h.counts {
			tallies[i].counting++
		}
	}
	slices.SortFunc(tallies, func(a, b tally) int {
		return cmp.Or(cmp.Compare(b.counting, a.counting), cmp.Compare(b.active, a.active), strings.Compare(a.name, b.name))
	})
	for _, t := range tallies {
		it.Categories = append(it.Categories, t.name)
	}
	return it
}

// Lookup calls fn with the index and the kept decision of every key in
// keys the engine keeps a decision on, under one read lock: for many
// keys, e.g. those of every firewall entry. fn must be fast and must not
// call the Engine.
func (e *Engine) Lookup(keys []string, fn func(i int, d *Decision)) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for i, key := range keys {
		if k, ok := e.kept.get(key); ok {
			fn(i, &k.d)
		}
	}
}

// Covering returns the kept decisions on the networks that contain the
// address range p, p's own aside, the widest first, without Publishers.
// It looks them up by key: at most 16 for IPv4, 96 for IPv6.
func (e *Engine) Covering(p netip.Prefix) []Decision {
	keys := coveringKeys(p)
	if len(keys) < 2 {
		return nil
	}
	var out []Decision
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, key := range slices.Backward(keys[1:]) {
		if k, ok := e.kept.get(key); ok {
			out = append(out, k.d)
		}
	}
	return out
}

// coveringKeys returns the keys of the indicators whose range contains
// the range p: p's own first, then every CIDR range around it that an
// indicator may name, the narrowest first. None for an invalid p.
func coveringKeys(p netip.Prefix) []string {
	if !p.IsValid() {
		return nil
	}
	addr := p.Masked().Addr()
	kind, minBits := obieproto.KindIPv6, obieproto.MinIPv6Prefix
	if addr.Is4() {
		kind, minBits = obieproto.KindIPv4, obieproto.MinIPv4Prefix
	}
	var keys []string
	if p.Bits() == addr.BitLen() {
		keys = append(keys, obieproto.Indicator{Kind: kind, Value: addr.String()}.Key())
	} else {
		keys = append(keys, obieproto.Indicator{Kind: obieproto.KindCIDR, Value: p.Masked().String()}.Key())
	}
	for bits := min(p.Bits(), addr.BitLen()) - 1; bits >= minBits; bits-- {
		keys = append(keys, obieproto.Indicator{Kind: obieproto.KindCIDR, Value: netip.PrefixFrom(addr, bits).Masked().String()}.Key())
	}
	return keys
}

// compare orders a before b in the order s, then by key.
func (s Sort) compare(a, b *sortKey) int {
	var c int
	switch s {
	case SortAddress:
		c = cmp.Or(a.prefix.Addr().Compare(b.prefix.Addr()), cmp.Compare(a.prefix.Bits(), b.prefix.Bits()))
	case SortScore:
		c = cmp.Compare(a.score, b.score)
	default:
		c = cmp.Compare(a.n, b.n)
	}
	if c != 0 {
		return c
	}
	return strings.Compare(a.key, b.key)
}

// stateRank orders blocks first, then allowed, then none.
func stateRank(s State) int {
	switch s {
	case StateBlock:
		return 0
	case StateAllowed:
		return 1
	default:
		return 2
	}
}

// cursor returns the cursor of k in the order s: the order, the sort
// value and the key, e.g. "decided:<unix nanoseconds>,<key>".
func (s Sort) cursor(k *sortKey) string {
	var value string
	switch s {
	case SortAddress:
	case SortScore:
		value = strconv.FormatFloat(-k.score, 'g', -1, 64)
	case SortPublishers, SortDecided:
		value = strconv.FormatInt(-k.n, 10)
	case SortExpires:
		if k.n != noExpiry {
			value = strconv.FormatInt(k.n, 10)
		}
	default:
		value = strconv.FormatInt(k.n, 10)
	}
	return string(s) + ":" + value + "," + k.key
}

// parseCursor returns the place a cursor of the order s stands for, and
// whether it is one.
func (s Sort) parseCursor(cursor string) (sortKey, bool) {
	order, rest, ok := strings.Cut(cursor, ":")
	if !ok || Sort(order) != s {
		return sortKey{}, false
	}
	value, key, ok := strings.Cut(rest, ",")
	if !ok || key == "" {
		return sortKey{}, false
	}
	k := sortKey{key: key}
	var err error
	switch s {
	case SortAddress:
		k.prefix, err = prefixOfKey(key)
	case SortScore:
		k.score, err = strconv.ParseFloat(value, 64)
		k.score = -k.score
	case SortExpires:
		k.n = noExpiry
		if value != "" {
			k.n, err = strconv.ParseInt(value, 10, 64)
		}
	default:
		k.n, err = strconv.ParseInt(value, 10, 64)
		if s == SortPublishers || s == SortDecided {
			k.n = -k.n
		}
	}
	return k, err == nil
}

// prefixOfKey returns the address range of the indicator with key.
func prefixOfKey(key string) (netip.Prefix, error) {
	kind, value, _ := strings.Cut(key, ":")
	return sovereignty.PrefixOf(obieproto.Indicator{Kind: kind, Value: value})
}

// selected is a decision the selection keeps: its sort key and the kept
// decision, valid while the engine's read lock is held.
type selected struct {
	key sortKey
	k   *keptDecision
}

// selection keeps the limit smallest decisions (or, with largest, the
// largest) of those added, in a heap whose root is the one to drop first:
// O(log limit) per decision, whatever order they come in.
type selection struct {
	limit   int
	order   Sort
	largest bool
	items   []selected
}

// add offers the kept decision k with sort key sk to the selection.
func (s *selection) add(sk *sortKey, k *keptDecision) {
	if len(s.items) < s.limit {
		s.items = append(s.items, selected{key: *sk, k: k})
		s.up(len(s.items) - 1)
		return
	}
	if !s.worse(&s.items[0].key, sk) {
		return
	}
	s.items[0] = selected{key: *sk, k: k}
	s.down(0)
}

// sorted returns the kept decisions in order.
func (s *selection) sorted() []selected {
	slices.SortFunc(s.items, func(a, b selected) int { return s.order.compare(&a.key, &b.key) })
	return s.items
}

// worse reports whether a is dropped before b.
func (s *selection) worse(a, b *sortKey) bool {
	if s.largest {
		return s.order.compare(a, b) < 0
	}
	return s.order.compare(a, b) > 0
}

func (s *selection) up(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if !s.worse(&s.items[i].key, &s.items[parent].key) {
			return
		}
		s.items[i], s.items[parent] = s.items[parent], s.items[i]
		i = parent
	}
}

func (s *selection) down(i int) {
	for {
		worst := i
		if l := 2*i + 1; l < len(s.items) && s.worse(&s.items[l].key, &s.items[worst].key) {
			worst = l
		}
		if r := 2*i + 2; r < len(s.items) && s.worse(&s.items[r].key, &s.items[worst].key) {
			worst = r
		}
		if worst == i {
			return
		}
		s.items[i], s.items[worst] = s.items[worst], s.items[i]
		i = worst
	}
}
