package docs

import (
	"maps"
	"net"
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
	"What it needs",
	"Remaining risks",
	"Is OBIE for me?",
	"How this page is kept current",
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

// requirementTopics are the subsections of "What it needs" (WP-1692).
var requirementTopics = []string{
	"Operating system and processor",
	"Privileges",
	"Network",
	"Resources",
	"Fail2Ban",
}

// measurements are the records the requirements are measured in, by the
// subsection that cites them.
var measurements = map[string]string{
	"Resources": "operations/performance.md#resource-usage-of-one-node",
	"Fail2Ban":  "operations/performance.md#fail2ban-versions",
}

var (
	// describedRelease is how the overview names the release it describes,
	// before its first section.
	describedRelease = regexp.MustCompile(`\*\*OBIE (\d+\.\d+\.\d+)\*\*`)
	// namedRelease is any mention of a release on the overview.
	namedRelease = regexp.MustCompile(`OBIE (\d+\.\d+\.\d+)`)
	// installedRelease is the release the README's install commands
	// download.
	installedRelease = regexp.MustCompile(`/releases/download/v(\d+\.\d+\.\d+)/`)
	// releasePlatforms is the default of PLATFORMS in packaging/release.sh:
	// the platforms a release is built for.
	releasePlatforms = regexp.MustCompile(`platforms=\$\{PLATFORMS:-([^}]*)\}`)
	// multiaddrPort is the TCP or UDP port of a multiaddr.
	multiaddrPort = regexp.MustCompile(`/(?:tcp|udp)/(\d+)`)
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

// TestRequirementsAreComplete checks "What it needs": a subsection per
// requirement, every processor architecture a release is built for, every
// port a node listens on by default, and a link to the measurement behind
// the resource and Fail2Ban requirements.
func TestRequirementsAreComplete(t *testing.T) {
	doc := readRepoFile(t, capabilitiesPath)
	needs, ok := section(doc, "What it needs")
	if !ok {
		t.Fatal("the overview has no section \"What it needs\"")
	}
	if got := headings(needs, 3); !slices.Equal(got, requirementTopics) {
		t.Errorf("\"What it needs\" covers %q, want %q", got, requirementTopics)
	}
	system, _ := section(needs, "Operating system and processor")
	m := releasePlatforms.FindStringSubmatch(readRepoFile(t, "packaging/release.sh"))
	if m == nil {
		t.Fatal("packaging/release.sh has no default PLATFORMS")
	}
	for _, platform := range strings.Fields(m[1]) {
		_, arch, _ := strings.Cut(platform, "/")
		if !strings.Contains(system, arch) {
			t.Errorf("\"Operating system and processor\" does not name %s, which releases are built for", arch)
		}
	}
	network, _ := section(needs, "Network")
	for _, port := range defaultPorts(t) {
		if !regexp.MustCompile(`\b` + port + `\b`).MatchString(network) {
			t.Errorf("\"Network\" does not name port %s, which a node listens on by default", port)
		}
	}
	for topic, target := range measurements {
		body, _ := section(needs, topic)
		if !slices.Contains(relativeLinks(body), target) {
			t.Errorf("%q does not link its measurement %s", topic, target)
		}
	}
}

// defaultPorts returns the ports a node listens on with the default
// configuration: the mesh, the metrics and the web console.
func defaultPorts(t *testing.T) []string {
	t.Helper()
	d := config.Default()
	var ports []string
	for _, addr := range d.Mesh.Listen {
		for _, m := range multiaddrPort.FindAllStringSubmatch(addr, -1) {
			ports = append(ports, m[1])
		}
	}
	for _, addr := range []string{d.Metrics.Listen, d.Console.Listen} {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatal(err)
		}
		ports = append(ports, port)
	}
	slices.Sort(ports)
	return slices.Compact(ports)
}

// threatModel is the full threat model the overview's risks summarize.
const threatModel = "../SECURITY.md#threat-model"

// threatContext is the threat model's introduction, not a threat.
const threatContext = "What OBIE protects and whom it trusts"

// maxRiskWords keeps each risk of the summary short.
const maxRiskWords = 70

// Answers of "Is OBIE for me?".
var answers = []string{"**Yes**", "**No**", "**Not yet**"}

// scenarioKinds are situations the scenarios must cover (WP-1692), by a
// word of their lead.
var scenarioKinds = []string{"VPS", "hosting provider", "homelab"}

// TestRisksSummarizeEveryThreat checks that "Remaining risks" links the
// full threat model and summarizes each of its threats in a short item
// that links the threat's section, so a new threat cannot be left out.
func TestRisksSummarizeEveryThreat(t *testing.T) {
	doc := readRepoFile(t, capabilitiesPath)
	risks, ok := section(doc, "Remaining risks")
	if !ok {
		t.Fatal("the overview has no section \"Remaining risks\"")
	}
	links := relativeLinks(risks)
	if !slices.Contains(links, threatModel) {
		t.Errorf("\"Remaining risks\" does not link the threat model %s", threatModel)
	}
	model, ok := section(readRepoFile(t, "SECURITY.md"), "Threat model")
	if !ok {
		t.Fatal("SECURITY.md has no section \"Threat model\"")
	}
	for _, threat := range headings(model, 3) {
		if threat == threatContext {
			continue
		}
		target := "../SECURITY.md#" + anchor(threat)
		if !slices.Contains(links, target) {
			t.Errorf("\"Remaining risks\" does not summarize %q (no link to %s)", threat, target)
		}
	}
	for _, block := range prose(risks) {
		if n := len(words(plain(block))); n > maxRiskWords {
			t.Errorf("a risk takes %d words, want at most %d: %q", n, maxRiskWords, strings.Join(strings.Fields(plain(block)), " "))
		}
	}
}

// TestScenariosHaveClearAnswers checks "Is OBIE for me?": 5 to 8
// situations, among them a single VPS, a small hosting provider and a
// homelab, each answered with a plain yes, no or not yet and a reason.
func TestScenariosHaveClearAnswers(t *testing.T) {
	doc := readRepoFile(t, capabilitiesPath)
	body, ok := section(doc, "Is OBIE for me?")
	if !ok {
		t.Fatal("the overview has no section \"Is OBIE for me?\"")
	}
	rows := rowsByLead(t, body, 3)
	if len(rows) < 5 || len(rows) > 8 {
		t.Errorf("\"Is OBIE for me?\" has %d situations, want 5 to 8", len(rows))
	}
	for _, kind := range scenarioKinds {
		if !slices.ContainsFunc(slices.Collect(maps.Keys(rows)), func(lead string) bool { return strings.Contains(lead, kind) }) {
			t.Errorf("no situation mentions %q", kind)
		}
	}
	for lead, row := range rows {
		if !slices.Contains(answers, row[1]) {
			t.Errorf("%q: answer %q, want one of %q", lead, row[1], answers)
		}
		if row[2] == "" {
			t.Errorf("%q gives no reason", lead)
		}
	}
}

// releasing is the part of CONTRIBUTING.md that says how a release updates
// the overview.
const releasing = "../CONTRIBUTING.md#releasing"

// TestCapabilitiesDescribeCurrentRelease checks that the overview names
// the release it describes before its first section, that this is the
// release the README installs, that it names no other release, and that
// it says how it is kept current with every release (make release refuses
// a version it does not name: TestReleaseRefusesStaleCapabilities in
// packaging).
func TestCapabilitiesDescribeCurrentRelease(t *testing.T) {
	doc := readRepoFile(t, capabilitiesPath)
	top, _, _ := strings.Cut(doc, "\n## ")
	m := describedRelease.FindStringSubmatch(top)
	if m == nil {
		t.Fatal("the overview does not name the release it describes, as **OBIE x.y.z**, before its first section")
	}
	release := m[1]
	installs := installedRelease.FindAllStringSubmatch(readRepoFile(t, "README.md"), -1)
	if len(installs) == 0 {
		t.Fatal("the README installs no release")
	}
	for _, in := range installs {
		if in[1] != release {
			t.Errorf("the README installs %s, but the overview describes OBIE %s", in[1], release)
		}
	}
	for _, n := range namedRelease.FindAllStringSubmatch(doc, -1) {
		if n[1] != release {
			t.Errorf("the overview describes OBIE %s, but also names OBIE %s", release, n[1])
		}
	}
	kept, _ := section(doc, "How this page is kept current")
	if !slices.Contains(relativeLinks(kept), releasing) {
		t.Errorf("\"How this page is kept current\" does not link %s", releasing)
	}
}

// TestCapabilitiesArePlainLanguage keeps the overview readable for
// evaluators who are not engineers: short sentences, as in the
// introduction.
func TestCapabilitiesArePlainLanguage(t *testing.T) {
	doc := readRepoFile(t, capabilitiesPath)
	for _, block := range prose(doc) {
		for _, s := range sentences(block) {
			if n := len(words(s)); n > maxSentenceWords {
				t.Errorf("sentence of %d words, want at most %d: %q", n, maxSentenceWords, s)
			}
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
