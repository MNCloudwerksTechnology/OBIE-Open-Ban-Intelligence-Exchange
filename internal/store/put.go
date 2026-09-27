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

// record is the value of a verdict key: a publisher's latest verdict on an
// indicator.
type record struct {
	Event   *obieproto.Event `json:"event"`
	Revoked bool             `json:"revoked,omitempty"`
}

// active reports whether the verdict counts at now.
func (r *record) active(now time.Time) bool {
	return !r.Revoked && !r.Event.Expired(now)
}

// outcome is what a Put changed.
type outcome struct {
	result result
	// change is the notification to send; empty Key for none.
	change Change
	// foreignRevokes counts revocations of the event by other publishers
	// that arrived before it and are now known to be ignored.
	foreignRevokes int
}

// Put stores ev; see Store.
func (s *DB) Put(ev *obieproto.Event) (bool, error) {
	if err := checkEvent(ev); err != nil {
		return false, err
	}
	now := s.opts.Now()
	if ev.Expired(now) {
		s.counters.add(resultExpired)
		return false, nil
	}

	s.writeMu.Lock()
	var out outcome
	err := s.update(func(txn *badger.Txn) error {
		var err error
		out, err = s.put(txn, ev, now)
		return err
	})
	s.writeMu.Unlock()
	if err != nil {
		return false, fmt.Errorf("put event %s: %w", ev.ID, err)
	}

	s.counters.add(out.result)
	for range out.foreignRevokes {
		s.counters.add(resultForeignRevoke)
	}
	if out.change.Key != "" {
		s.notify([]Change{out.change})
	}
	return out.result == resultAccepted, nil
}

// checkEvent rejects events the store cannot key. Full protocol validation
// is the caller's job.
func checkEvent(ev *obieproto.Event) error {
	switch {
	case ev == nil:
		return fmt.Errorf("%w event: nil", ErrInvalid)
	case ev.ID == "" || ev.Publisher.PeerID == "" || ev.Indicator.Kind == "" || ev.Indicator.Value == "":
		return fmt.Errorf("%w event: missing id, publisher or indicator", ErrInvalid)
	case ev.Type == obieproto.TypeVerdict && ev.Verdict == nil:
		return fmt.Errorf("%w event %s: verdict without verdict body", ErrInvalid, ev.ID)
	case ev.Type == obieproto.TypeRevoke && ev.Revokes == "":
		return fmt.Errorf("%w event %s: revocation without revokes", ErrInvalid, ev.ID)
	case ev.Type != obieproto.TypeVerdict && ev.Type != obieproto.TypeRevoke:
		return fmt.Errorf("%w event %s: type %q", ErrInvalid, ev.ID, ev.Type)
	}
	return nil
}

func (s *DB) put(txn *badger.Txn, ev *obieproto.Event, now time.Time) (outcome, error) {
	_, err := txn.Get(eventKey(ev.ID))
	switch {
	case err == nil:
		return outcome{result: resultDuplicate}, nil
	case !errors.Is(err, badger.ErrKeyNotFound):
		return outcome{}, err
	}
	if ev.Type == obieproto.TypeRevoke {
		return s.putRevoke(txn, ev, now)
	}
	return s.putVerdict(txn, ev, now)
}

// putVerdict stores ev if it is newer than the publisher's current verdict
// on the indicator. A revocation that arrived before the verdict is applied.
func (s *DB) putVerdict(txn *badger.Txn, ev *obieproto.Event, now time.Time) (outcome, error) {
	key := verdictKey(ev.Key(), ev.Publisher.PeerID)
	cur, err := getRecord(txn, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return outcome{}, err
	}
	if cur != nil && !newer(ev, cur.Event) {
		return outcome{result: resultStale}, nil
	}

	rec := record{Event: ev}
	var foreign int
	rec.Revoked, foreign, err = earlyRevocations(txn, ev)
	if err != nil {
		return outcome{}, err
	}
	if err := setEvent(txn, ev); err != nil {
		return outcome{}, err
	}
	if cur != nil && !cur.Revoked {
		if err := txn.Delete(expiryKey(cur.Event.ExpiresAt(), key)); err != nil {
			return outcome{}, err
		}
	}
	if err := setRecord(txn, key, &rec); err != nil {
		return outcome{}, err
	}

	out := outcome{result: resultAccepted, foreignRevokes: foreign}
	if !rec.Revoked || (cur != nil && cur.active(now)) {
		out.change = Change{Key: ev.Key(), Reason: ReasonVerdict}
	}
	return out, nil
}

