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

// codeDocLink is a page of the documentation that a program names, with the
// heading it points to, if any.
var codeDocLink = regexp.MustCompile(`documentation/[A-Za-z0-9_./-]+\.md(#[A-Za-z0-9_-]+)?`)

// TestCodeLinksResolve checks the pages of the documentation that the
// programs name in their messages and next steps, as a path or in a URL:
// the page exists and, for a link to a heading, the heading does. A renamed
// heading would otherwise send the reader of a message nowhere.
func TestCodeLinksResolve(t *testing.T) {
	walkRepo(t, ".go", func(path string, data []byte) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		for _, link := range codeDocLink.FindAllString(string(data), -1) {
			file, frag, _ := strings.Cut(link, "#")
			doc, err := os.ReadFile(filepath.Join(repoRoot, file)) // #nosec G304 G703 -- a page of this repository, named in its code.
			if err != nil {
				t.Errorf("%s names %s: %v", path, link, err)
				continue
			}
			if frag != "" && !headingAnchors(string(doc))[frag] {
				t.Errorf("%s names %s, but %s has no heading #%s", path, link, file, frag)
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
