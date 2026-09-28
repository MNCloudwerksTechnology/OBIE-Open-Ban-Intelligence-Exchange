package store

import (
	"bytes"
	"errors"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// sweepBatchSize bounds the expiry index entries handled per transaction,
// well below Badger's transaction size limit (15% of the 16 MiB memtable):
// each may archive an event of up to 4 KiB (ADR 0023).
const sweepBatchSize = 250

// Sweep removes the verdicts and overrides that expired at now and notifies
// subscribers of the indicators whose active verdicts or override changed.
// It runs periodically while the store is started; tests may call it
// directly.
func (s *DB) Sweep(now time.Time) error {
	for {
		changes, more, err := s.sweepBatch(now)
		s.notify(changes)
		if err != nil {
			return err
		}
		if !more {
			return s.recountEnded()
		}
	}
}

// sweepBatch expires up to sweepBatchSize index entries in one transaction
// and reports whether more are due.
func (s *DB) sweepBatch(now time.Time) (changes []Change, more bool, err error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	removed := 0
	var kept []keptEnded
	err = s.update(func(txn *badger.Txn) error {
		due, dueMore := dueExpiries(txn, now)
		more = dueMore
		seen := make(map[string]bool)
		for _, key := range due {
			if _, target := parseExpiryKey(key); bytes.HasPrefix(target, prefixVerdict) {
				removed++
			}
			c, err := s.expire(txn, key, &kept)
			if errors.Is(err, errCorrupt) {
				// Drop the index entry, so one bad value cannot stall expiry.
				s.log.Error("dropping expiry of undecodable entry", "key", string(key[expiryKeyHeader:]), "error", err)
				continue
			}
			if err != nil {
				return err
			}
			if c.Key != "" && !seen[c.Key] {
				seen[c.Key] = true
				changes = append(changes, c)
			}
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	s.addVerdicts(-removed)
	s.endedCounts.add(kept)
	return changes, more, nil
}

// dueExpiries returns up to sweepBatchSize expiry index keys due at now, in
// expiry order, and whether more are due.
func dueExpiries(txn *badger.Txn, now time.Time) ([][]byte, bool) {
	it := txn.NewIterator(badger.IteratorOptions{Prefix: prefixExpiry})
	defer it.Close()
	var due [][]byte
	for it.Rewind(); it.Valid(); it.Next() {
		if at, _ := parseExpiryKey(it.Item().Key()); at.After(now) {
			return due, false
		}
		if len(due) == sweepBatchSize {
			return due, true
		}
		due = append(due, it.Item().KeyCopy(nil))
	}
	return due, false
}

// expire removes an expiry index entry and the verdict or override it points
// to, if that still expires at the indexed time, adding an expired verdict
// kept to kept. It returns the change to notify, with an empty Key if
// nothing active changed.
func (s *DB) expire(txn *badger.Txn, indexKey []byte, kept *[]keptEnded) (Change, error) {
	if err := txn.Delete(indexKey); err != nil {
		return Change{}, err
	}
	at, target := parseExpiryKey(indexKey)
	switch {
	case bytes.HasPrefix(target, prefixVerdict):
		return s.expireVerdict(txn, target, at, kept)
	case bytes.HasPrefix(target, prefixOverride):
		return expireOverride(txn, target, at)
	}
	return Change{}, nil
}

func (s *DB) expireVerdict(txn *badger.Txn, key []byte, at time.Time, kept *[]keptEnded) (Change, error) {
	change := Change{Key: indicatorOfVerdictKey(key), Reason: ReasonExpiry}
	rec, err := getRecord(txn, key)
	switch {
	case errors.Is(err, ErrNotFound):
		// Badger's TTL removed the record already.
		return change, nil
	case err != nil:
		return Change{}, err
	case rec.Event.ExpiresAt().Unix() != at.Unix():
		// Stale index entry; the record's own entry handles it.
		return Change{}, nil
	case rec.Revoked:
		// Nothing active changes; the verdict was kept as revoked.
		return Change{}, txn.Delete(key)
	}
	if err := s.archive(txn, rec, EndedExpired, nil, kept); err != nil {
		return Change{}, err
	}
	return change, txn.Delete(key)
}

func expireOverride(txn *badger.Txn, key []byte, at time.Time) (Change, error) {
	change := Change{Key: string(key[len(prefixOverride):]), Reason: ReasonExpiry}
	o, err := getOverride(txn, key)
	switch {
	case errors.Is(err, ErrNotFound):
		return change, nil
	case err != nil:
		return Change{}, err
	case o.ExpiresAt.Unix() != at.Unix():
		return Change{}, nil
	}
	if err := archiveOverride(txn, &o); err != nil {
		return Change{}, err
	}
	return change, txn.Delete(key)
}
