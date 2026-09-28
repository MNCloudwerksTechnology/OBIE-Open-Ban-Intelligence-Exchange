package docs

import (
	"slices"
	"testing"
)

const faqPath = "documentation/faq.md"

// adoptionFears are the questions that stop people from adopting OBIE; the
// FAQ answers each of them (WP-1691).
var adoptionFears = []string{
	"Can a peer lock me out?",
	"What does OBIE share about me?",
	"What if a peer is malicious?",
	"What happens if OBIE crashes?",
	"How is this different from blocklists or CrowdSec?",
}

// maxLeadWords keeps the first sentence of an answer a short, direct answer.
const maxLeadWords = 25

// TestFAQAnswersAdoptionFears checks that the FAQ answers every adoption
// fear, each under its own heading and starting with a short, direct answer.
func TestFAQAnswersAdoptionFears(t *testing.T) {
	doc := readRepoFile(t, faqPath)
	questions := headings(doc, 2)
	for _, q := range adoptionFears {
		if !slices.Contains(questions, q) {
			t.Errorf("the FAQ does not answer %q", q)
			continue
		}
		answer, _ := section(doc, q)
		blocks := prose(answer)
		if len(blocks) == 0 {
			t.Errorf("%q has no answer", q)
			continue
		}
		lead := sentences(blocks[0])
		if len(lead) == 0 {
			t.Errorf("%q does not start with a sentence", q)
			continue
		}
		if len(words(lead[0])) > maxLeadWords {
			t.Errorf("%q: the answer starts with %d words, want a direct answer of at most %d: %q",
				q, len(words(lead[0])), maxLeadWords, lead[0])
		}
	}
}
