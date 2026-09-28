package store

import (
	"bytes"
	"encoding/binary"
	"time"
)

// Keyspace prefixes; see ADR 0008.
var (
	prefixEvent    = []byte("e/")
	prefixVerdict  = []byte("v/")
	prefixExpiry   = []byte("x/")
	prefixRevoke   = []byte("r/")
	prefixOverride = []byte("o/")
	prefixSeen     = []byte("s/")
	// prefixEnded holds the verdicts that ended and the revocations that
	// arrived before their verdict (ADR 0023).
	prefixEnded             = []byte("h/")
	prefixPendingRevocation = []byte("h/p/")
	keySeparator            = []byte{0}
	expiryKeyHeader         = len(prefixExpiry) + 8
)

func join(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

// eventKey is the key of a stored event and its seen-ID marker.
func eventKey(id string) []byte {
	return join(prefixEvent, []byte(id))
}

// seenKey marks the ID of an ignored event, so that its replays count as
// duplicates.
func seenKey(id string) []byte {
	return join(prefixSeen, []byte(id))
}

// verdictPrefix is the common prefix of all verdict records on an indicator.
func verdictPrefix(indicator string) []byte {
	return join(prefixVerdict, []byte(indicator), keySeparator)
}

// verdictKey is the key of a publisher's latest verdict on an indicator.
func verdictKey(indicator, publisher string) []byte {
	return join(verdictPrefix(indicator), []byte(publisher))
}

// indicatorOfVerdictKey returns the indicator part of a verdict key.
func indicatorOfVerdictKey(key []byte) string {
	rest := key[len(prefixVerdict):]
	if i := bytes.IndexByte(rest, 0); i >= 0 {
		rest = rest[:i]
	}
	return string(rest)
}

// publisherOfVerdictKey returns the publisher part of a verdict key.
func publisherOfVerdictKey(key []byte) string {
	_, publisher, _ := bytes.Cut(key[len(prefixVerdict):], keySeparator)
	return string(publisher)
}

// revokeKey marks that publisher revoked a verdict the store has not seen.
func revokeKey(verdictID, publisher string) []byte {
	return join(prefixRevoke, []byte(verdictID), keySeparator, []byte(publisher))
}

// endedPrefix is the common prefix of the verdicts that ended in state.
func endedPrefix(state EndedState) []byte {
	letter := "x"
	if state == EndedRevoked {
		letter = "r"
	}
	return join(prefixEnded, []byte(letter+"/"))
}

// endedKey is the key of a publisher's verdict of a category on an
// indicator that ended in state. Neither the indicator nor the publisher
// nor the category holds a NUL byte.
func endedKey(state EndedState, indicator, publisher, category string) []byte {
	return join(endedPrefix(state), []byte(indicator), keySeparator, []byte(publisher), keySeparator, []byte(category))
}

// pendingRevocationKey holds the revocation of a verdict the store has not
// seen yet, next to its revokeKey marker.
func pendingRevocationKey(verdictID, publisher string) []byte {
	return join(prefixPendingRevocation, []byte(verdictID), keySeparator, []byte(publisher))
}

// overrideKey is the key of an indicator's operator override.
func overrideKey(indicator string) []byte {
	return join(prefixOverride, []byte(indicator))
}

// expiryKey indexes target (a verdict or override key) under its expiry, so
// that iterating the expiry keyspace visits entries in expiry order.
func expiryKey(at time.Time, target []byte) []byte {
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(max(at.Unix(), 0))) // #nosec G115 -- clamped to >= 0.
	return join(prefixExpiry, ts[:], target)
}

// parseExpiryKey returns the expiry and the target of an expiry index key.
func parseExpiryKey(key []byte) (time.Time, []byte) {
	sec := binary.BigEndian.Uint64(key[len(prefixExpiry):expiryKeyHeader])
	return time.Unix(int64(sec), 0), key[expiryKeyHeader:] // #nosec G115 -- written from a non-negative int64.
}

// badgerExpiry converts t into a Badger ExpiresAt value.
func badgerExpiry(t time.Time) uint64 {
	return uint64(max(t.Unix(), 1)) // #nosec G115 -- clamped to >= 1.
}
