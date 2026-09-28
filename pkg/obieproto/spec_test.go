package obieproto

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// specFile is the obie/0.1 protocol specification.
const specFile = "../../documentation/spec/obie-0.1.md"

// repoRoot is searched for the test functions the specification names.
const repoRoot = "../.."

var (
	// absoluteKeyword matches the RFC 2119 keywords of absolute
	// requirements; MUST NOT and SHALL NOT contain them.
	absoluteKeyword = regexp.MustCompile(`\b(MUST|SHALL|REQUIRED)\b`)
	// quotedKeyword matches keywords quoted in the conventions section.
	quotedKeyword = regexp.MustCompile(`"[A-Z ]+"`)
	// requirementDef is a requirement tag where it is defined, e.g. **[ENC-1]**.
	requirementDef = regexp.MustCompile(`\*\*\[([A-Z]+-[0-9]+)\]\*\*`)
	// requirementRef is any mention of a requirement tag, e.g. [ENC-1].
	requirementRef = regexp.MustCompile(`\[([A-Z]+-[0-9]+)\]`)
	// unitStart begins a new unit inside a paragraph: a list item, a table
	// row or a heading.
	unitStart = regexp.MustCompile(`^\s*([-*]|[0-9]+\.|\||#)\s?`)
	// appendixRow is a row of the requirement table in appendix A.
	appendixRow = regexp.MustCompile(`^\|\s*([A-Z]+-[0-9]+)\s*\|(.*)\|\s*$`)
	// testName is a test function named in appendix A.
	testName    = regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
	testFuncDef = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(t \*testing\.T\)`)
)

// specUnits splits the normative part of the specification (everything
// before appendix A) into paragraphs, list items, table rows and headings,
// skipping code blocks. It also returns the lines of appendix A.
func specUnits(t *testing.T) (units, appendix []string) {
	t.Helper()
	data, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			units = append(units, strings.Join(cur, " "))
			cur = nil
		}
	}
	inCode, inAppendix := false, false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "## Appendix A"):
			flush()
			inAppendix = true
		case strings.HasPrefix(line, "## Appendix"):
			inAppendix = false
		}
		if inAppendix {
			appendix = append(appendix, line)
			continue
		}
		if strings.HasPrefix(line, "```") {
			flush()
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		if strings.TrimSpace(line) == "" || unitStart.MatchString(line) {
			flush()
		}
		if strings.TrimSpace(line) != "" {
			cur = append(cur, line)
		}
	}
	flush()
	return units, appendix
}

// repoTests returns the names of all test functions in the repository.
func repoTests(t *testing.T) map[string]bool {
	t.Helper()
	tests := map[string]bool{}
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "bin") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G122 G304 -- walking the repository's own sources
		if err != nil {
			return err
		}
		for _, m := range testFuncDef.FindAllStringSubmatch(string(data), -1) {
			tests[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tests
}

// TestSpecRequirementsAreTested enforces the traceability of ADR 0006: every
// normative statement of the specification is tagged, every tag is defined
// once and listed in appendix A, and every test named there exists.
func TestSpecRequirementsAreTested(t *testing.T) {
	units, appendix := specUnits(t)

	defined := map[string]bool{}
	var referenced []string
	for _, u := range units {
		defs := requirementDef.FindAllStringSubmatch(u, -1)
		for _, d := range defs {
			if defined[d[1]] {
				t.Errorf("requirement %s is defined twice", d[1])
			}
			defined[d[1]] = true
		}
		for _, r := range requirementRef.FindAllStringSubmatch(u, -1) {
			referenced = append(referenced, r[1])
		}
		if absoluteKeyword.MatchString(quotedKeyword.ReplaceAllString(u, "")) && len(defs) == 0 {
			t.Errorf("normative statement without a requirement tag: %.120s", u)
		}
	}
	if len(defined) == 0 {
		t.Fatal("the specification defines no requirements")
	}
	for _, r := range referenced {
		if !defined[r] {
			t.Errorf("reference to undefined requirement %s", r)
		}
	}

	tests := repoTests(t)
	mapped := map[string]bool{}
	for _, line := range appendix {
		m := appendixRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		req := m[1]
		if mapped[req] {
			t.Errorf("appendix A lists %s twice", req)
		}
		mapped[req] = true
		if !defined[req] {
			t.Errorf("appendix A lists %s, which the specification does not define", req)
		}
		names := testName.FindAllStringSubmatch(m[2], -1)
		if len(names) == 0 {
			t.Errorf("appendix A names no test for %s", req)
		}
		for _, n := range names {
			if !tests[n[1]] {
				t.Errorf("appendix A names %s for %s, but no such test exists", n[1], req)
			}
		}
	}
	for req := range defined {
		if !mapped[req] {
			t.Errorf("requirement %s is missing from appendix A", req)
		}
	}
}

// TestReadmeLinksSpec checks that the whitepaper's technical schema section
// points to the specification and the schema.
func TestReadmeLinksSpec(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(data), "##### 3.2.1 Technical Schema")
	section, _, _ = strings.Cut(section, "\n#### ")
	if !found {
		t.Fatal("README has no technical schema section")
	}
	for _, link := range []string{"(documentation/spec/obie-0.1.md)", "(documentation/spec/obie-0.1.schema.json)"} {
		if !strings.Contains(section, link) {
			t.Errorf("technical schema section does not link %s", link)
		}
	}
}

// TestSpecMatchesCode checks the constants the specification states
// literally against the code.
func TestSpecMatchesCode(t *testing.T) {
	data, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(data)
	for _, want := range []string{
		"`" + Topic + "`",
		"exactly `" + Spec + "`",
		"`" + TypeVerdict + "`",
		"`" + TypeRevoke + "`",
		"exceed 4096 bytes (`MaxEventSize`)",
		"more than 300 seconds (`MaxClockSkew`)",
		"60 to 2592000 (30 days)",
		"prefix length 16 to 31",
		"prefix length 32 to 127",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("specification does not contain %q", want)
		}
	}
	if MaxEventSize != 4096 || MaxClockSkew.Seconds() != 300 || MinTTLSeconds != 60 || MaxTTLSeconds != 2592000 ||
		MinIPv4Prefix != 16 || MinIPv6Prefix != 32 {
		t.Error("a protocol constant changed; update the specification and this test")
	}
	for _, r := range append(slices.Clone(nonPublicRanges), documentationRanges...) {
		if !strings.Contains(spec, "`"+r.String()+"`") {
			t.Errorf("specification does not list the special-purpose range %s", r)
		}
	}
}
