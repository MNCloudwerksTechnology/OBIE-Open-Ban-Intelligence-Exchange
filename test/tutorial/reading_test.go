package tutorial

import (
	"slices"
	"strings"
	"testing"
)

// TestParsePage checks how the check reads the page: a shell block with an
// output block is one command and its output, continuation lines and
// here-documents belong to their command, a section's first paragraph is
// its purpose.
func TestParsePage(t *testing.T) {
	doc := "# Get started with OBIE\n\n## 1. Check\n\nCheck the host.\nIt takes a minute.\n\n" +
		"```sh\nuname -sm\n```\n\n```text\nLinux x86_64\n```\n\n" +
		"- A list item with [a link](operations/troubleshooting.md#x).\n\n" +
		"```sh\nsudo tee /etc/x <<'EOF'\n[sshd]\n  obie\nEOF\nsudo obied setup --non-interactive \\\n  --allow 192.0.2.0/24\n```\n\n" +
		"## 2. Next\n\n```sh\ntrue\n```\n\nText after the block.\n"
	p := parsePage(doc)
	if len(p.problems) > 0 {
		t.Fatalf("problems: %q", p.problems)
	}
	if p.title != "Get started with OBIE" || len(p.sections) != 2 {
		t.Fatalf("title %q, sections %+v", p.title, p.sections)
	}
	s := p.sections[0]
	if s.purpose != "Check the host. It takes a minute." {
		t.Errorf("purpose %q", s.purpose)
	}
	if got := links(s.paragraphs); !slices.Equal(got, []string{"operations/troubleshooting.md#x"}) {
		t.Errorf("links %q", got)
	}
	if p.sections[1].purpose != "" {
		t.Errorf("a section that starts with a block has the purpose %q", p.sections[1].purpose)
	}
	want := []step{
		{section: "1. Check", line: 8, commands: []string{"uname -sm"}, want: "Linux x86_64", hasWant: true},
		{section: "1. Check", line: 18, commands: []string{"sudo tee /etc/x <<'EOF'\n[sshd]\n  obie\nEOF", "sudo obied setup --non-interactive --allow 192.0.2.0/24"}},
		{section: "2. Next", line: 29, commands: []string{"true"}},
	}
	if len(p.steps) != len(want) {
		t.Fatalf("steps %+v, want %+v", p.steps, want)
	}
	for i := range want {
		got := p.steps[i]
		if got.section != want[i].section || got.line != want[i].line || !slices.Equal(got.commands, want[i].commands) ||
			got.want != want[i].want || got.hasWant != want[i].hasWant {
			t.Errorf("step %d = %+v, want %+v", i, got, want[i])
		}
	}
}

// TestParsePageRejectsWhatTheCheckCannotRun checks that a code block the
// check would not run, an output block without a command and an output
// block after several commands are mistakes in the page's form.
func TestParsePageRejectsWhatTheCheckCannotRun(t *testing.T) {
	for doc, problem := range map[string]string{
		"## 1\n\n```console\n$ obiectl status\n```\n":          "line 3: a code block opened with \"```console\"",
		"## 1\n\n```yaml\nnode:\n  mode: enforce\n```\n":       "line 3: a code block opened with \"```yaml\"",
		"## 1\n\n```text\nMode: OBSERVE\n```\n":                "line 3: an output block follows no command",
		"## 1\n\n```sh\ntrue\nfalse\n```\n\n```text\nx\n```\n": "line 3: a shell block with an output block holds 2 commands",
		"## 1\n\n```sh\n# only a comment\n```\n":               "line 3: a shell block without a command",
	} {
		p := parsePage(doc)
		if !slices.ContainsFunc(p.problems, func(p string) bool { return strings.HasPrefix(p, problem) }) {
			t.Errorf("%q gave the problems %q, want %q", doc, p.problems, problem)
		}
	}
}

