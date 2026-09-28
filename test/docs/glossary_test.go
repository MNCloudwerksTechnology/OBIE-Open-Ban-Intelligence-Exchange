package docs

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

const glossaryPath = "documentation/glossary.md"

// glossaryTerm is a term a newcomer meets: the heading of its glossary entry
// and the words that use it in running text.
type glossaryTerm struct {
	heading string
	use     *regexp.Regexp
}

// requiredTerms must each have a glossary entry, and the guides link them
// to the glossary where they first use them (WP-1691).
var requiredTerms = []glossaryTerm{
	{"Allow-list", regexp.MustCompile(`(?i)\ballow-?lists?\b`)},
	{"Enforce mode", regexp.MustCompile(`(?i)\benforce mode\b`)},
	{"Indicator", regexp.MustCompile(`(?i)\bindicators?\b`)},
	{"Node", regexp.MustCompile(`(?i)\bnodes?\b`)},
	{"Observe mode", regexp.MustCompile(`(?i)\bobserve(-only)? mode\b`)},
	{"Override", regexp.MustCompile(`(?i)\boverrides?\b`)},
	{"Peer", regexp.MustCompile(`(?i)\bpeers?\b`)},
	{"Quorum", regexp.MustCompile(`(?i)\bquorums?\b`)},
	{"Revocation", regexp.MustCompile(`(?i)\b(revocations?|revok(e|es|ed|ing))\b`)},
	{"Sovereignty", regexp.MustCompile(`(?i)\bsovereignty\b`)},
	{"Threshold", regexp.MustCompile(`(?i)\bthresholds?\b`)},
	{"Trust weight", regexp.MustCompile(`(?i)\btrust weights?\b`)},
	{"Verdict", regexp.MustCompile(`(?i)\bverdicts?\b`)},
}

// TestGlossaryDefinesEveryTerm checks the glossary: every required term has
// an entry, the entries are in alphabetical order, and each defines its
// term in one or two sentences.
func TestGlossaryDefinesEveryTerm(t *testing.T) {
	doc := readRepoFile(t, glossaryPath)
	terms := headings(doc, 2)
	for _, term := range requiredTerms {
		if !slices.Contains(terms, term.heading) {
			t.Errorf("the glossary has no entry %q", term.heading)
		}
	}
	if !slices.IsSortedFunc(terms, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	}) {
		t.Errorf("the glossary entries are not in alphabetical order: %q", terms)
	}
	for _, term := range terms {
		body, _ := section(doc, term)
		var n int
		for _, block := range prose(body) {
			n += len(sentences(block))
		}
		if n < 1 || n > 2 {
			t.Errorf("glossary entry %q has %d sentences, want one or two", term, n)
		}
	}
}
