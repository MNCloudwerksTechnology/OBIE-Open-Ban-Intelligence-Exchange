package statedir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFormat(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(Path(dir), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareCreatesAndStampsNewDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	prev, err := Prepare(dir, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if prev != 0 {
		t.Errorf("previous format = %d, want 0", prev)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("state directory mode = %04o, want no group/other bits", perm)
	}
	if got := readFile(t, Path(dir)); got != "1\n" {
		t.Errorf("%s = %q, want %q", FileName, got, "1\n")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("state directory holds %d entries, want only %s (no temporary file left)", len(entries), FileName)
	}
}

func TestPrepareStampsLegacyDirectory(t *testing.T) {
	dir := t.TempDir()
	// A state directory written before the format file existed.
	if err := os.WriteFile(filepath.Join(dir, "node.key"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(dir, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, Path(dir)); got != "1\n" {
		t.Errorf("%s = %q, want %q", FileName, got, "1\n")
	}
	if got := readFile(t, filepath.Join(dir, "node.key")); got != "key" {
		t.Errorf("node.key changed to %q", got)
	}
}

func TestPrepareAcceptsCurrentFormat(t *testing.T) {
	dir := t.TempDir()
	writeFormat(t, dir, " 1 \n")
	prev, err := Prepare(dir, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if prev != Version {
		t.Errorf("previous format = %d, want %d", prev, Version)
	}
	if got := readFile(t, Path(dir)); got != " 1 \n" {
		t.Errorf("%s rewritten to %q; a current directory must be left alone", FileName, got)
	}
}

func TestPrepareRefusesNewerFormat(t *testing.T) {
	dir := t.TempDir()
	writeFormat(t, dir, "2\n")
	prev, err := Prepare(dir, "1.2.3")
	if !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("Prepare error = %v, want ErrNewerFormat", err)
	}
	if prev != 2 {
		t.Errorf("previous format = %d, want 2", prev)
	}
	for _, want := range []string{dir, "format 2", "obied 1.2.3", "format 1 or older", "newer obied", "backup"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if got := readFile(t, Path(dir)); got != "2\n" {
		t.Errorf("%s changed to %q; a newer directory must not be touched", FileName, got)
	}
}

func TestPrepareRefusesInvalidFormatFile(t *testing.T) {
	for name, content := range map[string]string{
		"empty":     "",
		"text":      "one\n",
		"zero":      "0\n",
		"negative":  "-1\n",
		"oversized": strings.Repeat("1", maxFileSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFormat(t, dir, content)
			_, err := Prepare(dir, "1.2.3")
			if err == nil || errors.Is(err, ErrNewerFormat) {
				t.Fatalf("Prepare error = %v, want an invalid-format error", err)
			}
			if !strings.Contains(err.Error(), Path(dir)) {
				t.Errorf("error %q does not name %s", err, Path(dir))
			}
		})
	}
}

func TestCheck(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "state")
		if err := Check(dir, "1.2.3"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("Check created the state directory: %v", err)
		}
	})
	t.Run("no format file", func(t *testing.T) {
		dir := t.TempDir()
		if err := Check(dir, "1.2.3"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
			t.Errorf("Check stamped the state directory: %v", err)
		}
	})
	t.Run("current", func(t *testing.T) {
		dir := t.TempDir()
		writeFormat(t, dir, "1\n")
		if err := Check(dir, "1.2.3"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("newer", func(t *testing.T) {
		dir := t.TempDir()
		writeFormat(t, dir, "3\n")
		if err := Check(dir, "1.2.3"); !errors.Is(err, ErrNewerFormat) || !strings.Contains(err.Error(), "format 3") {
			t.Errorf("Check = %v, want ErrNewerFormat naming format 3", err)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		dir := t.TempDir()
		writeFormat(t, dir, "x\n")
		if err := Check(dir, "1.2.3"); err == nil || errors.Is(err, ErrNewerFormat) {
			t.Errorf("Check = %v, want an invalid-format error", err)
		}
	})
}

func TestPrepareFailsOnUnreadableFormatFile(t *testing.T) {
	dir := t.TempDir()
	// A directory where the format file belongs cannot be read as one.
	if err := os.Mkdir(Path(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(dir, "1.2.3"); err == nil {
		t.Fatal("Prepare succeeded on a directory named FORMAT")
	}
}

func TestPrepareFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(filepath.Join(file, "state"), "1.2.3"); err == nil {
		t.Fatal("Prepare succeeded below a regular file")
	}
}
