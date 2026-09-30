package sandbox

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// walkthroughPath is the walkthrough, relative to the repository root.
const walkthroughPath = "documentation/sandbox.md"

// A step is a command of the walkthrough and the output the page shows
// for it: a shell block with one ./sandbox command, followed by a text
// block.
type step struct {
	section string
	line    int
	command string
	want    string
}

// A consoleHint is a paragraph that starts with "**In the console:**": it
// names one console page by its URL, and the page shows every phrase the
// paragraph sets in bold.
type consoleHint struct {
	section string
	line    int
	port    int
	path    string
	phrases []string
	// after is the number of steps before the hint.
	after int
}

// walkthrough is documentation/sandbox.md as the tests read it.
type walkthrough struct {
	title string
	// sections are the level-2 headings, in order.
	sections []string
	// setup are the commands of shell blocks without output: git clone
	// and cd, which prepare the steps and are not run by the check.
	setup []string
	steps []step
	hints []consoleHint
	// messages are text blocks without a command, by the level-3 heading
	// they follow: what the script says when it cannot start.
	messages map[string]string
	// problems are mistakes in the page's form.
	problems []string
}

var (
	shellFence = regexp.MustCompile("^```(sh|shell|bash)\\s*$")
	textFence  = regexp.MustCompile("^```text\\s*$")
	anyFence   = regexp.MustCompile("^```")
	// consoleURL is a console page as the walkthrough links it.
	consoleURL = regexp.MustCompile(`http://127\.0\.0\.1:(\d+)(/[^\s>)]*)`)
	bold       = regexp.MustCompile(`\*\*([^*]+)\*\*`)
)

// consoleLead opens a console hint.
const consoleLead = "**In the console:**"

// parseWalkthrough reads the walkthrough page.
func parseWalkthrough(page string) walkthrough {
	w := walkthrough{messages: map[string]string{}}
	lines := strings.Split(page, "\n")
	section, subsection := "", ""
	var pending []string // commands of the last shell block, not yet paired
	pendingLine := 0
	flushPending := func() {
		for _, c := range pending {
			if !strings.HasPrefix(c, "git clone ") && !strings.HasPrefix(c, "cd ") {
				w.problems = append(w.problems, fmt.Sprintf("line %d: %q has no output block; only git clone and cd may go without one", pendingLine, c))
			}
			w.setup = append(w.setup, c)
		}
		pending = nil
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "# "):
			w.title = strings.TrimPrefix(line, "# ")
		case strings.HasPrefix(line, "## "):
			flushPending()
			section, subsection = strings.TrimPrefix(line, "## "), ""
			w.sections = append(w.sections, section)
		case strings.HasPrefix(line, "### "):
			flushPending()
			subsection = strings.TrimPrefix(line, "### ")
		case shellFence.MatchString(line):
			flushPending()
			var body []string
			body, i = fenced(lines, i)
			pending, pendingLine = commands(body), i
		case textFence.MatchString(line):
			var body []string
			start := i + 1
			body, i = fenced(lines, i)
			text := strings.Join(body, "\n")
			switch {
			case len(pending) == 1:
				w.steps = append(w.steps, step{section: section, line: pendingLine, command: pending[0], want: text})
				pending = nil
			case len(pending) > 1:
				w.problems = append(w.problems, fmt.Sprintf("line %d: a shell block with an output block holds %d commands, want one", pendingLine, len(pending)))
				pending = nil
			case subsection != "":
				w.messages[subsection] = text
			default:
				w.problems = append(w.problems, fmt.Sprintf("line %d: an output block follows no command", start))
			}
		case anyFence.MatchString(line):
			// The check would skip it, and a step in it would never run.
			flushPending()
			w.problems = append(w.problems, fmt.Sprintf("line %d: a code block opened with %q; commands go in sh blocks, their output in text blocks", i+1, strings.TrimSpace(line)))
			_, i = fenced(lines, i)
		case strings.HasPrefix(line, consoleLead):
			flushPending()
			start := i + 1
			var para []string
			for ; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
				para = append(para, strings.TrimSpace(lines[i]))
			}
			w.hints = append(w.hints, parseHint(strings.Join(para, " "), section, start, len(w.steps), &w.problems))
		case strings.TrimSpace(line) != "":
			flushPending()
		}
	}
	flushPending()
	return w
}

