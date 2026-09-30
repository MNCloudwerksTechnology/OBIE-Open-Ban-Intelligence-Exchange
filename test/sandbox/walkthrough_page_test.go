package sandbox

import (
	"net"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// story is the walkthrough's story, one section per step, in order
// (WP-1695): a verdict, no block from one publisher, a block from two, the
// explanation, an override, a revocation that lifts the block everywhere,
// and a stranger who cannot change anything.
var story = []string{
	"1. node1 detects an attack and publishes a verdict",
	"2. node3 receives it, but does not block yet",
	"3. node2 agrees, and node3 blocks",
	"4. Look up why node3 blocks the address",
	"5. Overrule the decision on node3",
	"6. Revoke a verdict, and the block disappears everywhere",
	"7. The stranger tries to get an address blocked, and fails",
}

const (
	startSection = "Start the sandbox"
	endSection   = "Remove the sandbox"
	// troubleSection holds what the script says when it cannot start.
	troubleSection = "When the sandbox does not start"
)

// defaultPort is node1's console port unless OBIE_SANDBOX_PORT says
// otherwise, as the script sets it.
var defaultPort = regexp.MustCompile(`(?m)^port=\$\{OBIE_SANDBOX_PORT:-(\d+)\}$`)

// TestWalkthroughTellsTheStory checks the form of the walkthrough, which
// `make sandbox-check` relies on: the story's sections in order between
// starting and removing the sandbox, every step a ./sandbox command with
// its output, every story section with a console page, the consoles at the
// ports the script uses.
func TestWalkthroughTellsTheStory(t *testing.T) {
	w := parseWalkthrough(readRepoFile(t, walkthroughPath))
	for _, p := range w.problems {
		t.Errorf("%s: %s", walkthroughPath, p)
	}
	if w.title != "Try OBIE in a sandbox" {
		t.Errorf("title %q, want \"Try OBIE in a sandbox\"", w.title)
	}
	want := append(append([]string{startSection}, story...), endSection)
	var got []string
	for _, s := range w.sections {
		if slices.Contains(want, s) {
			got = append(got, s)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("the walkthrough's sections are %q, want %q in this order", got, want)
	}
	if len(w.steps) < 2 || w.steps[0].command != "./sandbox up" || w.steps[len(w.steps)-1].command != "./sandbox down" {
		t.Fatal("the walkthrough does not start with ./sandbox up and end with ./sandbox down")
	}
	if w.steps[0].section != startSection || w.steps[len(w.steps)-1].section != endSection {
		t.Errorf("./sandbox up and ./sandbox down are not in %q and %q", startSection, endSection)
	}
	for _, s := range w.steps {
		if !strings.HasPrefix(s.command, "./sandbox ") {
			t.Errorf("line %d: step %q is not a ./sandbox command", s.line, s.command)
		}
		if strings.TrimSpace(s.want) == "" {
			t.Errorf("line %d: step %q shows no output", s.line, s.command)
		}
	}
	for _, c := range w.setup {
		if !strings.HasPrefix(c, "git clone ") && c != "cd packaging/sandbox" && !strings.HasSuffix(c, "/packaging/sandbox") {
			t.Errorf("setup command %q, want git clone or cd into packaging/sandbox", c)
		}
	}
	for _, section := range story {
		if !slices.ContainsFunc(w.steps, func(s step) bool { return s.section == section }) {
			t.Errorf("%q has no step", section)
		}
		if !slices.ContainsFunc(w.hints, func(h consoleHint) bool { return h.section == section }) {
			t.Errorf("%q does not show the console", section)
		}
	}

	m := defaultPort.FindStringSubmatch(readRepoFile(t, "packaging/sandbox/sandbox"))
	if m == nil {
		t.Fatal("packaging/sandbox/sandbox sets no default port")
	}
	first, _ := strconv.Atoi(m[1])
	for _, h := range w.hints {
		if h.port < first || h.port > first+2 {
			t.Errorf("line %d: the console hint uses port %d; the consoles are on %d to %d", h.line, h.port, first, first+2)
		}
	}
}

// TestParseWalkthroughRejectsOtherBlocks checks that a code block the
// check would not run is a mistake in the page's form, not skipped.
func TestParseWalkthroughRejectsOtherBlocks(t *testing.T) {
	page := "# Try OBIE in a sandbox\n\n## Start the sandbox\n\n" +
		"```console\n./sandbox exec node3 obiectl decisions\n```\n\n" +
		"```text\nNo decisions.\n```\n"
	w := parseWalkthrough(page)
	if len(w.steps) != 0 || !slices.ContainsFunc(w.problems, func(p string) bool {
		return strings.Contains(p, "line 5: a code block opened with \"```console\"")
	}) {
		t.Errorf("a console block gave the steps %+v and the problems %q; want no step and the block named as a problem", w.steps, w.problems)
	}
}

// troubles are the problems the troubleshooting section shows, by heading,
// and how the script's test provokes each.
var troubles = []struct {
	heading string
	env     []string
}{
	{"Docker is not installed", []string{"NO_DOCKER=1"}},
	{"Docker refuses your user", []string{"FAKE_INFO=denied"}},
	{"Docker does not answer", []string{"FAKE_INFO=down"}},
	{"The Compose plugin is missing", []string{"FAKE_COMPOSE=missing"}},
	{"A console port is taken", nil},
}

// TestTroubleshootingShowsTheScriptsMessages checks that the walkthrough
// shows, for a missing or unreachable Docker and a taken port, exactly
// what ./sandbox up says.
func TestTroubleshootingShowsTheScriptsMessages(t *testing.T) {
	page := readRepoFile(t, walkthroughPath)
	w := parseWalkthrough(page)
	if !slices.Contains(w.sections, troubleSection) {
		t.Fatalf("the walkthrough has no section %q", troubleSection)
	}
	for _, tt := range troubles {
		t.Run(tt.heading, func(t *testing.T) {
			want, ok := w.messages[tt.heading]
			if !ok {
				t.Fatalf("%q has no subsection %q with the message", troubleSection, tt.heading)
			}
			var got string
			if tt.env == nil {
				got = portTakenMessage(t)
			} else {
				got = runScript(t, tt.env, "up").stderr
			}
			if strings.TrimSpace(got) != strings.TrimSpace(want) {
				t.Errorf("./sandbox up says:\n%s\nthe walkthrough shows:\n%s", got, want)
			}
		})
	}
}

// portTakenMessage returns what ./sandbox up says when a program listens
// on node3's console port, written for the default ports.
func portTakenMessage(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ss"); err != nil {
		if _, err := exec.LookPath("lsof"); err != nil {
			t.Skip("neither ss nor lsof is installed")
		}
	}
	port := freePort(t)
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port+2))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	out := runScript(t, []string{"OBIE_SANDBOX_PORT=" + strconv.Itoa(port)}, "up").stderr
	for i := 2; i >= 0; i-- {
		out = strings.ReplaceAll(out, strconv.Itoa(port+i), strconv.Itoa(9401+i))
	}
	return out
}

