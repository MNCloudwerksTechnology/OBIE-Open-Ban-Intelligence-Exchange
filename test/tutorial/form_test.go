package tutorial

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// story is the tutorial's path, one section per step, in order (WP-1696):
// no choice to make before the node runs, and enforcement last.
var story = []string{
	"1. Check the requirements",
	"2. Install OBIE",
	"3. Set up the node",
	"4. Start the node in observe mode",
	"5. Let the node check itself",
	"6. Connect Fail2Ban",
	"7. See the first verdict",
	"8. Connect to a peer",
	"9. Review what would be blocked",
	"10. Switch to enforcement (optional)",
}

const (
	title = "Get started with OBIE"
	// installSection installs from the release archive, the tutorial's
	// path; containerSection installs the container image, apart from it.
	installSection   = "2. Install OBIE"
	containerSection = "Install the container image instead"
	// noFail2BanSection is for a server without Fail2Ban.
	noFail2BanSection = "If this server has no Fail2Ban"
	peerSection       = "8. Connect to a peer"
	enforceSection    = "10. Switch to enforcement (optional)"
	testedSection     = "How this page is tested"
	nextSection       = "What next"
	// sessionAddress is the address the reader's SSH session comes from
	// on the page, and in the check.
	sessionAddress = "85.10.3.20"
	// troubleshooting is where a step sends the reader when its result
	// differs.
	troubleshooting = "operations/troubleshooting.md"
)

var (
	// mdLink is the target of an inline Markdown link.
	mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// sentenceEnd ends a sentence.
	sentenceEnd = regexp.MustCompile(`[.!?]["'”’*_)]*(\s|$)`)
	// inlineCode and linkTarget are not running text.
	inlineCode = regexp.MustCompile("`[^`]*`")
	linkTarget = regexp.MustCompile(`\]\([^)]*\)`)
	// abbreviation would otherwise end a sentence.
	abbreviation = regexp.MustCompile(`\b(e\.g|i\.e|etc)\.`)
)

// TestTutorialTakesOnePath checks the page's form, which `make
// tutorial-check` relies on: the steps in the order of the story, then
// the other ways and the edge cases, "What next" last; only shell blocks
// and their output; every step with one sentence that says what it is for,
// at least one expected output and a link to troubleshooting for when the
// result differs.
func TestTutorialTakesOnePath(t *testing.T) {
	p := parsePage(readRepoFile(t, tutorialPath))
	for _, problem := range p.problems {
		t.Errorf("%s: %s", tutorialPath, problem)
	}
	if p.title != title {
		t.Errorf("title %q, want %q", p.title, title)
	}
	// Exactly these: another section in between would be a way to choose.
	want := append(slices.Clone(story), noFail2BanSection, containerSection, testedSection, nextSection)
	var got []string
	for _, s := range p.sections {
		got = append(got, s.title)
	}
	if !slices.Equal(got, want) {
		t.Errorf("the tutorial's sections are %q, want %q in this order, and no others", got, want)
	}
	for _, title := range story {
		s, ok := p.section(title)
		if !ok {
			continue
		}
		if n := countSentences(s.purpose); n != 1 {
			t.Errorf("%q starts with %d sentences, want one that says what the step is for: %q", title, n, s.purpose)
		}
		if !slices.ContainsFunc(p.steps, func(st step) bool { return st.section == title && st.hasWant }) {
			t.Errorf("%q shows no expected output", title)
		}
		if !slices.ContainsFunc(links(s.paragraphs), func(l string) bool { return strings.HasPrefix(l, troubleshooting) }) {
			t.Errorf("%q does not link %s for when the result differs", title, troubleshooting)
		}
	}
}

// TestTutorialInstallsEachWayApart checks that the release archive and the
// container image are installed in sections of their own, and that only
// the container's section uses Docker.
func TestTutorialInstallsEachWayApart(t *testing.T) {
	p := parsePage(readRepoFile(t, tutorialPath))
	archive := p.commandsOf(installSection)
	if !slices.ContainsFunc(archive, func(c string) bool { return strings.Contains(c, "install.sh") }) {
		t.Errorf("%q does not run install.sh from the release archive", installSection)
	}
	container := p.commandsOf(containerSection)
	if !slices.ContainsFunc(container, func(c string) bool { return strings.HasPrefix(c, "docker run ") }) {
		t.Errorf("%q does not start the container image", containerSection)
	}
	for _, s := range p.steps {
		if len(s.commands) == 0 {
			continue // a problem TestTutorialTakesOnePath reports
		}
		docker := strings.HasPrefix(s.commands[0], "docker ")
		if docker != (s.section == containerSection) {
			t.Errorf("line %d: %q in %q; Docker commands belong in %q, and only there", s.line, s.commands[0], s.section, containerSection)
		}
	}
}

