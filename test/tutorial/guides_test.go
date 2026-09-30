package tutorial

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	// guidesDir holds the how-to guides and guideIndex, the index that
	// groups them by goal (ADR 0031).
	guidesDir  = "documentation/guides"
	guideIndex = "README.md"
	// guideTitle opens the title of every how-to guide, which asks the
	// task's question.
	guideTitle = "How do I "
	// warningLead opens the warning of a guide that changes the firewall.
	warningLead = "> **Warning:**"
)

// The sections of a how-to guide.
const (
	beforeSection = "Before you start"
	stepsSection  = "Steps"
	checkSection  = "Check that it worked"
	undoSection   = "Undo"
)

var (
	// guideSections are the sections of a guide, in order; firewallSections
	// those of a guide that changes the firewall, which shows the way back
	// before the steps.
	guideSections    = []string{beforeSection, stepsSection, checkSection, undoSection}
	firewallSections = []string{beforeSection, undoSection, stepsSection, checkSection}
	// runOrder is the order the check runs the sections in, whatever order
	// the page shows them in.
	runOrder = guideSections
	// firewallCommand adds or lifts blocks, or removes OBIE's table: a
	// guide that runs one changes the firewall.
	firewallCommand = regexp.MustCompile(`\bobied teardown-firewall\b|\bobiectl (allow|block|unoverride|revoke)\b|\bmode: (enforce|observe)\b|\bweight: `)
	// stepLink links a step of the tutorial.
	stepLink = regexp.MustCompile(`^\.\./getting-started\.md#(\d+-[a-z0-9-]+)$`)
)

// A guide is a how-to guide: a page that asks "How do I …?".
type guide struct {
	file string // its file name in guidesDir
	page
	// intro is the text between the title and the first section, the
	// warning included.
	intro []string
	// prerequisite is the step of the tutorial that the node must have
	// reached: the section of the tutorial that "Before you start" links.
	prerequisite string
}

// readGuides reads every how-to guide in guidesDir, by file name.
func readGuides(t *testing.T) []guide {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot, guidesDir))
	if err != nil {
		t.Fatal(err)
	}
	var guides []guide
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == guideIndex {
			continue
		}
		doc := readRepoFile(t, filepath.Join(guidesDir, e.Name()))
		p := parsePage(doc)
		if !strings.HasPrefix(p.title, guideTitle) {
			continue // a guide about a topic, such as fail2ban.md
		}
		g := guide{file: e.Name(), page: p, intro: introOf(doc)}
		for i := range g.steps {
			g.steps[i].file = filepath.Join(guidesDir, e.Name())
		}
		g.prerequisite = prerequisiteOf(g)
		guides = append(guides, g)
	}
	if len(guides) == 0 {
		t.Fatalf("%s holds no how-to guide", guidesDir)
	}
	return guides
}

// introOf returns the lines between a page's title and its first section.
func introOf(doc string) []string {
	var intro []string
	started := false
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case strings.HasPrefix(line, "# "):
			started = true
		case strings.HasPrefix(line, "## "):
			return intro
		case started:
			intro = append(intro, line)
		}
	}
	return intro
}

// prerequisiteOf returns the tutorial's section that "Before you start"
// links, or "" if it links none or several.
func prerequisiteOf(g guide) string {
	s, _ := g.section(beforeSection)
	var found []string
	for _, l := range links(s.paragraphs) {
		m := stepLink.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		for _, title := range story {
			if anchorOf(title) == m[1] {
				found = append(found, title)
			}
		}
	}
	if len(found) != 1 {
		return ""
	}
	return found[0]
}

