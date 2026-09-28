package docs

import (
	"regexp"
	"strings"
)

var (
	// inlineToken matches what running text holds besides words: an inline
	// code span, or an image or link with its text and target.
	inlineToken = regexp.MustCompile("`[^`]*`|(!?)\\[([^\\]]*)\\]\\(([^)\\s]+)\\)")
	// inlineCode is an inline code span.
	inlineCode = regexp.MustCompile("`[^`]*`")
	// blockStart begins a new paragraph-like block: a list item or a table row.
	blockStart = regexp.MustCompile(`^\s*([-*] |\d+\. |\|)`)
	// sentenceEnd ends a sentence: a full stop, question or exclamation mark,
	// possibly inside emphasis or quotes, followed by a space.
	sentenceEnd = regexp.MustCompile(`[.!?]["'”’*_)]* `)
	// abbreviation would otherwise end a sentence.
	abbreviation = regexp.MustCompile(`\b(e\.g|i\.e|etc)\.`)
)

// proseRun is a piece of running text; link is the target of the link the
// text belongs to, empty outside links.
type proseRun struct {
	text string
	link string
}

// prose returns the running text of a Markdown document, block by block:
// paragraphs, list items and table rows outside code blocks, without
// headings, inline code and images, with links reduced to their text.
func prose(doc string) [][]proseRun {
	var blocks [][]proseRun
	var block []proseRun
	flush := func() {
		if len(block) > 0 {
			blocks = append(blocks, block)
			block = nil
		}
	}
	inCode := false
	for _, line := range strings.Split(doc, "\n") {
		if fence.MatchString(line) {
			flush()
			inCode = !inCode
			continue
		}
		if inCode || heading.MatchString(line) || strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if blockStart.MatchString(line) {
			flush()
		}
		block = append(block, lineRuns(line)...)
		block = append(block, proseRun{text: " "})
	}
	flush()
	return blocks
}

// lineRuns splits one line of Markdown into runs of text.
func lineRuns(line string) []proseRun {
	var runs []proseRun
	rest := line
	for {
		m := inlineToken.FindStringSubmatchIndex(rest)
		if m == nil {
			break
		}
		runs = append(runs, proseRun{text: rest[:m[0]]})
		switch {
		case rest[m[0]] == '`':
			runs = append(runs, proseRun{text: " "})
		case m[3] > m[2]:
			// An image: its alt text is not running text.
		default:
			text := inlineCode.ReplaceAllString(rest[m[4]:m[5]], " ")
			runs = append(runs, proseRun{text: text, link: rest[m[6]:m[7]]})
		}
		rest = rest[m[1]:]
	}
	return append(runs, proseRun{text: rest})
}

// plain joins the text of runs.
func plain(runs []proseRun) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.text)
	}
	return b.String()
}

// sentences splits a block of running text into sentences.
func sentences(block []proseRun) []string {
	text := abbreviation.ReplaceAllString(plain(block), "$1")
	text = strings.Join(strings.Fields(text), " ") + " "
	var out []string
	start := 0
	for _, loc := range sentenceEnd.FindAllStringIndex(text, -1) {
		if s := strings.TrimSpace(text[start:loc[1]]); s != "" {
			out = append(out, s)
		}
		start = loc[1]
	}
	if s := strings.TrimSpace(text[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

// words returns the words of a sentence: its space-separated tokens that
// hold a letter or digit.
func words(sentence string) []string {
	var out []string
	for _, f := range strings.Fields(sentence) {
		if strings.IndexFunc(f, isWordRune) >= 0 {
			out = append(out, f)
		}
	}
	return out
}

func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// section returns the part of a Markdown document below the heading with
// the given text, up to the next heading of the same or a higher level; ok
// is false if there is no such heading.
func section(doc, title string) (body string, ok bool) {
	lines := strings.Split(doc, "\n")
	level := 0
	var out []string
	inCode := false
	for _, line := range lines {
		if fence.MatchString(line) {
			inCode = !inCode
		}
		m := heading.FindStringSubmatch(line)
		if inCode || m == nil {
			if level > 0 {
				out = append(out, line)
			}
			continue
		}
		depth := strings.Index(line, " ")
		switch {
		case level == 0 && m[1] == title:
			level = depth
		case level > 0 && depth <= level:
			return strings.Join(out, "\n"), true
		case level > 0:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n"), level > 0
}

// headings returns the texts of a document's headings of the given level,
// outside code blocks, in order.
func headings(doc string, level int) []string {
	prefix := strings.Repeat("#", level) + " "
	var out []string
	inCode := false
	for _, line := range strings.Split(doc, "\n") {
		if fence.MatchString(line) {
			inCode = !inCode
			continue
		}
		if !inCode && strings.HasPrefix(line, prefix) {
			out = append(out, strings.TrimPrefix(line, prefix))
		}
	}
	return out
}
