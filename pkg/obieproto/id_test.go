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

func TestIDTime(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 123_456_789, time.UTC)
	if got, ok := IDTime(NewID(at)); !ok || !got.Equal(at.Truncate(time.Millisecond)) || got.Location() != time.UTC {
		t.Errorf("IDTime(NewID(%v)) = %v, %v; want the time to the millisecond, in UTC", at, got, ok)
	}
	if got, ok := IDTime("00000000-0000-7000-8000-000000000000"); !ok || got.UnixMilli() != 0 {
		t.Errorf("IDTime of the Unix epoch = %v, %v", got, ok)
	}
	for _, id := range []string{
		"",
		"01A0E7E2-DE00-7000-8000-000000000000", // upper case
		"01a0e7e2-de00-4000-8000-000000000000", // version 4
		"01a0e7e2-de00-7000-c000-000000000000", // wrong variant
		"01a0e7e2de0070008000000000000000",     // no hyphens
		"sha256:01a0e7e2-de00-7000-8000-000000000000",
	} {
		if got, ok := IDTime(id); ok || !got.IsZero() {
			t.Errorf("IDTime(%q) = %v, %v; want no time", id, got, ok)
		}
	}
}
