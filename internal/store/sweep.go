package store

import (
	"bytes"
	"errors"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// sweepBatchSize bounds the expiry index entries handled per transaction,
// well below Badger's transaction size limit.
const sweepBatchSize = 1000

// Sweep removes the verdicts and overrides that expired at now and notifies
// subscribers of the indicators whose active verdicts or override changed.
// It runs periodically while the store is started; tests may call it
// directly.
func (s *DB) Sweep(now time.Time) error {
	for {
		changes, more, err := s.sweepBatch(now)
		s.notify(changes)
		if err != nil || !more {
			return err
		}
	}
}

// sweepBatch expires up to sweepBatchSize index entries in one transaction
// and reports whether more are due.
func (s *DB) sweepBatch(now time.Time) (changes []Change, more bool, err error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err = s.update(func(txn *badger.Txn) error {
		due, dueMore := dueExpiries(txn, now)
		more = dueMore
		seen := make(map[string]bool)
		for _, key := range due {
			c, err := expire(txn, key)
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
// to, if that still expires at the indexed time. It returns the change to
// notify, with an empty Key if nothing active changed.
func expire(txn *badger.Txn, indexKey []byte) (Change, error) {
	if err := txn.Delete(indexKey); err != nil {
		return Change{}, err
	}
	at, target := parseExpiryKey(indexKey)
	switch {
	case bytes.HasPrefix(target, prefixVerdict):
		return expireVerdict(txn, target, at)
	case bytes.HasPrefix(target, prefixOverride):
		return expireOverride(txn, target, at)
	}
	return Change{}, nil
}

func expireVerdict(txn *badger.Txn, key []byte, at time.Time) (Change, error) {
	change := Change{Key: indicatorOfVerdictKey(key), Reason: ReasonExpiry}
	rec, err := getRecord(txn, key)
	switch {
	case errors.Is(err, ErrNotFound):
		// Badger's TTL removed the record already.
		return change, nil
	case err != nil:
		return Change{}, err
	case rec.Revoked || rec.Event.ExpiresAt().Unix() != at.Unix():
		// Stale index entry; the record's own entry handles it.
		return Change{}, nil
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
	return change, txn.Delete(key)
}
