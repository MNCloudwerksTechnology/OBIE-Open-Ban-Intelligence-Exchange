package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// record is the value of a verdict key: a publisher's latest verdict on an
// indicator.
type record struct {
	Event   *obieproto.Event `json:"event"`
	Revoked bool             `json:"revoked,omitempty"`
	// Revocation is the revocation that ended a revoked verdict; kept only
	// with the ended verdict (ADR 0023).
	Revocation *Revocation `json:"revocation,omitempty"`
}

// active reports whether the verdict counts at now.
func (r *record) active(now time.Time) bool {
	return !r.Revoked && !r.Event.Expired(now)
}

// outcome is what a Put changed.
type outcome struct {
	result result
	// changes are the notifications to send.
	changes []Change
	// verdicts is the change in the number of verdict records; evicted
	// counts the records evicted to make room.
	verdicts, evicted int
	// evictedIndex is the expiry index key of the last record evicted.
	evictedIndex []byte
	// foreignRevokes and invalidRevokes count revocations of a verdict that
	// arrived before it and are now known to be ignored.
	foreignRevokes, invalidRevokes int
	// ended counts the verdicts kept once they ended (ADR 0023).
	ended int
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
	if err == nil {
		s.addVerdicts(out.verdicts)
		s.ended.Add(int64(out.ended))
		if out.evictedIndex != nil {
			s.evictFrom = out.evictedIndex
		}
	}
	s.writeMu.Unlock()
	if err != nil {
		return false, fmt.Errorf("put event %s: %w", ev.ID, err)
	}

	s.counters.add(out.result)
	for range out.foreignRevokes {
		s.counters.add(resultForeignRevoke)
	}
	for range out.invalidRevokes {
		s.counters.add(resultInvalidRevoke)
	}
	s.counters.evict(out.evicted)
	s.notify(out.changes)
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
	case strings.ContainsRune(ev.ID+ev.Publisher.PeerID+ev.Indicator.Kind+ev.Indicator.Value+ev.Revokes, 0):
		return fmt.Errorf("%w event: NUL byte in a key field", ErrInvalid)
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
	for _, key := range [][]byte{eventKey(ev.ID), seenKey(ev.ID)} {
		_, err := txn.Get(key)
		switch {
		case err == nil:
			return outcome{result: resultDuplicate}, nil
		case !errors.Is(err, badger.ErrKeyNotFound):
			return outcome{}, err
		}
	}
	apply := s.putVerdict
	if ev.Type == obieproto.TypeRevoke {
		apply = s.putRevoke
	}
	out, err := apply(txn, ev, now)
	if err != nil || out.result == resultAccepted {
		return out, err
	}
	// Remember the ignored event's ID until it expires, so replays are
	// duplicates.
	seen := badger.NewEntry(seenKey(ev.ID), nil)
	seen.ExpiresAt = badgerExpiry(ev.ExpiresAt())
	return out, txn.SetEntry(seen)
}

// putVerdict stores ev if it is newer than the publisher's current verdict
// on the indicator. A revocation that arrived before the verdict is applied.
func (s *DB) putVerdict(txn *badger.Txn, ev *obieproto.Event, now time.Time) (outcome, error) {
	key := verdictKey(ev.Key(), ev.Publisher.PeerID)
	cur, err := getRecord(txn, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return outcome{}, err
	}
	// A record past its expiry is kept for EndedRetention, but no verdict
	// supersedes an ended one (ADR 0023).
	if cur != nil && !cur.Event.Expired(now) && !newer(ev, cur.Event) {
		return outcome{result: resultStale}, nil
	}
	var out outcome
	if cur == nil {
		room, err := s.makeRoom(txn, ev, now, &out)
		if err != nil || !room {
			out.result = resultFull
			return out, err
		}
	}

	rec := record{Event: ev}
	var rev *Revocation
	rec.Revoked, rev, out.foreignRevokes, out.invalidRevokes, err = earlyRevocations(txn, ev)
	if err != nil {
		return outcome{}, err
	}
	if err := setEvent(txn, ev); err != nil {
		return outcome{}, err
	}
	if cur != nil {
		if err := s.archiveExpired(txn, cur, now, &out.ended); err != nil {
			return outcome{}, err
		}
		removed, err := deleteIndex(txn, expiryKey(cur.Event.ExpiresAt(), key))
		if err != nil {
			return outcome{}, err
		}
		out.verdicts -= removed
	}
	added, err := s.setRecord(txn, key, &rec)
	if err != nil {
		return outcome{}, err
	}
	out.verdicts += added
	if rec.Revoked {
		if err := s.archive(txn, &rec, EndedRevoked, rev, &out.ended); err != nil {
			return outcome{}, err
		}
	}

	out.result = resultAccepted
	if !rec.Revoked || (cur != nil && cur.active(now)) {
		out.changes = append(out.changes, Change{Key: ev.Key(), Reason: ReasonVerdict})
	}
	return out, nil
}

