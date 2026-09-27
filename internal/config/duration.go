package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const day = 24 * time.Hour

// Duration is a time.Duration written either in Go syntax ("10s", "1h30m")
// or as a whole number of days ("7d").
type Duration time.Duration

// ParseDuration parses Go duration syntax or a whole-day value such as "7d".
func ParseDuration(s string) (Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseInt(days, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: days must be a whole number like 7d", s)
		}
		if n > int64(time.Duration(1<<63-1)/day) || n < int64(time.Duration(-1<<63)/day) {
			return 0, fmt.Errorf("invalid duration %q: out of range", s)
		}
		return Duration(time.Duration(n) * day), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: want a value like 7d, 12h or 10s", s)
	}
	return Duration(d), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return errors.New("invalid duration \"\": want a value like 7d, 12h or 10s")
	}
	parsed, err := ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Std returns d as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// String formats d in days when it is a whole number of days, else in Go syntax.
func (d Duration) String() string {
	td := time.Duration(d)
	if td != 0 && td%day == 0 {
		return fmt.Sprintf("%dd", td/day)
	}
	return td.String()
}
