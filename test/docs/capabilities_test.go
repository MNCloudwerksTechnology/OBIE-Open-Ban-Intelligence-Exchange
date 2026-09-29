package docs

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

const capabilitiesPath = "documentation/capabilities.md"

// capabilitySections are the sections of the capability overview, in the
// order an evaluator reads them (WP-1692). What the release cannot do
// follows what it can do directly, at the same level.
var capabilitySections = []string{
	"What it can do",
	"What it cannot do yet",
}

// Status labels of what the release can do.
const (
	statusSupported    = "Supported"
	statusOffByDefault = "Off by default"
	statusObserveOnly  = "Observe only"
)

// capability is an outcome the overview must list (WP-1692), by the bold
// lead of its row. off reports whether a configuration leaves it switched
// off; nil if nothing does.
type capability struct {
	lead string
	off  func(config.Config) bool
}

var capabilities = []capability{
	{"Share what Fail2Ban catches.", nil},
	{"Exchange warnings only with peers you choose.", nil},
	{"Block only when enough trusted peers agree.", nil},
	{"Keep the last word.", nil},
	{"Watch before you block.", func(c config.Config) bool { return c.Node.Mode != config.ModeObserve }},
	{"Block attackers in your firewall.", func(c config.Config) bool {
		return c.Node.Mode != config.ModeEnforce || c.Enforce.Backend != config.BackendNFTables
	}},
	{"See how the node is doing.", func(c config.Config) bool { return c.Metrics.Listen == "" }},
	{"Keep an audit trail.", func(c config.Config) bool { return c.Audit.Path == "" }},
	{"Look into the node in a browser.", func(c config.Config) bool { return !c.Console.Enabled }},
}

// missingFeatures are what the release cannot do yet and the overview must
// say so (WP-1692), by the bold lead of their row.
var missingFeatures = []string{
	"Automatic peer discovery.",
	"Trust that adapts over time.",
	"Appeals.",
	"Other detection tools, ready-made.",
	"Other firewalls.",
	"Hosts other than Linux.",
}

// Plan labels of what the release cannot do yet: "Planned" names where it
// is planned, with a link.
const (
	planPlanned = "Planned"
	planNone    = "No plan yet"
)

var (
	// separatorRow is the row below a table's header.
	separatorRow = regexp.MustCompile(`^\|[\s:|-]+\|$`)
	// boldLead is the bold text a table cell starts with.
	boldLead = regexp.MustCompile(`^\*\*([^*]+)\*\*`)
	// linkText is a link, whose text alone counts in a bold lead.
	linkText = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// TestCapabilitiesSections checks the overview's title and that its
// sections come in the order an evaluator reads them.
func TestCapabilitiesSections(t *testing.T) {
	doc := readRepoFile(t, capabilitiesPath)
	if got := headings(doc, 1); !slices.Equal(got, []string{"What OBIE can and cannot do yet"}) {
		t.Errorf("title = %q, want \"What OBIE can and cannot do yet\"", got)
	}
	if got := headings(doc, 2); !slices.Equal(got, capabilitySections) {
		t.Errorf("sections = %q, want %q", got, capabilitySections)
	}
}

// TestCapabilitiesLinkTheirGuides checks that the overview lists every
// required outcome, each with a status and a link to the guide that shows
// how, and that the status of an outcome a default configuration leaves
// off says so instead of "Supported".
func TestCapabilitiesLinkTheirGuides(t *testing.T) {
	doc := readRepoFile(t, capabilitiesPath)
	section, _ := section(doc, "What it can do")
	rows := rowsByLead(t, section, 3)
	defaults := config.Default()
	for _, c := range capabilities {
		row, ok := rows[c.lead]
		if !ok {
			t.Errorf("\"What it can do\" has no row %q", c.lead)
			continue
		}
		want := statusSupported
		if c.off != nil && c.off(defaults) {
			want = statusOffByDefault
		}
		if row[1] != want {
			t.Errorf("%q: status %q, want %q", c.lead, row[1], want)
		}
	}
	for lead, row := range rows {
		if !slices.Contains([]string{statusSupported, statusOffByDefault, statusObserveOnly}, row[1]) {
			t.Errorf("%q: status %q, want one of %q, %q or %q", lead, row[1], statusSupported, statusOffByDefault, statusObserveOnly)
		}
		if !slices.ContainsFunc(relativeLinks(row[2]), isGuideLink) {
			t.Errorf("%q links no guide: %q", lead, row[2])
		}
	}
}

// TestNotYetIsAsProminent checks that what the release cannot do yet is
// stated like what it can do: a table of the same shape, every required
// gap listed, each saying whether it is planned and, if so, where.
func TestNotYetIsAsProminent(t *testing.T) {
	doc := readRepoFile(t, capabilitiesPath)
	can, _ := section(doc, "What it can do")
	cannot, _ := section(doc, "What it cannot do yet")
	canRows, cannotRows := rowsByLead(t, can, 3), rowsByLead(t, cannot, 3)
	if len(cannotRows) < len(missingFeatures) {
		t.Errorf("\"What it cannot do yet\" lists %d gaps, want at least %d", len(cannotRows), len(missingFeatures))
	}
	if len(canRows) == 0 {
		t.Error("\"What it can do\" lists nothing")
	}
	for _, lead := range missingFeatures {
		if _, ok := cannotRows[lead]; !ok {
			t.Errorf("\"What it cannot do yet\" has no row %q", lead)
		}
	}
	for lead, row := range cannotRows {
		switch {
		case row[1] == planNone:
		case strings.HasPrefix(row[1], planPlanned) && len(relativeLinks(row[1])) > 0:
		default:
			t.Errorf("%q: %q, want %q or %q with a link to where it is planned", lead, row[1], planNone, planPlanned)
		}
		if strings.TrimSpace(row[2]) == "" {
			t.Errorf("%q does not say what to do until then", lead)
		}
	}
}

// rowsByLead returns the body rows of the tables in doc by the bold lead
// of their first cell, failing t for a row without one or with another
// number of cells than want.
func rowsByLead(t *testing.T, doc string, want int) map[string][]string {
	t.Helper()
	rows := map[string][]string{}
	for _, row := range tableRows(doc) {
		m := boldLead.FindStringSubmatch(row[0])
		switch {
		case len(row) != want:
			t.Errorf("row %q has %d cells, want %d", row, len(row), want)
		case m == nil:
			t.Errorf("row %q does not start with a bold lead", row)
		default:
			rows[linkText.ReplaceAllString(m[1], "$1")] = row
		}
	}
	return rows
}

// tableRows returns the cells of the body rows of the Markdown tables in
// doc, without their header and separator rows.
func tableRows(doc string) [][]string {
	var rows [][]string
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || separatorRow.MatchString(line) {
			continue
		}
		if i+1 < len(lines) && separatorRow.MatchString(strings.TrimSpace(lines[i+1])) {
			continue // a header
		}
		var cells []string
		for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
			cells = append(cells, strings.TrimSpace(cell))
		}
		rows = append(rows, cells)
	}
	return rows
}

// isGuideLink reports whether a relative link leads to a page that shows
// how, rather than to a glossary entry.
func isGuideLink(target string) bool {
	file, _, _ := strings.Cut(target, "#")
	return strings.HasSuffix(file, ".md") && !strings.HasSuffix(file, "glossary.md")
}