// TestMatches checks how an output is compared with the page: what differs
// from run to run, column widths and the page's wildcards are ignored,
// everything else is not.
func TestMatches(t *testing.T) {
	got := "Peer ID:      12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD\r\n" +
		"Fingerprint:  SHA256:iJQCoAB0CUvEzbf/YZDCIElsOo7GuqUNHru08MWXzVY\n" +
		"Reported ipv4:85.10.0.9: verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 issued and published.\n" +
		"Expires:     2026-10-06T22:42:08Z (7d)\n" +
		"85.10.0.7/32  2026-09-29T22:47:55Z  7m38s\n" +
		"elements = { 85.10.0.7 timeout 7m41s173ms expires 7m38s98ms }\n" +
		"OK Clock the clock is synchronized (estimated error 847ms)\n" +
		"2026-09-29 22:33:59,504 fail2ban.configreader [312]: WARNING 'allowipv6' not defined\n" +
		"OK: configuration test is successful\n"
	want := "Peer ID: 12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf\n" +
		"Fingerprint: SHA256:ZZZZoAB0CUvEzbf/YZDCIElsOo7GuqUNHru08MWXzVY\n" +
		"Reported ipv4:85.10.0.9: verdict 01a0ee6b-08e1-731d-a8b7-66c3c3a411b8 issued and published.\n" +
		"Expires:     2026-10-14T09:00:00Z (7d)\n" +
		"85.10.0.7/32  2026-10-14T09:10:00Z  9m2s\n" +
		"elements = { 85.10.0.7 timeout 9m59s999ms expires 9m58s7ms }\n" +
		"OK Clock the clock is synchronized (estimated error 12ms)\n" +
		"…\n" +
		"OK: configuration test is…\n"
	if !matches(want, got) {
		t.Errorf("matches(%q, %q) = false\nnormalized: %q\n%q", want, got, normalize(want), normalize(got))
	}
	for _, differs := range []string{
		strings.Replace(want, "(7d)", "(12h)", 1),
		strings.Replace(want, "85.10.0.9", "85.10.0.8", 1),
		strings.Replace(want, "Peer ID:", "Peer:", 1),
		strings.Replace(want, "…\n", "", 1),
		want + "one more line\n",
	} {
		if matches(differs, got) {
			t.Errorf("matches hides a real difference:\n%s", differs)
		}
	}
	for _, tt := range []struct{ want, got string }{
		{"…", ""},
		{"a\n…\nb", "a\nb"},
		{"a\n…\nb", "a\nx\ny\nb"},
		{"systemd 255 (…)", "systemd 255 (255.4-1ubuntu8.11)"},
		{"x…", "x"},
	} {
		if !matches(tt.want, tt.got) {
			t.Errorf("matches(%q, %q) = false", tt.want, tt.got)
		}
	}
}

// TestParsePageReadsConsoleHints checks how a page names a console page:
// a paragraph that starts with the console lead, one console page, the
// phrases in bold besides the lead, and the number of steps before it.
func TestParsePageReadsConsoleHints(t *testing.T) {
	doc := "## Steps\n\n```sh\nsudo obiectl explain 85.10.0.7\n```\n\n" +
		consoleLead + " the explanation,\n<" + consoleAddress + "/decisions/85.10.0.7>, says **Blocked** and\noffers **Always allow…**.\n"
	p := parsePage(doc)
	if len(p.problems) > 0 {
		t.Fatalf("problems: %q", p.problems)
	}
	want := consoleHint{section: "Steps", line: 7, after: 1, path: "/decisions/85.10.0.7", phrases: []string{"Blocked", "Always allow…"}}
	if len(p.hints) != 1 || p.hints[0].section != want.section || p.hints[0].line != want.line || p.hints[0].after != want.after ||
		p.hints[0].path != want.path || !slices.Equal(p.hints[0].phrases, want.phrases) {
		t.Errorf("hints %+v, want %+v", p.hints, want)
	}
	for doc, problem := range map[string]string{
		"## 1\n\n" + consoleLead + " <" + consoleAddress + "/a> and <" + consoleAddress + "/b> show **x**.\n": "line 3: a console hint names 2 pages",
		"## 1\n\n" + consoleLead + " the page <" + consoleAddress + "/peers> shows it.\n":                     "line 3: a console hint sets nothing in bold",
	} {
		p := parsePage(doc)
		if !slices.ContainsFunc(p.problems, func(p string) bool { return strings.HasPrefix(p, problem) }) {
			t.Errorf("%q gave the problems %q, want %q", doc, p.problems, problem)
		}
	}
}
