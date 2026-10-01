package tutorial

import (
	"regexp"
	"strings"
)

// wildcard in an expected output stands for any text within a line; a
// line of only the wildcard stands for any number of lines.
const wildcard = "…"

var (
	// reading is a command that only looks.
	reading = regexp.MustCompile(`^(sudo )?(uname|ps|sha256sum|journalctl|grep|nft list|fail2ban-client (version|status|-t)|obied (self-check|identity|--config)|obiectl (status|peers|identity|indicators|decisions|explain|enforced|overrides|show))\b` +
		`|^docker exec obie obiectl (status|identity)\b`)
	// another runs a second command, or writes the output to a file: a
	// command that starts like one that only looks may change something.
	another = regexp.MustCompile(`\|\||&&|;|>|\btee\b`)
)

// readsOnly reports whether a command only looks, so that the check may
// repeat it until its output is what the page shows: bans, verdicts and
// peers take a moment.
func readsOnly(cmd string) bool {
	return reading.MatchString(cmd) && !another.MatchString(cmd)
}

// volatile is what differs from one run of the tutorial to the next, in
// the order it is replaced.
var volatile = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`12D3KooW[1-9A-HJ-NP-Za-km-z]+`), "<peer ID>"},
	{regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`), "<event ID>"},
	{regexp.MustCompile(`SHA256:[A-Za-z0-9+/]{43}=?`), "SHA256:<fingerprint>"},
	{regexp.MustCompile(`\b[0-9a-f]{64}\b`), "<container ID>"},
	{regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z`), "<time>"},
	// Elapsed times and time left end in seconds or less; lifetimes such as
	// a verdict's 7d are what the page says, and stay.
	{regexp.MustCompile(`\b(\d+d)?(\d+h)?(\d+m)?(\d+(\.\d+)?(s|ms|µs|ns))+\b`), "<duration>"},
}

// normalize makes an output comparable across runs: what differs from run
// to run is replaced, carriage returns go, runs of blanks (column widths,
// tabs) become one, and blank lines around the output are dropped.
func normalize(out string) []string {
	out = strings.ReplaceAll(out, "\r", "")
	for _, v := range volatile {
		out = v.re.ReplaceAllString(out, v.with)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		lines = append(lines, strings.Join(strings.Fields(l), " "))
	}
	return lines
}

// matches reports whether an output is what the page shows, after both are
// normalized, with the page's wildcards.
func matches(want, got string) bool {
	w, g := normalize(want), normalize(got)
	memo := map[[2]int]bool{}
	var match func(i, j int) bool
	match = func(i, j int) bool {
		key := [2]int{i, j}
		if v, ok := memo[key]; ok {
			return v
		}
		var ok bool
		switch {
		case i == len(w):
			ok = j == len(g)
		case w[i] == wildcard:
			ok = match(i+1, j) || (j < len(g) && match(i, j+1))
		default:
			ok = j < len(g) && lineMatches(w[i], g[j]) && match(i+1, j+1)
		}
		memo[key] = ok
		return ok
	}
	return match(0, 0)
}

// lineMatches reports whether a line of output is the line the page shows,
// where a wildcard in the page's line stands for any text.
func lineMatches(want, got string) bool {
	if !strings.Contains(want, wildcard) {
		return want == got
	}
	parts := strings.Split(want, wildcard)
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$").MatchString(got)
}