// anchorOf returns the anchor GitHub gives a heading.
func anchorOf(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

// changesFirewall reports whether a guide runs a command that adds or
// lifts blocks, or removes OBIE's table.
func (g guide) changesFirewall() bool {
	return slices.ContainsFunc(g.steps, func(s step) bool {
		return slices.ContainsFunc(s.commands, firewallCommand.MatchString)
	})
}

// problemsOf returns what is wrong with a guide's form.
func (g guide) problemsOf() []string {
	problems := slices.Clone(g.problems)
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if !strings.HasSuffix(g.title, "?") {
		add("the title %q asks no question", g.title)
	}
	want := guideSections
	if g.changesFirewall() {
		want = firewallSections
	}
	var got []string
	for _, s := range g.sections {
		got = append(got, s.title)
	}
	if !slices.Equal(got, want) {
		add("the sections are %q, want %q in this order, and no others", got, want)
	}
	if !slices.ContainsFunc(g.intro, func(l string) bool { l = strings.TrimSpace(l); return l != "" && !strings.HasPrefix(l, ">") }) {
		add("no paragraph between the title and the first section says what the guide does")
	}
	warns := slices.ContainsFunc(g.intro, func(l string) bool { return strings.HasPrefix(l, warningLead) })
	if warns != g.changesFirewall() {
		add("changes the firewall: %v, but warns (a line that starts with %q before the first section): %v", g.changesFirewall(), warningLead, warns)
	}
	if g.prerequisite == "" {
		add("%q does not link exactly one step of the tutorial (../getting-started.md#<step>) as the node's starting point", beforeSection)
	}
	if !slices.ContainsFunc(g.steps, func(s step) bool { return s.section == stepsSection }) {
		add("%q runs no command", stepsSection)
	}
	if !slices.ContainsFunc(g.steps, func(s step) bool { return s.section == checkSection && s.hasWant }) {
		add("%q shows no command with its expected output", checkSection)
	}
	if s, _ := g.section(undoSection); !s.started {
		add("%q says nothing", undoSection)
	}
	for _, s := range g.steps {
		if s.subsection != "" {
			add("line %d: a subsection %q; a guide has only its four sections", s.line, s.subsection)
		}
	}
	return problems
}

// TestGuidesKeepTheirForm checks every how-to guide's form, which `make
// guides-check` relies on (ADR 0031): a question as its title, what it
// does, the four sections in order, the warning and the way back first for
// a guide that changes the firewall, a step of the tutorial to start from,
// a command with expected output to check the result, and only blocks the
// check runs.
func TestGuidesKeepTheirForm(t *testing.T) {
	for _, g := range readGuides(t) {
		for _, problem := range g.problemsOf() {
			t.Errorf("%s/%s: %s", guidesDir, g.file, problem)
		}
	}
}

// TestGuideFormFindsMistakes checks that the form test finds what it
// should: a guide that changes the firewall without a warning or with the
// way back last, no step of the tutorial to start from, and nothing to
// check the result with.
func TestGuideFormFindsMistakes(t *testing.T) {
	const good = "# How do I block an address?\n\n" + warningLead + " It blocks.\n\nBlock it.\n\n" +
		"## Before you start\n\n- A node as in [Get started](../getting-started.md#4-start-the-node-in-observe-mode).\n\n" +
		"## Undo\n\n```sh\nsudo obiectl unoverride 85.10.0.9\n```\n\n" +
		"## Steps\n\n```sh\nsudo obiectl block 85.10.0.9\n```\n\n" +
		"## Check that it worked\n\n```sh\nsudo obiectl explain 85.10.0.9\n```\n\n```text\nDecision: block\n```\n"
	parse := func(doc string) guide {
		g := guide{page: parsePage(doc), intro: introOf(doc)}
		g.prerequisite = prerequisiteOf(g)
		return g
	}
	if problems := parse(good).problemsOf(); len(problems) > 0 {
		t.Fatalf("a guide in form has the problems %q", problems)
	}
	if got := parse(good).prerequisite; got != "4. Start the node in observe mode" {
		t.Errorf("prerequisite %q", got)
	}
	for mistake, doc := range map[string]string{
		"no warning":           strings.Replace(good, warningLead+" It blocks.\n\n", "", 1),
		"the way back last":    strings.Replace(strings.Replace(good, "## Undo\n\n```sh\nsudo obiectl unoverride 85.10.0.9\n```\n\n", "", 1), "```text\nDecision: block\n```\n", "```text\nDecision: block\n```\n\n## Undo\n\nUnblock it.\n", 1),
		"no step of tutorial":  strings.Replace(good, "#4-start-the-node-in-observe-mode", "", 1),
		"nothing to check":     strings.Replace(good, "\n\n```text\nDecision: block\n```\n", "\n", 1),
		"no question":          strings.Replace(good, "address?", "address", 1),
		"another code block":   strings.Replace(good, "```sh\nsudo obiectl block", "```console\nsudo obiectl block", 1),
		"a warning, no change": strings.NewReplacer("obiectl unoverride", "obiectl show", "obiectl block", "obiectl show").Replace(good),
	} {
		if len(parse(doc).problemsOf()) == 0 {
			t.Errorf("%s: the form test finds no problem in\n%s", mistake, doc)
		}
	}
}

// testedGuidesSection of the index says how the guides are tested; the
// sections before it are the goals the guides are grouped by.
const testedGuidesSection = "How the guides are tested"

// TestGuideIndexListsEveryGuide checks the index of the how-to guides:
// every guide is listed once, in a section of the goal it serves, and the
// index ends with how the guides are tested.
func TestGuideIndexListsEveryGuide(t *testing.T) {
	p := parsePage(readRepoFile(t, filepath.Join(guidesDir, guideIndex)))
	if len(p.sections) == 0 || p.sections[len(p.sections)-1].title != testedGuidesSection {
		t.Fatalf("the index's last section is not %q", testedGuidesSection)
	}
	listed := map[string]int{}
	for _, s := range p.sections[:len(p.sections)-1] {
		var guides int
		for _, l := range links(s.paragraphs) {
			if !strings.Contains(l, "/") && strings.HasSuffix(l, ".md") {
				listed[l]++
				guides++
			}
		}
		if guides == 0 {
			t.Errorf("the index's section %q lists no guide", s.title)
		}
		if s.purpose == "" || listItem.MatchString(s.purpose) {
			t.Errorf("the index's section %q does not start by saying what its guides are for", s.title)
		}
	}
	for _, g := range readGuides(t) {
		if listed[g.file] != 1 {
			t.Errorf("the index lists %s %d times, want once", g.file, listed[g.file])
		}
		delete(listed, g.file)
	}
	for file := range listed {
		if _, err := os.Stat(filepath.Join(repoRoot, guidesDir, file)); err != nil {
			t.Errorf("the index lists %s: %v", file, err)
		}
	}
}

// tasks are the routine jobs of an operator (WP-1697), each with the guide
// that asks how to do it.
var tasks = []struct{ task, guide, title string }{
	{"connect with a friend's or partner's node and choose a sensible trust level", "connect-a-peer.md", "How do I connect with a friend's node and choose a trust level?"},
	{"review what my node would block before enforcing", "review-what-would-be-blocked.md", "How do I review what my node would block before enforcing?"},
	{"find out why an address is blocked", "why-is-an-address-blocked.md", "How do I find out why an address is blocked?"},
	{"unblock an address I trust (a false positive) now and permanently", "unblock-an-address.md", "How do I unblock an address I trust, now and for good?"},
	{"block an address manually", "block-an-address.md", "How do I block an address manually?"},
	{"withdraw a verdict I published by mistake", "withdraw-a-verdict.md", "How do I withdraw a verdict I published by mistake?"},
	{"switch from observe to enforce, and back", "switch-enforcement.md", "How do I switch from observe to enforce, and back?"},
	{"stop trusting a peer", "stop-trusting-a-peer.md", "How do I stop trusting a peer?"},
	{"recover after locking myself out", "recover-from-a-lockout.md", "How do I recover after locking myself out?"},
	{"back up and restore the node identity", "back-up-the-identity.md", "How do I back up and restore the node identity?"},
	{"upgrade to a new release", "upgrade.md", "How do I upgrade to a new release?"},
	{"uninstall completely, including the firewall rules", "uninstall.md", "How do I uninstall OBIE completely, firewall rules included?"},
}

// consoleGuides are the guides whose task the web console makes easier:
// they show the console's way next to the command line's.
var consoleGuides = []string{
	"review-what-would-be-blocked.md",
	"why-is-an-address-blocked.md",
	"unblock-an-address.md",
	"block-an-address.md",
	"withdraw-a-verdict.md",
}

// TestEveryTaskHasAGuide checks that every routine job has its guide,
// titled as the task, and that there is no guide beyond them, so that the
// index and this list stay one.
func TestEveryTaskHasAGuide(t *testing.T) {
	guides := map[string]guide{}
	for _, g := range readGuides(t) {
		guides[g.file] = g
	}
	for _, task := range tasks {
		g, ok := guides[task.guide]
		switch {
		case !ok:
			t.Errorf("no guide %s/%s to %s", guidesDir, task.guide, task.task)
		case g.title != task.title:
			t.Errorf("%s/%s is titled %q, want %q", guidesDir, task.guide, g.title, task.title)
		}
		delete(guides, task.guide)
	}
	for file := range guides {
		t.Errorf("%s/%s is a guide to no task of this list; add its task", guidesDir, file)
	}
}

// TestGuidesShowTheConsoleWhereItIsEasier checks that the guides whose task
// the web console makes easier show its way, and that only they do.
func TestGuidesShowTheConsoleWhereItIsEasier(t *testing.T) {
	for _, g := range readGuides(t) {
		shows := len(g.hints) > 0
		if want := slices.Contains(consoleGuides, g.file); shows != want {
			t.Errorf("%s/%s shows the console: %v, want %v", guidesDir, g.file, shows, want)
		}
		for _, h := range g.hints {
			if h.section != stepsSection && h.section != checkSection {
				t.Errorf("%s/%s:%d: the console's way is in %q; it belongs in %q or %q", guidesDir, g.file, h.line, h.section, stepsSection, checkSection)
			}
		}
	}
}

// TestGuideIndexIsLinked checks that a reader finds the index of the guides
// from the pages they come from: the introduction, the tutorial and the
// README.
func TestGuideIndexIsLinked(t *testing.T) {
	for page, link := range map[string]string{
		"documentation/introduction.md":    "guides/README.md",
		"documentation/getting-started.md": "guides/README.md",
		"README.md":                        "documentation/guides/README.md",
	} {
		var all []string
		for _, s := range parsePage(readRepoFile(t, page)).sections {
			all = append(all, links(s.paragraphs)...)
		}
		if !slices.Contains(all, link) {
			t.Errorf("%s does not link %s", page, link)
		}
	}
}