// TestNormalize checks what the walkthrough check ignores in an output:
// what differs from run to run, column widths and the order of table rows.
func TestNormalize(t *testing.T) {
	run1 := "Reported ipv4:1.2.3.4: verdict 01a0ee5d-d1ac-7373-9a73-d4c583fb1200 issued and published.\n\n" +
		"Issued:      2026-09-29T18:12:01Z\n" +
		"Expires:     2026-10-06T18:12:01Z (7d)\n\n" +
		"PEER ID                                               NAME   TRUST  LATENCY  ADDRESSES\n" +
		"12D3KooWASb79ySv3yLiMxeFxnmcYcDSxXoGd84i6gKHRhh6kHzh  node2  1      100µs    /ip4/172.23.0.3/tcp/4001\n" +
		"12D3KooWJ39v5j4Xnb4YoBLrhvVJV5iQybeQiwakhDc5XBin3QF8  node1  1      200µs    /ip4/172.23.0.4/tcp/4001\n\n" +
		"node3  http://127.0.0.1:9403/  token mE_pcc-RkoSE95a1cqEZ5xiN19cVyfZ31yIymMFX2Ds\n" +
		"1.2.3.4/32  2026-10-06T18:12:07Z  6d23h59m57s\n"
	run2 := "Reported ipv4:1.2.3.4: verdict 01a0ee62-a5b7-7e10-82b0-9ad9da5bd0a8 issued and published.\n\n" +
		"Issued: 2026-09-30T08:00:00Z\n" +
		"Expires: 2026-10-07T08:00:00Z (7d)\n\n" +
		"PEER ID NAME TRUST LATENCY ADDRESSES\n" +
		"12D3KooWKvGetPStEJmgRG1k2kuVb3TyfSZz9fvabEq1HBHoogGF node1 1 1.2ms /ip4/172.18.0.2/tcp/4001\n" +
		"12D3KooWH1bVLmkqCEXCfUHqKQ42VDRibnVo3tbgRajXDCg2Qt32 node2 1 300µs /ip4/172.18.0.5/tcp/4001\n\n" +
		"node3 http://127.0.0.1:19403/ token SPOxHIIpn1QczycnrwFQjAUcdZYkZFkWnbVEkHGKJmM\n" +
		"1.2.3.4/32 2026-10-07T08:00:00Z 6d23h59m58s"
	// Two nodes that dialed each other at the same moment: node1 and node2
	// each have a second connection, node2's from a port of its own.
	run3 := strings.NewReplacer("/ip4/172.18.0.2/tcp/4001\n", "/ip4/172.18.0.2/tcp/4001,/ip4/172.18.0.2/tcp/4001\n",
		"/ip4/172.18.0.5/tcp/4001\n", "/ip4/172.18.0.5/tcp/35804,/ip4/172.18.0.5/tcp/4001\n").Replace(run2)
	for _, run := range []string{run2, run3} {
		if a, b := normalize(run1), normalize(run); a != b {
			t.Errorf("two runs of the same story differ:\n%s\n---\n%s", a, b)
		}
	}
	for _, differs := range []string{
		strings.Replace(run1, "(7d)", "(12h)", 1),
		strings.Replace(run1, "node2  1", "node2  0", 1),
		strings.Replace(run1, "1.2.3.4/32", "1.2.3.5/32", 1),
		strings.Replace(run1, "/ip4/172.23.0.3/tcp/4001", "/ip4/172.23.0.3/tcp/4002", 1),
		strings.Replace(run1, "/ip4/172.23.0.3/tcp/4001", "/ip4/172.23.0.3/tcp/35804,/ip4/172.23.0.3/tcp/35805", 1),
	} {
		if normalize(differs) == normalize(run1) {
			t.Errorf("normalize hides a real difference:\n%s", differs)
		}
	}
}

// TestChangesSomething checks which commands the walkthrough check runs
// only once.
func TestChangesSomething(t *testing.T) {
	for cmd, want := range map[string]bool{
		"./sandbox up":   true,
		"./sandbox down": true,
		"./sandbox exec node1 obiectl report --protocol ssh --reason password_bruteforce 1.2.3.4": true,
		"./sandbox exec node1 obiectl revoke 1.2.3.4":                                             true,
		"./sandbox exec node3 obiectl allow 1.2.3.4":                                              true,
		"./sandbox exec node3 obiectl unoverride 1.2.3.4":                                         true,
		"./sandbox exec node3 obiectl explain 1.2.3.4":                                            false,
		"./sandbox exec node3 obiectl enforced":                                                   false,
		"./sandbox console node3":                                                                 false,
	} {
		if got := changesSomething(cmd); got != want {
			t.Errorf("changesSomething(%q) = %v, want %v", cmd, got, want)
		}
	}
}
