package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/cli"
)

// shareFiles are the files gendocs -out writes.
var shareFiles = []string{
	"man/man1/obied.1", "man/man1/obiectl.1",
	"bash-completion/completions/obied", "bash-completion/completions/obiectl",
	"zsh/site-functions/_obied", "zsh/site-functions/_obiectl",
	"fish/vendor_completions.d/obied.fish", "fish/vendor_completions.d/obiectl.fish",
}

// readTree returns every file below dir by its slash-separated path.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path) // #nosec G304 G122 -- a file the test wrote.
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestShareIsCompleteAndReproducible(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1790000000")
	var trees []map[string]string
	for range 2 {
		dir := t.TempDir()
		var stderr bytes.Buffer
		if code := run([]string{"-out", dir, "-version", "0.1.0"}, &stderr); code != 0 {
			t.Fatalf("exit code %d: %s", code, stderr.String())
		}
		trees = append(trees, readTree(t, dir))
	}
	got := trees[0]
	if len(got) != len(shareFiles) {
		t.Errorf("wrote %d files, want %d: %v", len(got), len(shareFiles), got)
	}
	for _, f := range shareFiles {
		switch {
		case got[f] == "":
			t.Errorf("%s is missing or empty", f)
		case got[f] != trees[1][f]:
			t.Errorf("%s differs between two runs", f)
		}
	}
	if !strings.Contains(got["man/man1/obiectl.1"], `.TH OBIECTL 1 "2026\-09\-21" "OBIE 0.1.0"`) {
		t.Errorf("the manual page does not carry the date of SOURCE_DATE_EPOCH:\n%.200s", got["man/man1/obiectl.1"])
	}
	if !strings.HasPrefix(got["zsh/site-functions/_obied"], "#compdef obied\n") {
		t.Errorf("the zsh completion is not a #compdef function")
	}
}

func TestReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli.md")
	var stderr bytes.Buffer
	if code := run([]string{"-reference", path}, &stderr); code != 0 {
		t.Fatalf("exit code %d: %s", code, stderr.String())
	}
	got, err := os.ReadFile(path) // #nosec G304 -- the file the test wrote.
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := cli.WriteReference(&want); err != nil || !bytes.Equal(got, want.Bytes()) {
		t.Errorf("the reference differs from cli.WriteReference (%v)", err)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"-out", "x"}, {"-out", "x", "-version", "1", "-reference", "y"}, {"-reference", "y", "extra"}, {"-bogus"}} {
		var stderr bytes.Buffer
		if code := run(args, &stderr); code != 2 {
			t.Errorf("%v: exit code %d, stderr %q", args, code, stderr.String())
		}
	}
	t.Setenv("SOURCE_DATE_EPOCH", "yesterday")
	var stderr bytes.Buffer
	if code := run([]string{"-out", t.TempDir(), "-version", "0.1.0"}, &stderr); code != 1 || !strings.Contains(stderr.String(), "SOURCE_DATE_EPOCH") {
		t.Errorf("bad SOURCE_DATE_EPOCH: exit code %d, stderr %q", code, stderr.String())
	}
}
