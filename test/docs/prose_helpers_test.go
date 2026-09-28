package docs

import (
	"slices"
	"strings"
	"testing"
)

// TestSentences checks how the documentation tests split running text into
// sentences: numbers, file names, inline code, link targets and common
// abbreviations do not end a sentence.
func TestSentences(t *testing.T) {
	tests := []struct {
		markdown string
		want     []string
	}{
		{"One. Two!", []string{"One.", "Two!"}},
		{"A threshold of 1.8 (`decision.threshold`) is the default.", []string{"A threshold of 1.8 ( ) is the default."}},
		{"See [the guide](guides/fail2ban.md). Then go on.", []string{"See the guide.", "Then go on."}},
		{"**Never blocked.** Your node, e.g. this one, is safe.", []string{"**Never blocked.**", "Your node, e.g this one, is safe."}},
		{"Is it free? \"Yes.\" No list", []string{"Is it free?", "\"Yes.\"", "No list"}},
	}
	for _, tt := range tests {
		blocks := prose(tt.markdown)
		if len(blocks) != 1 {
			t.Fatalf("prose(%q) has %d blocks, want 1", tt.markdown, len(blocks))
		}
		if got := sentences(blocks[0]); !slices.Equal(got, tt.want) {
			t.Errorf("sentences(%q) = %q, want %q", tt.markdown, got, tt.want)
		}
	}
}

// TestProse checks which parts of a Markdown document count as running
// text, that a paragraph's lines are joined, and that link text keeps its
// target, also across a line break.
func TestProse(t *testing.T) {
	doc := "# Title\n\nA [node](glossary.md#node) and ![alt text](x.svg) `code` in\nenforce mode.\n\n```sh\nnode\n```\n\n- item one\n- item [two\n  lines](x.md)\n"
	blocks := prose(doc)
	var texts []string
	var links []proseRun
	for _, b := range blocks {
		texts = append(texts, strings.Join(strings.Fields(plain(b)), " "))
		for _, r := range b {
			if r.link != "" {
				links = append(links, r)
			}
		}
	}
	want := []string{"A node and in enforce mode.", "- item one", "- item two lines"}
	if !slices.Equal(texts, want) {
		t.Errorf("prose texts = %q, want %q", texts, want)
	}
	want2 := []proseRun{{text: "node", link: "glossary.md#node"}, {text: "two lines", link: "x.md"}}
	if !slices.Equal(links, want2) {
		t.Errorf("links = %+v, want %+v", links, want2)
	}
}
