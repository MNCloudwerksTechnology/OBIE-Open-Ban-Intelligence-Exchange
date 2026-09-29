package store

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// MaxNoteLength is the longest override note in bytes.
const MaxNoteLength = 1024

// OverrideRetention is how long the store keeps an override after its
// expiry: its record, so that the sweep can still archive it after a
// downtime, and the override once it expired, so that the operator can
// see it (ADR 0024).
const OverrideRetention = 7 * 24 * time.Hour

// SetOverride stores or replaces an override; see Store. The indicator is
// normalized, CreatedAt defaults to now and ExpiresAt is truncated to whole
// seconds. An expired override it replaces is kept as expired.
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
		old, err := deleteOverride(txn, key)
		switch {
		case errors.Is(err, ErrNotFound):
		case err != nil:
			return err
		case !old.Active(now):
			if err := archiveOverride(txn, &old); err != nil {
				return err
			}
		}
		e := badger.NewEntry(key, data)
		if o.ExpiresAt.IsZero() {
			return txn.SetEntry(e)
		}
		e.ExpiresAt = badgerExpiry(o.ExpiresAt.Add(OverrideRetention))
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

// CheckOverride returns the error SetOverride would return at now for an
// override it cannot hold — an unknown action, an invalid indicator, a
// note that is too long, an expiry that has passed — without storing
// anything; the console checks an override with it before asking to
// confirm it (ADR 0026).
func CheckOverride(o Override, now time.Time) error {
	return normalizeOverride(&o, now)
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

// DeleteOverride removes an override; see Store. An expired override is
// none: it is left for the sweep, which keeps it as expired.
func (s *DB) DeleteOverride(indicatorKey string) (bool, error) {
	now := s.opts.Now()
	s.writeMu.Lock()
	err := s.update(func(txn *badger.Txn) error {
		key := overrideKey(indicatorKey)
		if o, err := getOverride(txn, key); err != nil || !o.Active(now) {
			return cmp.Or(err, ErrNotFound)
		}
		_, err := deleteOverride(txn, key)
		return err
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
		return walkOverrides(txn, prefixOverride, func(o *Override) {
			if o.Active(now) {
				out = append(out, *o)
			}
		})
	})
	return out, err
}

// ExpiredOverrides returns the overrides that expired before now and are
// kept until OverrideRetention after their expiry, ordered by key: those
// the sweep moved, and those it has not reached yet (ADR 0024).
func (s *DB) ExpiredOverrides(now time.Time) ([]Override, error) {
	var out []Override
	err := s.view(func(txn *badger.Txn) error {
		unswept := map[string]bool{}
		err := walkOverrides(txn, prefixOverride, func(o *Override) {
			if !o.Active(now) {
				unswept[o.Indicator.Key()] = true
				out = append(out, *o)
			}
		})
		if err != nil {
			return err
		}
		// An unswept override is newer than the one kept on its indicator.
		return walkOverrides(txn, prefixExpiredOverride, func(o *Override) {
			if !unswept[o.Indicator.Key()] {
				out = append(out, *o)
			}
		})
	})
	slices.SortFunc(out, func(a, b Override) int { return strings.Compare(a.Indicator.Key(), b.Indicator.Key()) })
	return out, err
}

// walkOverrides calls fn with every override stored under prefix, in key
// order.
func walkOverrides(txn *badger.Txn, prefix []byte, fn func(*Override)) error {
	it := txn.NewIterator(badger.IteratorOptions{Prefix: prefix, PrefetchValues: true, PrefetchSize: 100})
	defer it.Close()
	for it.Rewind(); it.Valid(); it.Next() {
		var o Override
		if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &o) }); err != nil {
			return fmt.Errorf("decode %q: %w", it.Item().Key(), err)
		}
		fn(&o)
	}
	return nil
}

func getOverride(txn *badger.Txn, key []byte) (Override, error) {
	var o Override
	err := getJSON(txn, key, &o)
	return o, err
}

// deleteOverride removes the override at key and its expiry index entry,
// returning it; it returns ErrNotFound if there is none.
func deleteOverride(txn *badger.Txn, key []byte) (Override, error) {
	old, err := getOverride(txn, key)
	if err != nil {
		return Override{}, err
	}
	if !old.ExpiresAt.IsZero() {
		if err := txn.Delete(expiryKey(old.ExpiresAt, key)); err != nil {
			return Override{}, err
		}
	}
	return old, txn.Delete(key)
}

// archiveOverride keeps the expired override o until OverrideRetention
// after its expiry, replacing an earlier one on the same indicator.
func archiveOverride(txn *badger.Txn, o *Override) error {
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	e := badger.NewEntry(expiredOverrideKey(o.Indicator.Key()), data)
	e.ExpiresAt = badgerExpiry(o.ExpiresAt.Add(OverrideRetention))
	return txn.SetEntry(e)
}
