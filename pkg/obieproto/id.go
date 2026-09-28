package obieproto

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"
)

// NewID returns a new random UUIDv7 (RFC 9562) in canonical lower-case form,
// as required for [Event.ID], carrying the Unix millisecond time of t.
func NewID(t time.Time) string {
	var b [16]byte
	// crypto/rand.Read never returns an error; it crashes the program if the
	// system's randomness is unavailable.
	_, _ = rand.Read(b[6:])
	ms := uint64(t.UnixMilli()) // #nosec G115 -- event times lie after 1970.
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], ms)
	copy(b[:6], ts[2:])
	b[6] = 0x70 | b[6]&0x0f // version 7
	b[8] = 0x80 | b[8]&0x3f // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
