package store

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// EndedRetention is how long the store keeps a verdict after its expiry:
// its record, so that the sweep can still archive it after a downtime, and
// the verdict once it ended, so that the operator can see it (ADR 0023).
const EndedRetention = 24 * time.Hour

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
// revocation rev that ended it, until EndedRetention after its expiry. It
// replaces an earlier verdict of the same publisher and category on the
// same indicator that ended the same way.
func archive(txn *badger.Txn, rec *record, state EndedState, rev *Revocation) error {
	ended := record{Event: rec.Event, Revoked: state == EndedRevoked, Revocation: rev}
	data, err := json.Marshal(&ended)
	if err != nil {
		return err
	}
	ev := rec.Event
	e := badger.NewEntry(endedKey(state, ev.Key(), ev.Publisher.PeerID, categoryOf(ev)), data)
	e.ExpiresAt = badgerExpiry(ev.ExpiresAt().Add(EndedRetention))
	return txn.SetEntry(e)
}

// archiveExpired keeps the verdict of rec as expired if it reached its
// expiry at now unrevoked; a revoked one was kept when it was revoked.
func archiveExpired(txn *badger.Txn, rec *record, now time.Time) error {
	if rec.Revoked || !rec.Event.Expired(now) {
		return nil
	}
	return archive(txn, rec, EndedExpired, nil)
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

// EndedCounts counts the ended verdicts by publisher peer ID, in one walk
// over their keys.
func (s *DB) EndedCounts() (map[string]EndedCount, error) {
	counts := map[string]*EndedCount{}
	err := s.view(func(txn *badger.Txn) error {
		return s.walkEnded(txn, "", func(state EndedState, suffix []byte, _ *badger.Item) error {
			_, rest, _ := bytes.Cut(suffix, keySeparator)
			publisher, _, _ := bytes.Cut(rest, keySeparator)
			c := counts[string(publisher)]
			if c == nil {
				c = &EndedCount{}
				counts[string(publisher)] = c
			}
			if state == EndedRevoked {
				c.Revoked++
			} else {
				c.Expired++
			}
			return nil
		})
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