// fenced returns the lines of the code block that opens at lines[i] and
// the index of its closing fence.
func fenced(lines []string, i int) ([]string, int) {
	var body []string
	for i++; i < len(lines) && !anyFence.MatchString(lines[i]); i++ {
		body = append(body, lines[i])
	}
	return body, i
}

// commands returns the commands of a shell block, continuation lines
// joined, comments and blank lines dropped.
func commands(body []string) []string {
	var cmds []string
	pending := ""
	for _, line := range body {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if cont, ok := strings.CutSuffix(line, "\\"); ok {
			pending += cont
			continue
		}
		cmds = append(cmds, pending+line)
		pending = ""
	}
	return cmds
}

// parseHint reads a console hint paragraph.
func parseHint(para, section string, line, after int, problems *[]string) consoleHint {
	h := consoleHint{section: section, line: line, after: after}
	urls := consoleURL.FindAllStringSubmatch(para, -1)
	if len(urls) != 1 {
		*problems = append(*problems, fmt.Sprintf("line %d: a console hint names %d console pages, want one", line, len(urls)))
		return h
	}
	h.port, _ = strconv.Atoi(urls[0][1])
	h.path = urls[0][2]
	for _, m := range bold.FindAllStringSubmatch(strings.TrimPrefix(para, consoleLead), -1) {
		h.phrases = append(h.phrases, strings.Join(strings.Fields(m[1]), " "))
	}
	if len(h.phrases) == 0 {
		*problems = append(*problems, fmt.Sprintf("line %d: a console hint sets nothing in bold that the page shows", line))
	}
	return h
}

// volatile is what differs from one run of the sandbox to the next, in
// the order it is replaced.
var volatile = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`12D3KooW[1-9A-HJ-NP-Za-km-z]+`), "<peer ID>"},
	{regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`), "<event ID>"},
	{regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z`), "<time>"},
	{regexp.MustCompile(`\b(\d+d)?(\d+h)?(\d+m)?\d+(\.\d+)?(s|ms|µs|ns)\b`), "<duration>"},
	{regexp.MustCompile(`/ip4/\d+\.\d+\.\d+\.\d+/`), "/ip4/<address>/"},
	// obiectl peers lists one address per open connection. Two nodes
	// that dial each other at the same moment both keep their connection,
	// the second dialed from any port if the mesh port is taken: a peer's
	// connections compare as the one to its mesh port.
	{regexp.MustCompile(`(/ip4/<address>/tcp/\d+,)*/ip4/<address>/tcp/4001\b(,/ip4/<address>/tcp/\d+)*`), "/ip4/<address>/tcp/4001"},
	{regexp.MustCompile(`127\.0\.0\.1:\d+`), "127.0.0.1:<port>"},
	{regexp.MustCompile(`\btoken [A-Za-z0-9_-]+`), "token <token>"},
}

// tableHeader is the header line of a table obiectl prints.
var tableHeader = regexp.MustCompile(`^[A-Z][A-Z()/-]*( [A-Z][A-Z()/-]*)+$`)

// normalize makes an output comparable across runs: what differs from run
// to run is replaced, runs of blanks (column widths) become one, and the
// rows of every table are sorted, since obiectl orders some tables by
// peer ID.
func normalize(out string) string {
	for _, v := range volatile {
		out = v.re.ReplaceAllString(out, v.with)
	}
	var blocks []string
	for _, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var rows []string
		for _, l := range strings.Split(block, "\n") {
			rows = append(rows, strings.Join(strings.Fields(l), " "))
		}
		if len(rows) > 2 && tableHeader.MatchString(rows[0]) {
			sort.Strings(rows[1:])
		}
		blocks = append(blocks, strings.Join(rows, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

// changes are the commands that change something: the check runs them
// once, and repeats every other until its output is what the page shows.
var changes = []string{"up", "down", "report", "revoke", "allow", "block", "unoverride"}

// changesSomething reports whether a ./sandbox command changes something.
func changesSomething(command string) bool {
	f := strings.Fields(command)
	if len(f) >= 2 && slices.Contains(changes, f[1]) {
		return true
	}
	// ./sandbox exec <node> obiectl <command> ...
	return len(f) >= 5 && f[1] == "exec" && f[3] == "obiectl" && slices.Contains(changes, f[4])
}
