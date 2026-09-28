package obieproto

import (
	"testing"
	"time"
)

func TestNewID(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	seen := make(map[string]bool)
	for range 1000 {
		id := NewID(at)
		if !uuidV7Pattern.MatchString(id) {
			t.Fatalf("NewID() = %q, not a lower-case UUIDv7", id)
		}
		if seen[id] {
			t.Fatalf("NewID() repeated %q", id)
		}
		seen[id] = true
	}
	// The first 48 bits are the Unix milliseconds, so IDs sort by time.
	if id := NewID(at); id[:13] != "01a0e7e2-de00" {
		t.Errorf("NewID() = %q, want the timestamp prefix 01a0e7e2-de00", id)
	}
	if earlier, later := NewID(at), NewID(at.Add(time.Millisecond)); earlier >= later {
		t.Errorf("NewID() at a later time sorts first: %q >= %q", earlier, later)
	}
}
