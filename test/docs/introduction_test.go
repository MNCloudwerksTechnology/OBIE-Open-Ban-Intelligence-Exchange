package docs

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

const introductionPath = "documentation/introduction.md"

// introductionQuestions are the introduction's first sections, in the order
// a newcomer asks them (WP-1691): the problem, the idea, what happens on
// their host, and what can never happen.
var introductionQuestions = []string{
	"The problem: every server fights alone",
	"The idea: servers warn each other",
	"What happens on your server",
	"What can never happen",
}

// guarantees are what the section "What can never happen" promises.
var guarantees = []string{
	"**Your server's own addresses and the protected addresses are never blocked.**",
	"**Nothing is enforced until you switch it on.**",
	"**Raw logs never leave your server.**",
}

// jargon is protocol and tooling vocabulary the introduction does without,
// so that readers without a networking background can follow it.
var jargon = regexp.MustCompile(`(?i)\b(libp2p|gossipsub|ed25519|multiaddr|cidr|json|yaml|uuid|sighup|badgerdb|netlink|nftables|prometheus|ipv[46]|tcp|udp|quic|api|daemon|obied|obiectl|hash(es|ed)?|ssh|config|consensus)\b`)

const (
	// maxSentenceWords and maxAverageSentenceWords keep the sentences short
	// enough for plain language.
	maxSentenceWords        = 30
	maxAverageSentenceWords = 18
	// maxIntroductionWords is about five minutes of reading at 240 words a
	// minute.
	maxIntroductionWords = 1200
)

// TestIntroductionAnswersInOrder checks that "What is OBIE?" answers the
// newcomer's questions in order, makes its three promises and points on to
// the FAQ and the glossary.
func TestIntroductionAnswersInOrder(t *testing.T) {
	doc := readRepoFile(t, introductionPath)
	if got := headings(doc, 1); !slices.Equal(got, []string{"What is OBIE?"}) {
		t.Errorf("title = %q, want \"What is OBIE?\"", got)
	}
	got := headings(doc, 2)
	if len(got) < len(introductionQuestions) || !slices.Equal(got[:len(introductionQuestions)], introductionQuestions) {
		t.Errorf("the introduction's sections start with %q, want %q", got, introductionQuestions)
	}
	never, _ := section(doc, "What can never happen")
	never = strings.Join(strings.Fields(never), " ")
	for _, g := range guarantees {
		if !strings.Contains(never, g) {
			t.Errorf("\"What can never happen\" does not promise %s", g)
		}
	}
	links := relativeLinks(doc)
	for _, page := range []string{"faq.md", "glossary.md"} {
		if !slices.ContainsFunc(links, func(l string) bool { return strings.HasPrefix(l, page) }) {
			t.Errorf("the introduction does not link %s", page)
		}
	}
}

// TestIntroductionIsPlainLanguage keeps the introduction readable for
// decision makers without a networking background: no commands or
// configuration, no protocol jargon, short sentences, about five minutes
// of reading.
func TestIntroductionIsPlainLanguage(t *testing.T) {
	doc := readRepoFile(t, introductionPath)
	for _, line := range strings.Split(doc, "\n") {
		if fence.MatchString(line) || inlineCode.MatchString(line) {
			t.Errorf("the introduction shows code: %q", line)
		}
	}
	total, count := 0, 0
	for _, block := range prose(doc) {
		for _, s := range sentences(block) {
			n := len(words(s))
			total += n
			count++
			if n > maxSentenceWords {
				t.Errorf("sentence of %d words, want at most %d: %q", n, maxSentenceWords, s)
			}
			if m := jargon.FindString(s); m != "" {
				t.Errorf("jargon %q in: %q", m, s)
			}
		}
	}
	if count == 0 {
		t.Fatal("the introduction has no running text")
	}
	avg := float64(total) / float64(count)
	t.Logf("%d words in %d sentences, %.1f words a sentence", total, count, avg)
	if avg > maxAverageSentenceWords {
		t.Errorf("sentences have %.1f words on average, want at most %d", avg, maxAverageSentenceWords)
	}
	if total > maxIntroductionWords {
		t.Errorf("the introduction has %d words, want at most %d (about five minutes)", total, maxIntroductionWords)
	}
}

// introductionLink is how README.md links the introduction.
const introductionLink = "documentation/introduction.md"

// TestReadmeOpensWithIntroduction checks that the repository's front page
// sends newcomers to the introduction first: with its first link, before
// any section, and in the first row of its documentation table.
func TestReadmeOpensWithIntroduction(t *testing.T) {
	doc := readRepoFile(t, "README.md")
	top, _, _ := strings.Cut(doc, "\n## ")
	if links := relativeLinks(top); len(links) == 0 || links[0] != introductionLink {
		t.Errorf("the README's first link, before its first section, is not %s", introductionLink)
	}
	table, ok := section(doc, "Documentation")
	if !ok {
		t.Fatal("the README has no section \"Documentation\"")
	}
	var rows []string
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "|") && !strings.HasPrefix(line, "|-") {
			rows = append(rows, line)
		}
	}
	if len(rows) < 2 || !strings.Contains(rows[1], "]("+introductionLink+")") {
		t.Errorf("the first row of the README's documentation table does not link %s", introductionLink)
	}
}
