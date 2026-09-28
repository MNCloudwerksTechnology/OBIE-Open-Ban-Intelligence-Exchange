package store

import (
	"bytes"
	"errors"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// maxEvictionsPerPut bounds the records one Put evicts. Above one, a store
// holding more than MaxIndicators records (e.g. after the cap was lowered)
// shrinks back to it.
const maxEvictionsPerPut = 2

// makeRoom reports whether a new verdict record for ev fits into the store.
// Below MaxIndicators records it does. At the cap, the record that expires
// first — never one of this node's own — is evicted, provided ev expires
// later or is this node's own; otherwise ev is refused. This node's own
// verdicts are stored even if nothing can be evicted (ADR 0017).
func (s *DB) makeRoom(txn *badger.Txn, ev *obieproto.Event, now time.Time, out *outcome) (bool, error) {
	own := s.opts.Self != "" && ev.Publisher.PeerID == s.opts.Self
	if s.verdicts.Load() < int64(s.opts.MaxIndicators)/10*9 {
		s.full.Store(false)
	}
	for s.verdicts.Load()+int64(out.verdicts) >= int64(s.opts.MaxIndicators) {
		if out.evicted == maxEvictionsPerPut {
			return true, nil
		}
		s.warnFull()
		index, at, ok := s.evictionCandidate(txn)
		switch {
		case !ok:
			return own, nil
		case !own && !at.Before(ev.ExpiresAt()):
			return out.evicted > 0, nil
		}
		change, err := s.evict(txn, index, now)
		if err != nil {
			return false, err
		}
		out.verdicts--
		out.evicted++
		out.evictedIndex = index
		if change.Key != "" {
			out.changes = append(out.changes, change)
		}
	}
	return true, nil
}

// warnFull logs that the store reached MaxIndicators, once until it drops
// below 90% of it again.
func (s *DB) warnFull() {
	if !s.full.Swap(true) {
		s.log.Warn("event store full; evicting the verdicts that expire first", "max_indicators", s.opts.MaxIndicators)
	}
}

// evictionCandidate returns the expiry index key of the verdict record
// that expires first, not counting this node's own, and its expiry. The
// search starts at evictFrom: every index entry below it was deleted (and
// may still be a tombstone Badger has to skip) or is this node's own.
func (s *DB) evictionCandidate(txn *badger.Txn) ([]byte, time.Time, bool) {
	it := txn.NewIterator(badger.IteratorOptions{Prefix: prefixExpiry})
	defer it.Close()
	start := prefixExpiry
	if bytes.Compare(s.evictFrom, start) > 0 {
		start = s.evictFrom
	}
	for it.Seek(start); it.Valid(); it.Next() {
		at, target := parseExpiryKey(it.Item().Key())
		if !bytes.HasPrefix(target, prefixVerdict) {
			continue // an override
		}
		if s.opts.Self != "" && publisherOfVerdictKey(target) == s.opts.Self {
			continue
		}
		return it.Item().KeyCopy(nil), at, true
	}
	return nil, time.Time{}, false
}

// evict removes an expiry index entry, the verdict record it points to and
// the record's event. The event's ID stays known until it expires, so a
// replay is a duplicate. It returns the change to notify, with an empty Key
// if nothing active changed.
func (s *DB) evict(txn *badger.Txn, index []byte, now time.Time) (Change, error) {
	if err := txn.Delete(index); err != nil {
		return Change{}, err
	}
	at, key := parseExpiryKey(index)
	rec, err := getRecord(txn, key)
	switch {
	case errors.Is(err, ErrNotFound):
		return Change{}, nil // already expired
	case errors.Is(err, errCorrupt):
		s.log.Error("evicting undecodable verdict", "key", string(key), "error", err)
		return Change{}, txn.Delete(key)
	case err != nil:
		return Change{}, err
	case rec.Event.ExpiresAt().Unix() != at.Unix():
		return Change{}, nil // stale index entry; the record has its own
	}
	if err := txn.Delete(key); err != nil {
		return Change{}, err
	}
	if err := txn.Delete(eventKey(rec.Event.ID)); err != nil {
		return Change{}, err
	}
	seen := badger.NewEntry(seenKey(rec.Event.ID), nil)
	seen.ExpiresAt = badgerExpiry(rec.Event.ExpiresAt())
	if err := txn.SetEntry(seen); err != nil {
		return Change{}, err
	}
	s.log.Debug("evicted verdict", "event", rec.Event.ID, "indicator", rec.Event.Key(),
		"publisher", rec.Event.Publisher.PeerID, "expires_at", rec.Event.ExpiresAt())
	if !rec.active(now) {
		return Change{}, nil
	}
	return Change{Key: rec.Event.Key(), Reason: ReasonEvict}, nil
}

// countVerdicts counts the verdict records through their expiry index
// entries.
func countVerdicts(txn *badger.Txn) int64 {
	it := txn.NewIterator(badger.IteratorOptions{Prefix: prefixExpiry})
	defer it.Close()
	var n int64
	for it.Rewind(); it.Valid(); it.Next() {
		if _, target := parseExpiryKey(it.Item().Key()); bytes.HasPrefix(target, prefixVerdict) {
			n++
		}
	}
	return n
}

// addVerdicts adds delta to the number of verdict records. Callers hold
// writeMu.
func (s *DB) addVerdicts(delta int) {
	verdictsGauge.Set(float64(s.verdicts.Add(int64(delta))))
}

// Verdicts returns the number of verdict records held, active, revoked or
// expired but not yet swept; MaxIndicators bounds it.
func (s *DB) Verdicts() int64 {
	return s.verdicts.Load()
}
