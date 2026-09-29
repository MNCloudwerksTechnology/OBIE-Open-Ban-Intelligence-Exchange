package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	// mdLink matches the target of an inline Markdown link or image. Links
	// with a title, reference-style links and <…> targets are not checked;
	// the documentation does not use them.
	mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// fence opens or closes a code block.
	fence = regexp.MustCompile("^\\s*```")
	// heading is an ATX heading.
	heading = regexp.MustCompile(`^#{1,6} (.+)$`)
	// slugDrop is what GitHub removes from a heading to form its anchor.
	slugDrop = regexp.MustCompile(`[^\p{L}\p{N}\- _]`)
)

// TestRelativeLinksResolve checks every relative link in the repository's
// Markdown files: the target file exists and, for links to a heading of a
// Markdown file, the heading does.
func TestRelativeLinksResolve(t *testing.T) {
	anchors := map[string]map[string]bool{}
	anchorsOf := func(path string) map[string]bool {
		if a, ok := anchors[path]; ok {
			return a
		}
		data, err := os.ReadFile(path) // #nosec G304 -- repository file.
		if err != nil {
			t.Fatal(err)
		}
		anchors[path] = headingAnchors(string(data))
		return anchors[path]
	}

	walkRepo(t, ".md", func(path string, data []byte) {
		for _, target := range relativeLinks(string(data)) {
			file, frag, _ := strings.Cut(target, "#")
			dest := path
			if file != "" {
				dest = filepath.Join(filepath.Dir(path), file)
			}
			info, err := os.Stat(dest)
			if err != nil {
				t.Errorf("%s: link %s: %v", path, target, err)
				continue
			}
			if frag == "" || info.IsDir() || !strings.HasSuffix(dest, ".md") {
				continue
			}
			if !anchorsOf(dest)[frag] {
				t.Errorf("%s: link %s: %s has no heading #%s", path, target, dest, frag)
			}
		}
	})
}

// relativeLinks returns the targets of the links outside code blocks that
// point into the repository.
func relativeLinks(doc string) []string {
	var links []string
	inCode := false
	for _, line := range strings.Split(doc, "\n") {
		if fence.MatchString(line) {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			links = append(links, target)
		}
	}
	return links
}

// headingAnchors returns the GitHub anchors of the document's headings.
func headingAnchors(doc string) map[string]bool {
	anchors := map[string]bool{}
	seen := map[string]int{}
	inCode := false
	for _, line := range strings.Split(doc, "\n") {
		if fence.MatchString(line) {
			inCode = !inCode
			continue
		}
		m := heading.FindStringSubmatch(line)
		if inCode || m == nil {
			continue
		}
		slug := anchor(m[1])
		if n := seen[slug]; n > 0 {
			anchors[slug+"-"+strconv.Itoa(n)] = true
		} else {
			anchors[slug] = true
		}
		seen[slug]++
	}
	return anchors
}

// anchor returns the GitHub anchor of a heading, before any suffix that
// tells equal headings apart.
func anchor(heading string) string {
	return strings.ReplaceAll(slugDrop.ReplaceAllString(strings.ToLower(heading), ""), " ", "-")
}
