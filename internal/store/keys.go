package store

import (
	"bytes"
	"encoding/binary"
	"time"
)

// Keyspace prefixes; see ADR 0004.
var (
	prefixEvent     = []byte("e/")
	prefixVerdict   = []byte("v/")
	prefixExpiry    = []byte("x/")
	prefixRevoke    = []byte("r/")
	prefixOverride  = []byte("o/")
	keySeparator    = []byte{0}
	expiryKeyHeader = len(prefixExpiry) + 8
)

func join(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

// eventKey is the key of a stored event and its seen-ID marker.
func eventKey(id string) []byte {
	return join(prefixEvent, []byte(id))
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

// revokeKey marks that publisher revoked a verdict the store has not seen.
func revokeKey(verdictID, publisher string) []byte {
	return join(prefixRevoke, []byte(verdictID), keySeparator, []byte(publisher))
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
