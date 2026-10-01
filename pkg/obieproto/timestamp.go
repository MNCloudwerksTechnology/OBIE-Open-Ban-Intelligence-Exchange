package obieproto

import (
	"encoding/json"
	"fmt"
	"time"
)

// TimestampLayout is the only accepted form of an obie/0.1 timestamp: RFC 3339
// in UTC with second precision, e.g. "2026-01-05T01:50:00Z".
const TimestampLayout = "2006-01-02T15:04:05Z"

// Timestamp is a point in time that marshals to and from TimestampLayout.
// Decoding is strict so that a timestamp always round-trips byte for byte.
type Timestamp struct {
	time.Time
}

// NewTimestamp returns t in UTC, truncated to whole seconds.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp{t.UTC().Truncate(time.Second)}
}

// MarshalJSON encodes the timestamp in TimestampLayout.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.UTC().Format(TimestampLayout))
}

// UnmarshalJSON accepts only a JSON string in TimestampLayout.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("timestamp must be a string: %w", err)
	}
	parsed, err := time.Parse(TimestampLayout, s)
	if err != nil || parsed.Format(TimestampLayout) != s {
		return fmt.Errorf("timestamp %q is not RFC 3339 UTC with second precision", s)
	}
	t.Time = parsed
	return nil
}
