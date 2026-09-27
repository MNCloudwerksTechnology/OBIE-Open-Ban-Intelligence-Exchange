package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// MaxNoteLength is the longest override note in bytes.
const MaxNoteLength = 1024

// SetOverride stores or replaces an override; see Store. The indicator is
// normalized, CreatedAt defaults to now and ExpiresAt is truncated to whole
// seconds.
func (s *DB) SetOverride(o Override) error {
	now := s.opts.Now()
	if err := normalizeOverride(&o, now); err != nil {
		return err
	}
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	indicator := o.Indicator.Key()
	key := overrideKey(indicator)

	s.writeMu.Lock()
	err = s.update(func(txn *badger.Txn) error {
		if err := deleteOverride(txn, key); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		e := badger.NewEntry(key, data)
		if o.ExpiresAt.IsZero() {
			return txn.SetEntry(e)
		}
		e.ExpiresAt = badgerExpiry(o.ExpiresAt)
		if err := txn.SetEntry(e); err != nil {
			return err
		}
		return txn.Set(expiryKey(o.ExpiresAt, key), nil)
	})
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("set override %s: %w", indicator, err)
	}
	s.notify([]Change{{Key: indicator, Reason: ReasonOverride}})
	return nil
}

func normalizeOverride(o *Override, now time.Time) error {
	if o.Action != ForceAllow && o.Action != ForceBlock {
		return fmt.Errorf("%w override: action %q", ErrInvalid, o.Action)
	}
	if err := o.Indicator.Normalize(); err != nil {
		return fmt.Errorf("%w override: %w", ErrInvalid, err)
	}
	if len(o.Note) > MaxNoteLength {
		return fmt.Errorf("%w override: note longer than %d bytes", ErrInvalid, MaxNoteLength)
	}
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	}
	o.CreatedAt = o.CreatedAt.UTC()
	if o.ExpiresAt.IsZero() {
		return nil
	}
	o.ExpiresAt = o.ExpiresAt.UTC().Truncate(time.Second)
	if !now.Before(o.ExpiresAt) {
		return fmt.Errorf("%w override: already expired at %s", ErrInvalid, o.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}

// Override returns the override in effect for an indicator; see Store.
func (s *DB) Override(indicatorKey string, now time.Time) (Override, error) {
	var o Override
	err := s.view(func(txn *badger.Txn) error {
		var err error
		o, err = getOverride(txn, overrideKey(indicatorKey))
		return err
	})
	if err != nil {
		return Override{}, err
	}
	if !o.Active(now) {
		return Override{}, ErrNotFound
	}
	return o, nil
}

// DeleteOverride removes an override; see Store.
func (s *DB) DeleteOverride(indicatorKey string) (bool, error) {
	s.writeMu.Lock()
	err := s.update(func(txn *badger.Txn) error {
		return deleteOverride(txn, overrideKey(indicatorKey))
	})
	s.writeMu.Unlock()
	switch {
	case errors.Is(err, ErrNotFound):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("delete override %s: %w", indicatorKey, err)
	}
	s.notify([]Change{{Key: indicatorKey, Reason: ReasonOverride}})
	return true, nil
}

// Overrides returns all overrides in effect; see Store.
func (s *DB) Overrides(now time.Time) ([]Override, error) {
	var out []Override
	err := s.view(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.IteratorOptions{Prefix: prefixOverride, PrefetchValues: true, PrefetchSize: 100})
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var o Override
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &o) }); err != nil {
				return fmt.Errorf("decode %q: %w", it.Item().Key(), err)
			}
			if o.Active(now) {
				out = append(out, o)
			}
		}
		return nil
	})
	return out, err
}

func getOverride(txn *badger.Txn, key []byte) (Override, error) {
	var o Override
	err := getJSON(txn, key, &o)
	return o, err
}

// deleteOverride removes the override at key and its expiry index entry; it
// returns ErrNotFound if there is none.
func deleteOverride(txn *badger.Txn, key []byte) error {
	old, err := getOverride(txn, key)
	if err != nil {
		return err
	}
	if !old.ExpiresAt.IsZero() {
		if err := txn.Delete(expiryKey(old.ExpiresAt, key)); err != nil {
			return err
		}
	}
	return txn.Delete(key)
}