// TestTutorialProtectsAccessBeforeEnforcing checks that the reader confirms
// that their own address is protected, and learns the way back, before the
// command that switches enforcement on.
func TestTutorialProtectsAccessBeforeEnforcing(t *testing.T) {
	p := parsePage(readRepoFile(t, tutorialPath))
	cmds := p.commandsOf(enforceSection)
	switchOn := slices.IndexFunc(cmds, func(c string) bool { return strings.Contains(c, "mode: enforce/") })
	if switchOn < 0 {
		t.Fatalf("%q has no command that sets mode: enforce", enforceSection)
	}
	for _, before := range []string{
		"sudo obied self-check",                  // the SSH session line
		"sudo obiectl explain " + sessionAddress, // Decision: allowed
		"sudo obied teardown-firewall",           // the way back after a lockout
		"mode: observe/",                         // the way back to observe mode
	} {
		i := slices.IndexFunc(cmds, func(c string) bool { return strings.Contains(c, before) })
		if i < 0 || i > switchOn {
			t.Errorf("%q does not run %q before it switches enforcement on", enforceSection, before)
		}
	}
	// What the reader confirms is on the page.
	for cmd, shows := range map[string]string{
		"sudo obied self-check":                  "your SSH session comes from " + sessionAddress + ", which is protected",
		"sudo obiectl explain " + sessionAddress: "Decision: allowed",
	} {
		if !slices.ContainsFunc(p.steps, func(s step) bool {
			return s.section == enforceSection && s.hasWant && s.commands[0] == cmd &&
				strings.Contains(strings.Join(normalize(s.want), "\n"), shows)
		}) {
			t.Errorf("%q does not show that %s prints %q", enforceSection, cmd, shows)
		}
	}
}

// TestTutorialCoversTheEdgeCases checks the ways without Fail2Ban and
// without a peer: report by hand and install Fail2Ban; skip the peer and
// add it later.
func TestTutorialCoversTheEdgeCases(t *testing.T) {
	p := parsePage(readRepoFile(t, tutorialPath))
	cmds := p.commandsOf(noFail2BanSection)
	for _, want := range []string{"sudo obiectl report ", "sudo apt install fail2ban", "install.sh"} {
		if !slices.ContainsFunc(cmds, func(c string) bool { return strings.Contains(c, want) }) {
			t.Errorf("%q does not run %q", noFail2BanSection, want)
		}
	}
	for title, lead := range map[string]string{"6. Connect Fail2Ban": "**No Fail2Ban?**", peerSection: "**No peer yet?**"} {
		s, _ := p.section(title)
		if !slices.ContainsFunc(s.paragraphs, func(para string) bool { return strings.HasPrefix(para, lead) }) {
			t.Errorf("%q has no paragraph that starts with %s", title, lead)
		}
	}
}

// TestTutorialEndsWithWhatNext checks the links the tutorial ends with.
func TestTutorialEndsWithWhatNext(t *testing.T) {
	p := parsePage(readRepoFile(t, tutorialPath))
	s, ok := p.section(nextSection)
	if !ok {
		t.Fatalf("the tutorial has no section %q", nextSection)
	}
	got := links(s.paragraphs)
	for what, prefix := range map[string]string{
		"how-to guides":   "guides/",
		"the web console": "operations/console.md",
		"federation":      "operations/federation.md",
		"operations":      "operations/operations.md",
	} {
		if !slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, prefix) }) {
			t.Errorf("%q does not link %s (%s…)", nextSection, what, prefix)
		}
	}
}

// links returns the targets of the links in the paragraphs.
func links(paragraphs []string) []string {
	var out []string
	for _, para := range paragraphs {
		for _, m := range mdLink.FindAllStringSubmatch(para, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// countSentences counts the sentences of a paragraph; inline code, link
// targets and common abbreviations end none.
func countSentences(para string) int {
	text := inlineCode.ReplaceAllString(para, "code")
	text = linkTarget.ReplaceAllString(text, "]")
	text = abbreviation.ReplaceAllString(text, "$1")
	return len(sentenceEnd.FindAllString(strings.TrimSpace(text), -1))
}
