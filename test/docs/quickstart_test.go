package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const quickstartPath = "documentation/operations/quickstart.md"

var (
	// mappingRow is a row of the quick start's command table:
	// | `command prefix` | what exercises it |.
	mappingRow = regexp.MustCompile("^\\| `([^`]+)` \\| (.+) \\|$")
	ciStepRef  = regexp.MustCompile("CI step `([^`]+)`")
	codeRef    = regexp.MustCompile("`([^`]+)`")
	testFunc   = regexp.MustCompile(`(?m)^func (Test\w+)\(`)
)

// TestQuickstartCommandsAreTested enforces the quick start's command
// table: every shell command of the page has a row, every row matches a
// command, and every test, make target, CI step and file a row names
// exists — make targets and CI steps in the CI workflow.
func TestQuickstartCommandsAreTested(t *testing.T) {
	page := readRepoFile(t, quickstartPath)
	commands := shellCommands(page)
	if len(commands) == 0 {
		t.Fatal("the quick start shows no shell commands")
	}
	rows := commandTable(page)
	for _, cmd := range commands {
		if matchingRow(rows, cmd) == "" {
			t.Errorf("command %q has no row in the table of how the quick start is tested", cmd)
		}
	}
	for prefix := range rows {
		if !matchesAny(commands, prefix) {
			t.Errorf("table row %q matches no command of the quick start", prefix)
		}
	}

	tests := testFunctions(t)
	makefile := readRepoFile(t, "Makefile")
	ci := readRepoFile(t, ".gitea/workflows/ci.yml")
	for prefix, cell := range rows {
		checked := 0
		for _, m := range ciStepRef.FindAllStringSubmatch(cell, -1) {
			checked++
			if !strings.Contains(ci, "- name: "+m[1]+"\n") {
				t.Errorf("row %q: CI step %q is not in .gitea/workflows/ci.yml", prefix, m[1])
			}
		}
		for _, m := range codeRef.FindAllStringSubmatch(ciStepRef.ReplaceAllString(cell, ""), -1) {
			ref := m[1]
			switch {
			case strings.HasPrefix(ref, "Test"):
				checked++
				if !tests[ref] {
					t.Errorf("row %q: test %s does not exist", prefix, ref)
				}
			case strings.HasPrefix(ref, "make "):
				checked++
				target := strings.TrimPrefix(ref, "make ")
				if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:`).MatchString(makefile) {
					t.Errorf("row %q: make target %s does not exist", prefix, target)
				}
				if !strings.Contains(ci, ref) {
					t.Errorf("row %q: CI does not run %s", prefix, ref)
				}
			case strings.Contains(ref, "/"):
				checked++
				if _, err := os.Stat(filepath.Join(repoRoot, ref)); err != nil {
					t.Errorf("row %q: %v", prefix, err)
				}
			}
		}
		if checked == 0 {
			t.Errorf("row %q names no test, make target, CI step or file", prefix)
		}
	}
}

// shellCommands returns the commands of the page's sh code blocks, with
// continuation lines joined and a leading sudo removed.
func shellCommands(page string) []string {
	var cmds []string
	inBlock, pending := false, ""
	for _, line := range strings.Split(page, "\n") {
		switch {
		case !inBlock && line == "```sh":
			inBlock = true
		case inBlock && line == "```":
			inBlock = false
		case inBlock:
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if cont, ok := strings.CutSuffix(line, "\\"); ok {
				pending += cont
				continue
			}
			cmds = append(cmds, strings.TrimPrefix(pending+line, "sudo "))
			pending = ""
		}
	}
	return cmds
}

// commandTable returns the rows of the command table: command prefix →
// "exercised by" cell.
func commandTable(page string) map[string]string {
	rows := map[string]string{}
	for _, line := range strings.Split(page, "\n") {
		if m := mappingRow.FindStringSubmatch(line); m != nil {
			rows[m[1]] = m[2]
		}
	}
	return rows
}

func matchingRow(rows map[string]string, cmd string) string {
	for prefix := range rows {
		if strings.HasPrefix(cmd, prefix) {
			return prefix
		}
	}
	return ""
}

func matchesAny(cmds []string, prefix string) bool {
	for _, cmd := range cmds {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}

// testFunctions returns the names of every Go test function in the
// repository, whatever its build tags.
func testFunctions(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	walkRepo(t, "_test.go", func(_ string, data []byte) {
		for _, m := range testFunc.FindAllSubmatch(data, -1) {
			names[string(m[1])] = true
		}
	})
	return names
}