// earlyRevocations looks up revocations of verdict ev that arrived before
// it. It reports whether its own publisher revoked it for the same
// indicator and with which revocation (nil if the store did not keep it),
// how many other publishers tried to, and whether its publisher named
// another indicator (invalid).
func earlyRevocations(txn *badger.Txn, ev *obieproto.Event) (revoked bool, rev *Revocation, foreign, invalid int, err error) {
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
			return false, nil, 0, 0, err
		}
		if !revoked {
			invalid++
		}
	}
	if revoked {
		var r Revocation
		switch err := getJSON(txn, pendingRevocationKey(ev.ID, ev.Publisher.PeerID), &r); {
		case err == nil:
			rev = &r
		case !errors.Is(err, ErrNotFound):
			return false, nil, 0, 0, err
		}
	}
	return revoked, rev, foreign, invalid, nil
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
		// Kept apart from the marker, whose value older versions read, so
		// the verdict shows why it was revoked when it arrives (ADR 0023).
		info, err := json.Marshal(revocationOf(ev))
		if err != nil {
			return outcome{}, err
		}
		pending := badger.NewEntry(pendingRevocationKey(ev.Revokes, ev.Publisher.PeerID), info)
		pending.ExpiresAt = mark.ExpiresAt
		if err := txn.SetEntry(pending); err != nil {
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
	added, err := s.setRecord(txn, key, cur)
	if err != nil {
		return outcome{}, err
	}
	out := outcome{result: resultAccepted, verdicts: added}
	if err := s.archive(txn, cur, EndedRevoked, revocationOf(ev), &out.ended); err != nil {
		return outcome{}, err
	}
	if wasActive {
		out.changes = append(out.changes, Change{Key: target.Key(), Reason: ReasonRevoke})
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

// setRecord stores rec under key until EndedRetention after its verdict
// expires, so the sweep can still archive it after a downtime (ADR 0023),
// and enters it into the expiry index, revoked or not: the index holds
// exactly one entry per verdict record, which the sweep and the eviction
// rely on. It returns 1 if the index entry is new, else 0. Callers hold
// writeMu.
func (s *DB) setRecord(txn *badger.Txn, key []byte, rec *record) (int, error) {
	data, err := json.Marshal(rec)
	if err != nil {
		return 0, err
	}
	expires := rec.Event.ExpiresAt()
	e := badger.NewEntry(key, data)
	e.ExpiresAt = badgerExpiry(expires.Add(EndedRetention))
	if err := txn.SetEntry(e); err != nil {
		return 0, err
	}
	index := expiryKey(expires, key)
	if bytes.Compare(index, s.evictFrom) < 0 {
		s.evictFrom = index // keep it a lower bound of the candidates
	}
	switch _, err := txn.Get(index); {
	case err == nil:
		return 0, nil
	case !errors.Is(err, badger.ErrKeyNotFound):
		return 0, err
	}
	return 1, txn.Set(index, nil)
}

// deleteIndex removes an expiry index entry and returns 1 if it existed,
// else 0.
func deleteIndex(txn *badger.Txn, index []byte) (int, error) {
	switch _, err := txn.Get(index); {
	case errors.Is(err, badger.ErrKeyNotFound):
		return 0, nil
	case err != nil:
		return 0, err
	}
	return 1, txn.Delete(index)
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
			return fmt.Errorf("%w: decode %q: %w", errCorrupt, key, err)
		}
		return nil
	})
}
