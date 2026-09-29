package cli

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReferenceIsCurrent keeps the command-line reference in the
// documentation in step with the help it is generated from.
func TestReferenceIsCurrent(t *testing.T) {
	path := filepath.Join("..", "..", filepath.FromSlash(ReferencePath))
	current, err := os.ReadFile(path) // #nosec G304 -- a file of the repository.
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := WriteReference(&want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, want.Bytes()) {
		t.Errorf("%s differs from the help; regenerate it: go run ./packaging/gendocs -reference %s", ReferencePath, ReferencePath)
	}
	for _, name := range ToolNames {
		for _, c := range toolByName(name).commands {
			if !bytes.Contains(current, []byte("\n### "+name+" "+c.name+"\n")) {
				t.Errorf("the reference has no section for %s %s", name, c.name)
			}
		}
	}
}

// TestManPages checks both manual pages: every command and flag is on
// them, groff reads them without a warning, and the commands they show
// can be copied, with plain hyphens.
func TestManPages(t *testing.T) {
	for _, name := range ToolNames {
		var b bytes.Buffer
		if err := WriteManPage(&b, name, "0.1.0", "2026-09-29"); err != nil {
			t.Fatal(err)
		}
		page := b.String()
		tl := toolByName(name)
		wants := []string{".TH " + strings.ToUpper(name) + " 1 \"2026\\-09\\-29\" \"OBIE 0.1.0\" \"OBIE Manual\"\n",
			".SH NAME\n" + name + " \\- ", ".SH SYNOPSIS\n", ".SH COMMANDS\n", ".SH \"EXIT STATUS\"\n", ".SH FILES\n", ".SH \"SEE ALSO\"\n"}
		for _, c := range tl.commands {
			wants = append(wants, ".SS \""+roffText(name+" "+c.name)+"\"\n")
			tl.flags(c.name).VisitAll(func(f *flag.Flag) { wants = append(wants, roffText("--"+f.Name)) })
		}
		for _, want := range wants {
			if !strings.Contains(page, want) {
				t.Errorf("%s(1) lacks %q", name, want)
			}
		}
		if _, err := exec.LookPath("groff"); err != nil {
			continue
		}
		lint := exec.Command("groff", "-man", "-Tutf8", "-ww", "-z") // #nosec G204 -- a fixed command.
		lint.Stdin = strings.NewReader(page)
		if out, err := lint.CombinedOutput(); err != nil || len(out) > 0 {
			t.Errorf("groff warns about %s(1): %v\n%s", name, err, out)
		}
		render := exec.Command("groff", "-man", "-Tascii", "-rLL=200n") // #nosec G204 -- a fixed command.
		render.Stdin = strings.NewReader(page)
		out, err := render.Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range tl.commands {
			for _, e := range c.examples {
				if !strings.Contains(string(out), e.command) {
					t.Errorf("%s(1) does not show %q as it is typed", name, e.command)
				}
			}
		}
	}
	if err := WriteManPage(&bytes.Buffer{}, "nft", "0.1.0", "2026-09-29"); err == nil {
		t.Error("unknown tool: no error")
	}
}

func TestRoffText(t *testing.T) {
	for in, want := range map[string]string{
		"a-b":               `a\-b`,
		`back\slash`:        `back\eslash`,
		".starts with dot":  `\&.starts with dot`,
		"x\n'quote":         "x\n\\(aqquote",
		"mid.dle and 'mid'": `mid.dle and \(aqmid\(aq`,
		"`grave`":           `\(gagrave\(ga`,
	} {
		if got := roffText(in); got != want {
			t.Errorf("roffText(%q) = %q, want %q", in, got, want)
		}
	}
}
