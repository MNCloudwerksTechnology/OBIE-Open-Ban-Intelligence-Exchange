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

// glossaryGuides are the pages newcomers and operators read. Each links a
// required term to the glossary where it first uses it. Reference material
// (configuration reference, performance report, specification, ADRs) is
// written for engineers and not checked.
var glossaryGuides = []string{
	"README.md",
	"documentation/introduction.md",
	"documentation/faq.md",
	"documentation/capabilities.md",
	"documentation/sandbox.md",
	"documentation/getting-started.md",
	"documentation/operations/setup.md",
	"documentation/operations/federation.md",
	"documentation/operations/install.md",
	"documentation/operations/operations.md",
	"documentation/operations/troubleshooting.md",
	"documentation/operations/monitoring.md",
	"documentation/operations/console.md",
	"documentation/operations/messages.md",
	"documentation/guides/fail2ban.md",
	"documentation/guides/nftables.md",
	"documentation/guides/README.md",
	"documentation/guides/connect-a-peer.md",
	"documentation/guides/stop-trusting-a-peer.md",
	"documentation/guides/review-what-would-be-blocked.md",
	"documentation/guides/why-is-an-address-blocked.md",
	"documentation/guides/unblock-an-address.md",
	"documentation/guides/block-an-address.md",
	"documentation/guides/withdraw-a-verdict.md",
	"packaging/compose/README.md",
}

// glossaryLink is the target of a link into the glossary, with the anchor
// of the entry.
var glossaryLink = regexp.MustCompile(`(^|/)glossary\.md#([a-z0-9-]+)$`)

// linksEntry reports whether a link target is the glossary entry of a term,
// or of a compound term that contains it: "peer" may link to "peer-id" or
// "bootstrap-peer". TestRelativeLinksResolve checks that the entry exists.
func linksEntry(target, term string) bool {
	m := glossaryLink.FindStringSubmatch(target)
	if m == nil {
		return false
	}
	anchor := strings.ReplaceAll(strings.ToLower(term), " ", "-")
	return strings.Contains("-"+m[2]+"-", "-"+anchor+"-")
}

// TestGuidesLinkGlossaryOnFirstUse checks that every guide links each
// required term to the glossary the first time its running text uses it.
func TestGuidesLinkGlossaryOnFirstUse(t *testing.T) {
	for _, guide := range glossaryGuides {
		blocks := prose(readRepoFile(t, guide))
		for _, term := range requiredTerms {
			run, ok := firstUse(blocks, term.use)
			if ok && !linksEntry(run.link, term.heading) {
				t.Errorf("%s: the first use of %q is not a link to its glossary entry: %q -> %q",
					guide, term.heading, strings.TrimSpace(run.text), run.link)
			}
		}
	}
}

// firstUse returns the first run of text that uses a term.
func firstUse(blocks [][]proseRun, use *regexp.Regexp) (proseRun, bool) {
	for _, block := range blocks {
		for _, run := range block {
			if use.MatchString(run.text) {
				return run, true
			}
		}
	}
	return proseRun{}, false
}
