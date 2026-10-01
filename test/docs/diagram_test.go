package docs

import (
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	attackDiagram        = "images/one-attack.svg"
	attackDiagramSection = "One attack, start to finish"
	// minAltWords keeps the image's alt text a description, not a label.
	minAltWords = 12
)

// attackStages are the steps one attack takes through OBIE, in order
// (WP-1691).
var attackStages = []string{"Detection", "Signed verdict", "Peers", "Trust-weighted decision", "Firewall"}

var (
	// mdImage is a Markdown image with its alt text and source.
	mdImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)
	// stageItem is an item of the text alternative: "1. **Detection.** …".
	stageItem = regexp.MustCompile(`(?m)^\d+\. \*\*([^*]+)\.\*\*`)
)

// TestAttackDiagramHasTextAlternative checks the introduction's diagram: it
// is its only image, has a descriptive alt text and a text alternative that
// walks through the stages in order, and the SVG itself carries a title, a
// description and the stages in the same order.
func TestAttackDiagramHasTextAlternative(t *testing.T) {
	doc := readRepoFile(t, introductionPath)
	images := mdImage.FindAllStringSubmatch(doc, -1)
	if len(images) != 1 || images[0][2] != attackDiagram {
		t.Fatalf("the introduction's images are %q, want only %s", images, attackDiagram)
	}
	if alt := images[0][1]; len(words(alt)) < minAltWords {
		t.Errorf("alt text %q has fewer than %d words", alt, minAltWords)
	}

	body, ok := section(doc, attackDiagramSection)
	if !ok || !strings.Contains(body, attackDiagram) {
		t.Fatalf("the diagram is not in the section %q", attackDiagramSection)
	}
	var steps []string
	for _, m := range stageItem.FindAllStringSubmatch(body, -1) {
		steps = append(steps, m[1])
	}
	if !slices.Equal(steps, attackStages) {
		t.Errorf("the text alternative walks through %q, want %q", steps, attackStages)
	}

	svg := parseSVG(t, readRepoFile(t, "documentation/"+attackDiagram))
	if svg.role != "img" || svg.labelledBy != "title desc" {
		t.Errorf("svg role=%q aria-labelledby=%q, want img and \"title desc\"", svg.role, svg.labelledBy)
	}
	if svg.title == "" || len(words(svg.desc)) < minAltWords {
		t.Errorf("svg title %q and desc %q must describe the diagram", svg.title, svg.desc)
	}
	rest := svg.text
	for _, stage := range attackStages {
		i := strings.Index(rest, stage)
		if i < 0 {
			t.Errorf("the diagram does not show %q after the stages before it", stage)
			continue
		}
		rest = rest[i+len(stage):]
	}
}

// svgDoc is what the test reads from an SVG: the root's accessibility
// attributes, its title and description, and all visible text in document
// order.
type svgDoc struct {
	role, labelledBy string
	title, desc      string
	text             string
}

func parseSVG(t *testing.T, data string) svgDoc {
	t.Helper()
	var doc svgDoc
	var visible []string
	var stack []string
	dec := xml.NewDecoder(strings.NewReader(data))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("the diagram is not well-formed XML: %v", err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				for _, a := range tok.Attr {
					switch a.Name.Local {
					case "role":
						doc.role = a.Value
					case "aria-labelledby":
						doc.labelledBy = a.Value
					}
				}
			}
			stack = append(stack, tok.Name.Local)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			text := strings.Join(strings.Fields(string(tok)), " ")
			if text == "" || len(stack) == 0 {
				continue
			}
			switch stack[len(stack)-1] {
			case "title":
				doc.title += text
			case "desc":
				doc.desc += text
			case "text", "tspan":
				visible = append(visible, text)
			}
		}
	}
	doc.text = strings.Join(visible, " ")
	return doc
}
