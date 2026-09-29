package cli

import (
	"flag"
	"fmt"
	"io"
	"time"
)

// Output for people follows the same rules in every command: times are
// RFC 3339 in UTC, spans of time are given to the second with days for long
// ones, labels are words and never only a color, and a long listing starts
// with a summary and shows at most --limit rows. Output for programs is
// --json, which is always complete.

// formatTime writes t as every table and message does: RFC 3339 in UTC.
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// day is 24 hours, for spans of time.
const day = 24 * time.Hour

// formatDuration writes a span of time to the second, with days for spans
// of a day or more, e.g. "3d4h5m6s", "3d" or "1h2m3s".
func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	days, rest := d/day, d%day
	switch {
	case days == 0:
		return rest.String()
	case rest == 0:
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd%s", days, rest)
}

// defaultRows is how many rows a long listing shows unless --limit says
// otherwise.
const defaultRows = 100

// limitFlag registers --limit, the most rows a long listing shows.
func limitFlag(fs *flag.FlagSet) *int {
	return fs.Int("limit", defaultRows, "show at most this `number` of rows; 0 shows every row (--json always has every row)")
}

// checkLimit reports whether --limit is 0 or more; otherwise it explains
// the mistake of program on stderr.
func checkLimit(program string, limit int, stderr io.Writer) bool {
	if limit < 0 {
		usageProblem(program, fmt.Sprintf("--limit must be 0 (every row) or more, got %d", limit)).write(stderr, program)
		return false
	}
	return true
}

// shownRows returns how many of total rows a listing shows with limit.
func shownRows(total, limit int) int {
	if limit > 0 && total > limit {
		return limit
	}
	return total
}

// writeMoreRows says below a table that it shows only shown of total rows
// of what, and how to see the others; narrow is how to narrow them down,
// if there is a way.
func writeMoreRows(w io.Writer, shown, total int, what, narrow string) error {
	if shown >= total {
		return nil
	}
	_, err := fmt.Fprintf(w, "\nShowing %d of %d %s. %sSee them all with --limit 0, or use --json.\n", shown, total, what, narrow)
	return err
}
