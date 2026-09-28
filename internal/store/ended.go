package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// EndedRetention is how long the store keeps a verdict after its expiry:
// its record, so that the sweep can still archive it after a downtime, and
// the verdict once it ended, so that the operator can see it (ADR 0023).
const EndedRetention = 24 * time.Hour

// minMaxEnded is the least default of Options.MaxEnded.
const minMaxEnded = 1000

// EndedState is how a verdict ended.
type EndedState string

// States of an ended verdict.
const (
	// EndedRevoked: its publisher revoked it.
	EndedRevoked EndedState = "revoked"
	// EndedExpired: it reached its expiry unrevoked.
	EndedExpired EndedState = "expired"
)

// EndedStates lists the states of an ended verdict.
var EndedStates = []EndedState{EndedRevoked, EndedExpired}

// Revocation is the revocation that ended a verdict.
type Revocation struct {
	// ID is the revocation event's ID; Reason says why, e.g.
	// "false_positive"; At is when it was issued.
	ID     string    `json:"id"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// revocationOf returns the revocation that the revoke event ev is.
func revocationOf(ev *obieproto.Event) *Revocation {
	return &Revocation{ID: ev.ID, Reason: ev.Reason, At: ev.IssuedAt.Time}
}

// EndedVerdict is a verdict that was revoked or expired, kept until
// EndedRetention after its expiry.
type EndedVerdict struct {
	Event *obieproto.Event
	State EndedState
	// Revocation is the revocation of a revoked verdict; nil for an expired
	// one, and for one revoked before this version kept revocations.
	Revocation *Revocation
	// Cursor is its place in the order, for Page.After.
	Cursor string
}

// EndedFilter selects the ended verdicts of EndedVerdicts. Zero fields
// other than State match everything.
type EndedFilter struct {
	// State is the state of the verdicts listed; both states are counted.
	State EndedState
	// Key keeps the verdicts on the indicator with that key, Publisher
	// those of the publisher with that peer ID and Category those of that
	// category (see Category); Except leaves out those of the publisher
	// with that peer ID.
	Key, Publisher, Except, Category string
}

// matches reports whether the ended verdict with key suffix
// <indicator>\0<publisher>\0<category> is selected; the key is matched
// by the walk's prefix.
func (f *EndedFilter) matches(suffix []byte) bool {
	_, rest, _ := bytes.Cut(suffix, keySeparator)
	publisher, category, _ := bytes.Cut(rest, keySeparator)
	return (f.Publisher == "" || string(publisher) == f.Publisher) &&
		(f.Except == "" || string(publisher) != f.Except) &&
		(f.Category == "" || string(category) == f.Category)
}

// EndedPage is one page of EndedVerdicts.
type EndedPage struct {
	// Verdicts are ordered by indicator key, publisher and category.
	Verdicts []EndedVerdict
	// Next is the Page.After of the following page; empty on the last page.
	Next string
	// Total counts the ended verdicts the filter selects; Offset is the
	// position of the first of the page among them.
	Total, Offset int
	// States counts the ended verdicts the filter selects without its
	// State, by state.
	States map[EndedState]int
}

// EndedCount counts one publisher's ended verdicts.
type EndedCount struct {
	Revoked, Expired int
}

// add counts one more verdict that ended in state.
func (c *EndedCount) add(state EndedState) {
	if state == EndedRevoked {
		c.Revoked++
	} else {
		c.Expired++
	}
}

// of returns the count of state.
func (c EndedCount) of(state EndedState) int {
	if state == EndedRevoked {
		return c.Revoked
	}
	return c.Expired
}

// EndedTally counts the ended verdicts the store keeps.
type EndedTally struct {
	// ByPublisher counts them by publisher peer ID.
	ByPublisher map[string]EndedCount
	// Max is Options.MaxEnded; Full is set for a state in which the store
	// keeps that many of other publishers' ended verdicts, and so keeps no
	// more of them until older ones are forgotten.
	Max  int
	Full map[EndedState]bool
}

// endedTally counts the ended verdicts the store keeps, by publisher and
// state: set by every recount, raised by every verdict kept since. Badger's
// TTL forgets ended verdicts silently, so a count may stay too high until
// the next sweep recounts.
type endedTally struct {
	self string
	mu   sync.Mutex
	// byPublisher counts them by publisher; others counts those of other
	// publishers than self, by state, which the cap applies to.
	byPublisher map[string]EndedCount
	others      EndedCount
}

// keptEnded is a verdict kept once it ended: its publisher and state.
type keptEnded struct {
	publisher string
	state     EndedState
}

// reset replaces the counts with those of a recount.
func (t *endedTally) reset(counts map[string]EndedCount) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.byPublisher, t.others = counts, EndedCount{}
	for p, c := range counts {
		if p != t.self {
			t.others.Revoked += c.Revoked
			t.others.Expired += c.Expired
		}
	}
	t.publish()
}

// add counts the verdicts kept.
func (t *endedTally) add(kept []keptEnded) {
	if len(kept) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byPublisher == nil {
		t.byPublisher = map[string]EndedCount{}
	}
	for _, k := range kept {
		c := t.byPublisher[k.publisher]
		c.add(k.state)
		t.byPublisher[k.publisher] = c
		if k.publisher != t.self {
			t.others.add(k.state)
		}
	}
	t.publish()
}

// othersOf returns how many ended verdicts of other publishers in state
// the store keeps.
func (t *endedTally) othersOf(state EndedState) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.others.of(state)
}

// snapshot returns the counts with Max and Full for the cap limit.
func (t *endedTally) snapshot(limit int) EndedTally {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := EndedTally{ByPublisher: make(map[string]EndedCount, len(t.byPublisher)), Max: limit,
		Full: make(map[EndedState]bool, len(EndedStates))}
	for p, c := range t.byPublisher {
		if c != (EndedCount{}) {
			out.ByPublisher[p] = c
		}
	}
	for _, state := range EndedStates {
		out.Full[state] = t.others.of(state) >= limit
	}
	return out
}

// publish sets the ended verdicts gauge. Callers hold t.mu.
func (t *endedTally) publish() {
	var all EndedCount
	for _, c := range t.byPublisher {
		all.Revoked += c.Revoked
		all.Expired += c.Expired
	}
	for _, state := range EndedStates {
		endedGauge.WithLabelValues(string(state)).Set(float64(all.of(state)))
	}
}

// Category names what a verdict is about: its evidence reason and the
// attacked protocol, e.g. "password_bruteforce/ssh" (ADR 0022). Neither
// may contain a slash, so the name is unambiguous.
func Category(reason, protocol string) string {
	if protocol == "" {
		return reason
	}
	return reason + "/" + protocol
}

// categoryOf returns the category of the verdict ev.
func categoryOf(ev *obieproto.Event) string {
	var reason string
	if ev.Evidence != nil {
		reason = ev.Evidence.Reason
	}
	return Category(reason, ev.Protocol)
}

// archive keeps the verdict of rec as one that ended in state, with the
// revocation rev that ended it, until EndedRetention after its expiry, and
// adds it to kept unless it replaces an earlier verdict of the same
// publisher and category on the same indicator that ended the same way.
// Once the store keeps Options.MaxEnded of other publishers' verdicts that
// ended in state, it keeps no more of them, so a flood of short-lived
// verdicts cannot fill the disk; this node's own are always kept
// (ADR 0023).
func (s *DB) archive(txn *badger.Txn, rec *record, state EndedState, rev *Revocation, kept *[]keptEnded) error {
	ev := rec.Event
	publisher := ev.Publisher.PeerID
	key := endedKey(state, ev.Key(), publisher, categoryOf(ev))
	_, err := txn.Get(key)
	replaces := err == nil
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	own := s.opts.Self != "" && publisher == s.opts.Self
	if !own && !replaces && s.endedCounts.othersOf(state)+pendingOthers(*kept, s.opts.Self, state) >= s.opts.MaxEnded {
		s.warnEndedFull(state)
		return nil
	}
	data, err := json.Marshal(&record{Event: ev, Revoked: state == EndedRevoked, Revocation: rev})
	if err != nil {
		return err
	}
	e := badger.NewEntry(key, data)
	e.ExpiresAt = badgerExpiry(ev.ExpiresAt().Add(EndedRetention))
	if err := txn.SetEntry(e); err != nil {
		return err
	}
	if !replaces {
		*kept = append(*kept, keptEnded{publisher: publisher, state: state})
	}
	return nil
}

// pendingOthers counts the verdicts of other publishers than self in kept
// that ended in state.
func pendingOthers(kept []keptEnded, self string, state EndedState) int {
	n := 0
	for _, k := range kept {
		if k.publisher != self && k.state == state {
			n++
		}
	}
	return n
}

// archiveExpired keeps the verdict of rec as expired, adding it to kept, if
// it reached its expiry at now unrevoked; a revoked one was kept when it
// was revoked.
func (s *DB) archiveExpired(txn *badger.Txn, rec *record, now time.Time, kept *[]keptEnded) error {
	if rec.Revoked || !rec.Event.Expired(now) {
		return nil
	}
	return s.archive(txn, rec, EndedExpired, nil, kept)
}

// warnEndedFull logs that the store keeps the most of other publishers'
// verdicts that ended in state, once until a recount finds fewer than 90%
// of it again.
func (s *DB) warnEndedFull(state EndedState) {
	if !s.endedFull.Swap(true) {
		s.log.Warn("the store keeps the most verdicts of other publishers that ended; newer ones are not kept until "+
			"older ones are forgotten", "state", string(state), "max_ended", s.opts.MaxEnded)
	}
}

// countEnded counts the ended verdicts kept at the store's time, by
// publisher.
func (s *DB) countEnded(txn *badger.Txn) (map[string]EndedCount, error) {
	counts := map[string]*EndedCount{}
	err := s.walkEnded(txn, "", func(state EndedState, suffix []byte, _ *badger.Item) error {
		_, rest, _ := bytes.Cut(suffix, keySeparator)
		publisher, _, _ := bytes.Cut(rest, keySeparator)
		c := counts[string(publisher)]
		if c == nil {
			c = &EndedCount{}
			counts[string(publisher)] = c
		}
		c.add(state)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]EndedCount, len(counts))
	for publisher, c := range counts {
		out[publisher] = *c
	}
	return out, nil
}

// recountEnded counts the ended verdicts kept again: Badger's TTL forgets
// them without telling, so the sweep recounts them for the cap and the
// totals to follow. Verdicts kept while it counts are counted from the next
// sweep on.
func (s *DB) recountEnded() error {
	var counts map[string]EndedCount
	err := s.view(func(txn *badger.Txn) error {
		var err error
		counts, err = s.countEnded(txn)
		return err
	})
	if err != nil {
		return err
	}
	s.endedCounts.reset(counts)
	if limit := s.opts.MaxEnded / 10 * 9; s.endedCounts.othersOf(EndedRevoked) < limit &&
		s.endedCounts.othersOf(EndedExpired) < limit {
		s.endedFull.Store(false)
	}
	return nil
}

// cursorOf returns the cursor of an ended verdict's key suffix: its
// indicator key, publisher and category, separated by commas, which none
// of them holds.
func cursorOf(suffix []byte) string {
	return strings.ReplaceAll(string(suffix), "\x00", ",")
}

// EndedVerdicts returns the ended verdicts f selects, in the order of
// their keys, one page at a time (ADR 0023). It walks the keys of the
// ended verdicts, which hold everything f filters by, counting the
// matches of both states, and decodes only the page.
func (s *DB) EndedVerdicts(f EndedFilter, page Page) (EndedPage, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	limit = min(limit, MaxPageLimit)
	after := []byte(strings.ReplaceAll(page.After, ",", "\x00"))
	out := EndedPage{States: make(map[EndedState]int, len(EndedStates))}
	err := s.view(func(txn *badger.Txn) error {
		return s.walkEnded(txn, f.Key, func(state EndedState, suffix []byte, item *badger.Item) error {
			if !f.matches(suffix) {
				return nil
			}
			out.States[state]++
			if state != f.State {
				return nil
			}
			out.Total++
			switch {
			case page.After != "" && bytes.Compare(suffix, after) <= 0:
				out.Offset++
			case len(out.Verdicts) < limit:
				rec, err := decodeRecord(item)
				if err != nil {
					return err
				}
				out.Verdicts = append(out.Verdicts, EndedVerdict{Event: rec.Event, State: state, Revocation: rec.Revocation,
					Cursor: cursorOf(suffix)})
			case out.Next == "":
				out.Next = out.Verdicts[limit-1].Cursor
			}
			return nil
		})
	})
	if err != nil {
		return EndedPage{}, err
	}
	return out, nil
}

// EndedCounts counts the ended verdicts the store keeps by publisher, as
// of the last sweep and with every one kept since, without reading the
// database; a verdict forgotten since the last sweep still counts.
func (s *DB) EndedCounts() (EndedTally, error) {
	s.mu.RLock()
	open := s.db != nil
	s.mu.RUnlock()
	if !open {
		return EndedTally{}, ErrClosed
	}
	return s.endedCounts.snapshot(s.opts.MaxEnded), nil
}

// walkEnded calls fn with the key suffix and the item of every ended
// verdict still kept at the store's time, revoked ones first, in the order
// of their keys; only those on the indicator with key, unless it is empty.
// Values are read only when fn asks for them.
func (s *DB) walkEnded(txn *badger.Txn, key string, fn func(state EndedState, suffix []byte, item *badger.Item) error) error {
	now := s.opts.Now().Unix()
	for _, state := range EndedStates {
		base := endedPrefix(state)
		prefix := base
		if key != "" {
			prefix = join(base, []byte(key), keySeparator)
		}
		err := func() error {
			it := txn.NewIterator(badger.IteratorOptions{Prefix: prefix})
			defer it.Close()
			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				if int64(item.ExpiresAt()) <= now { // #nosec G115 -- Unix seconds written from an int64.
					continue // kept past its retention by the wall clock only
				}
				if err := fn(state, item.Key()[len(base):], item); err != nil {
					return err
				}
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}
	return nil
}
