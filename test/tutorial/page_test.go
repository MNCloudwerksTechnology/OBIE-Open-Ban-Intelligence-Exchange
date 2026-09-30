package tutorial

import (
	"fmt"
	"regexp"
	"strings"
)

// tutorialPath is the tutorial, relative to the repository root.
const tutorialPath = "documentation/getting-started.md"

// A step is a shell block of the tutorial: its commands and, if a text
// block follows it, the output the page shows for its one command. line is
// the line of the block's opening fence.
type step struct {
	section    string
	subsection string
	line       int
	commands   []string
	want       string
	hasWant    bool
}

// A section is a level-2 heading of the tutorial with its running text.
type section struct {
	title string
	// purpose is the paragraph that opens the section, if one does.
	purpose string
	// paragraphs are the section's paragraphs and list items, each joined
	// into one line, without code blocks.
	paragraphs []string
	// started is set once the section holds anything.
	started bool
}

// page is documentation/getting-started.md as the tests read it.
type page struct {
	title    string
	sections []section
	steps    []step
	// problems are mistakes in the page's form.
	problems []string
}

var (
	shellFence = regexp.MustCompile("^```(sh|shell|bash)\\s*$")
	textFence  = regexp.MustCompile("^```text\\s*$")
	anyFence   = regexp.MustCompile("^\\s*```")
	// heredoc opens a here-document; its lines belong to the command.
	heredoc = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_]+)['"]?`)
	// listItem starts a list item, which is a paragraph of its own.
	listItem = regexp.MustCompile(`^\s*([-*]|\d+\.) `)
)

// parsePage reads the tutorial page.
func parsePage(doc string) page {
	var p page
	lines := strings.Split(doc, "\n")
	cur := -1 // the current section
	subsection := ""
	var pending *step // the last shell block, until an output block or text follows
	var para []string
	flushStep := func() {
		if pending != nil {
			p.steps = append(p.steps, *pending)
			pending = nil
		}
	}
	// mark notes that the current section holds something.
	mark := func() {
		if cur >= 0 {
			p.sections[cur].started = true
		}
	}
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		text := strings.Join(para, " ")
		para = nil
		if cur < 0 {
			return
		}
		s := &p.sections[cur]
		if !s.started {
			s.purpose = text
		}
		s.started = true
		s.paragraphs = append(s.paragraphs, text)
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "# "):
			flushPara()
			p.title = strings.TrimPrefix(line, "# ")
		case strings.HasPrefix(line, "## "):
			flushPara()
			flushStep()
			p.sections = append(p.sections, section{title: strings.TrimPrefix(line, "## ")})
			cur, subsection = len(p.sections)-1, ""
		case strings.HasPrefix(line, "### "):
			flushPara()
			flushStep()
			mark()
			subsection = strings.TrimPrefix(line, "### ")
		case shellFence.MatchString(line):
			flushPara()
			flushStep()
			mark()
			start := i + 1
			var body []string
			body, i = fenced(lines, i)
			pending = &step{subsection: subsection, line: start, commands: commands(body)}
			if cur >= 0 {
				pending.section = p.sections[cur].title
			}
			if len(pending.commands) == 0 {
				p.problems = append(p.problems, fmt.Sprintf("line %d: a shell block without a command", start))
			}
		case textFence.MatchString(line):
			flushPara()
			mark()
			start := i + 1
			var body []string
			body, i = fenced(lines, i)
			switch {
			case pending == nil:
				p.problems = append(p.problems, fmt.Sprintf("line %d: an output block follows no command; show examples in the text", start))
			case len(pending.commands) != 1:
				p.problems = append(p.problems, fmt.Sprintf("line %d: a shell block with an output block holds %d commands, want one", pending.line, len(pending.commands)))
			default:
				pending.want, pending.hasWant = strings.Join(body, "\n"), true
			}
			flushStep()
		case anyFence.MatchString(line):
			// The check would skip it, and a command in it would never run.
			flushPara()
			flushStep()
			mark()
			p.problems = append(p.problems, fmt.Sprintf("line %d: a code block opened with %q; commands go in sh blocks, their output in text blocks", i+1, strings.TrimSpace(line)))
			_, i = fenced(lines, i)
		case strings.TrimSpace(line) == "":
			flushPara()
		default:
			flushStep()
			if listItem.MatchString(line) {
				flushPara()
			}
			para = append(para, strings.TrimSpace(line))
		}
	}
	flushPara()
	flushStep()
	return p
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

// commands returns the commands of a shell block: continuation lines
// joined, the lines of a here-document kept with its command, comments
// and blank lines dropped.
func commands(body []string) []string {
	var cmds []string
	pending := ""
	for i := 0; i < len(body); i++ {
		line := strings.TrimSpace(body[i])
		if pending == "" && (line == "" || strings.HasPrefix(line, "#")) {
			continue
		}
		if cont, ok := strings.CutSuffix(line, "\\"); ok {
			pending += cont
			continue
		}
		cmd := pending + line
		pending = ""
		if m := heredoc.FindStringSubmatch(cmd); m != nil {
			for i++; i < len(body); i++ {
				cmd += "\n" + body[i]
				if strings.TrimSpace(body[i]) == m[1] {
					break
				}
			}
		}
		cmds = append(cmds, cmd)
	}
	return cmds
}

// section returns the section with the title, and false if the page has
// none.
func (p page) section(title string) (section, bool) {
	for _, s := range p.sections {
		if s.title == title {
			return s, true
		}
	}
	return section{}, false
}

// commandsOf returns the commands of a section's shell blocks, in order.
func (p page) commandsOf(title string) []string {
	var cmds []string
	for _, s := range p.steps {
		if s.section == title {
			cmds = append(cmds, s.commands...)
		}
	}
	return cmds
}