// earlyRevocations looks up revocations of verdict ev that arrived before
// it. It reports whether its own publisher revoked it (for the same
// indicator) and how many other publishers tried to.
func earlyRevocations(txn *badger.Txn, ev *obieproto.Event) (revoked bool, foreign int, err error) {
	own := revokeKey(ev.ID, ev.Publisher.PeerID)
	it := txn.NewIterator(badger.IteratorOptions{Prefix: revokeKey(ev.ID, "")})
	defer it.Close()
	for it.Rewind(); it.Valid(); it.Next() {
		item := it.Item()
		if !bytes.Equal(item.Key(), own) {
			foreign++
			continue
		}
		err := item.Value(func(indicator []byte) error {
			revoked = string(indicator) == ev.Key()
			return nil
		})
		if err != nil {
			return false, 0, err
		}
	}
	return revoked, foreign, nil
}

// newer reports whether verdict a supersedes verdict b of the same publisher:
// it was issued later, or at the same second with a greater (UUIDv7) ID.
func newer(a, b *obieproto.Event) bool {
	if !a.IssuedAt.Equal(b.IssuedAt.Time) {
		return a.IssuedAt.After(b.IssuedAt.Time)
	}
	return a.ID > b.ID
}

// putRevoke applies a revocation. Only the verdict's own publisher may revoke
// it, and only for the verdict's indicator. A revocation of a verdict the
// store has not seen is remembered, so the verdict is stored as revoked if it
// arrives later.
func (s *DB) putRevoke(txn *badger.Txn, ev *obieproto.Event, now time.Time) (outcome, error) {
	target, err := getEvent(txn, ev.Revokes)
	if errors.Is(err, ErrNotFound) {
		mark := badger.NewEntry(revokeKey(ev.Revokes, ev.Publisher.PeerID), []byte(ev.Key()))
		mark.ExpiresAt = badgerExpiry(ev.ExpiresAt())
		if err := txn.SetEntry(mark); err != nil {
			return outcome{}, err
		}
		return outcome{result: resultAccepted}, setEvent(txn, ev)
	}
	if err != nil {
		return outcome{}, err
	}
	switch {
	case target.Publisher.PeerID != ev.Publisher.PeerID:
		s.log.Debug("ignoring revocation by another publisher", "event", ev.ID, "revokes", ev.Revokes,
			"publisher", ev.Publisher.PeerID, "verdict_publisher", target.Publisher.PeerID)
		return outcome{result: resultForeignRevoke}, nil
	case target.Type != obieproto.TypeVerdict || target.Key() != ev.Key():
		s.log.Debug("ignoring revocation that does not match its verdict", "event", ev.ID, "revokes", ev.Revokes)
		return outcome{result: resultInvalidRevoke}, nil
	}
	if err := setEvent(txn, ev); err != nil {
		return outcome{}, err
	}

	key := verdictKey(target.Key(), target.Publisher.PeerID)
	cur, err := getRecord(txn, key)
	switch {
	case errors.Is(err, ErrNotFound):
		return outcome{result: resultAccepted}, nil
	case err != nil:
		return outcome{}, err
	case cur.Event.ID != target.ID || cur.Revoked:
		// Superseded by a newer verdict or already revoked: nothing active changes.
		return outcome{result: resultAccepted}, nil
	}
	wasActive := cur.active(now)
	cur.Revoked = true
	if err := setRecord(txn, key, cur); err != nil {
		return outcome{}, err
	}
	if err := txn.Delete(expiryKey(cur.Event.ExpiresAt(), key)); err != nil {
		return outcome{}, err
	}
	out := outcome{result: resultAccepted}
	if wasActive {
		out.change = Change{Key: target.Key(), Reason: ReasonRevoke}
	}
	return out, nil
}

// setEvent stores ev under its ID until it expires.
func setEvent(txn *badger.Txn, ev *obieproto.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	e := badger.NewEntry(eventKey(ev.ID), data)
	e.ExpiresAt = badgerExpiry(ev.ExpiresAt())
	return txn.SetEntry(e)
}

// setRecord stores rec under key until its verdict expires. An unrevoked
// record is also entered into the expiry index.
func setRecord(txn *badger.Txn, key []byte, rec *record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	expires := rec.Event.ExpiresAt()
	e := badger.NewEntry(key, data)
	e.ExpiresAt = badgerExpiry(expires)
	if err := txn.SetEntry(e); err != nil {
		return err
	}
	if rec.Revoked {
		return nil
	}
	return txn.Set(expiryKey(expires, key), nil)
}

func getEvent(txn *badger.Txn, id string) (*obieproto.Event, error) {
	var ev obieproto.Event
	if err := getJSON(txn, eventKey(id), &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

func getRecord(txn *badger.Txn, key []byte) (*record, error) {
	var rec record
	if err := getJSON(txn, key, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// getJSON decodes the value of key into v; ErrNotFound if it does not exist.
func getJSON(txn *badger.Txn, key []byte, v any) error {
	item, err := txn.Get(key)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return item.Value(func(data []byte) error {
		if err := json.Unmarshal(data, v); err != nil {
			return fmt.Errorf("decode %q: %w", key, err)
		}
		return nil
	})
}
