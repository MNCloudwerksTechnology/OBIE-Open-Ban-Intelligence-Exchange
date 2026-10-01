package obieproto

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strconv"
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

// IDTime returns the Unix millisecond time that the UUIDv7 id carries in
// its first 48 bits: the event's creation time if its publisher created
// the id from it, as the specification recommends and [NewID] does. ok is
// false if id is not a UUIDv7 in canonical lower-case form.
func IDTime(id string) (t time.Time, ok bool) {
	if !uuidV7Pattern.MatchString(id) {
		return time.Time{}, false
	}
	ms, err := strconv.ParseInt(id[0:8]+id[9:13], 16, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms).UTC(), true
}
