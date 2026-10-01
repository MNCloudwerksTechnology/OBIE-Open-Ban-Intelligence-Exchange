package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Get returns the event with the given ID; see Store.
func (s *DB) Get(id string) (*obieproto.Event, error) {
	var ev *obieproto.Event
	err := s.view(func(txn *badger.Txn) error {
		var err error
		ev, err = getEvent(txn, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	if ev.Expired(s.opts.Now()) {
		return nil, ErrNotFound
	}
	return ev, nil
}

// Seen reports whether an event ID was already put; see Store.
func (s *DB) Seen(id string) (bool, error) {
	seen := false
	err := s.view(func(txn *badger.Txn) error {
		for _, key := range [][]byte{eventKey(id), seenKey(id)} {
			_, err := txn.Get(key)
			switch {
			case err == nil:
				seen = true
				return nil
			case !errors.Is(err, badger.ErrKeyNotFound):
				return err
			}
		}
		return nil
	})
	return seen, err
}

// ActiveVerdicts returns the active verdicts on an indicator; see Store.
func (s *DB) ActiveVerdicts(indicatorKey string, now time.Time) ([]*obieproto.Event, error) {
	var out []*obieproto.Event
	err := s.view(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.IteratorOptions{Prefix: verdictPrefix(indicatorKey)})
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			rec, err := decodeRecord(it.Item())
			if err != nil {
				return err
			}
			if rec.active(now) {
				out = append(out, rec.Event)
			}
		}
		return nil
	})
	return out, err
}

// ListIndicators returns a page of indicators with active verdicts; see
// Store.
func (s *DB) ListIndicators(now time.Time, filter Filter, page Page) (IndicatorPage, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	limit = min(limit, MaxPageLimit)

	// Verdict keys start with the indicator key, which starts with the kind,
	// so a kind filter narrows the scanned range.
	prefix := prefixVerdict
	if filter.Kind != "" {
		prefix = join(prefixVerdict, []byte(filter.Kind+":"))
	}
	start := prefix
	if page.After != "" {
		// Every key of indicator After is below After + "\x01".
		if after := join(prefixVerdict, []byte(page.After), []byte{1}); string(after) > string(start) {
			start = after
		}
	}

	var out IndicatorPage
	err := s.view(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.IteratorOptions{Prefix: prefix, PrefetchValues: true, PrefetchSize: 100})
		defer it.Close()
		var cur *IndicatorState
		// flush adds cur to the page if it matches and reports whether the
		// scan should go on: it stops at the first match beyond the page.
		flush := func() bool {
			if cur == nil || !filter.matches(cur) {
				return true
			}
			if len(out.Items) == limit {
				out.Next = out.Items[limit-1].Key
				return false
			}
			out.Items = append(out.Items, *cur)
			return true
		}
		for it.Seek(start); it.Valid(); it.Next() {
			key := indicatorOfVerdictKey(it.Item().Key())
			if cur != nil && cur.Key != key {
				if !flush() {
					return nil
				}
				cur = nil
			}
			if cur == nil {
				cur = &IndicatorState{Key: key}
			}
			rec, err := decodeRecord(it.Item())
			if err != nil {
				return err
			}
			if rec.active(now) {
				cur.Indicator = rec.Event.Indicator
				cur.Verdicts = append(cur.Verdicts, rec.Event)
			}
		}
		flush()
		return nil
	})
	if err != nil {
		return IndicatorPage{}, err
	}
	return out, nil
}

// PublisherVerdicts returns the active verdicts of publisher at now,
// ordered by indicator key, one page at a time. It walks the verdict keys
// and decodes only the publisher's records (ADR 0021): it stops once the
// page is full, and walks all keys for a publisher with fewer verdicts.
func (s *DB) PublisherVerdicts(publisher string, now time.Time, page Page) (VerdictPage, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	limit = min(limit, MaxPageLimit)
	start := prefixVerdict
	if page.After != "" {
		// Every key of indicator After is below After + "\x01".
		start = join(prefixVerdict, []byte(page.After), []byte{1})
	}
	// Indicator keys hold no NUL byte, so a key of the publisher ends
	// with exactly this.
	suffix := join(keySeparator, []byte(publisher))

	var out VerdictPage
	err := s.view(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.IteratorOptions{Prefix: prefixVerdict})
		defer it.Close()
		for it.Seek(start); it.Valid(); it.Next() {
			if !bytes.HasSuffix(it.Item().Key(), suffix) {
				continue
			}
			rec, err := decodeRecord(it.Item())
			if err != nil {
				return err
			}
			if !rec.active(now) {
				continue
			}
			if len(out.Verdicts) == limit {
				out.Next = out.Verdicts[limit-1].Key()
				return nil
			}
			out.Verdicts = append(out.Verdicts, rec.Event)
		}
		return nil
	})
	if err != nil {
		return VerdictPage{}, err
	}
	return out, nil
}

// matches reports whether an indicator is listed under the filter.
func (f Filter) matches(st *IndicatorState) bool {
	if len(st.Verdicts) == 0 {
		return false
	}
	if f.Kind != "" && st.Indicator.Kind != f.Kind {
		return false
	}
	if f.Publisher == "" {
		return true
	}
	for _, v := range st.Verdicts {
		if v.Publisher.PeerID == f.Publisher {
			return true
		}
	}
	return false
}

func decodeRecord(item *badger.Item) (*record, error) {
	var rec record
	err := item.Value(func(data []byte) error {
		return json.Unmarshal(data, &rec)
	})
	if err != nil {
		return nil, fmt.Errorf("decode %q: %w", item.Key(), err)
	}
	return &rec, nil
}
