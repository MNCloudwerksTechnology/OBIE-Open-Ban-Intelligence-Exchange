package setup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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

func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %04o, want %04o", path, got, want)
	}
}

func TestWriteCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "obie", "obie.yaml")
	backup, err := Write(path, []byte("node: {}\n"), WriteOptions{Group: "obie-test-no-such-group"})
	if err != nil || backup != "" {
		t.Fatalf("Write = %q, %v", backup, err)
	}
	if got := readFile(t, path); got != "node: {}\n" {
		t.Errorf("file holds %q", got)
	}
	assertMode(t, path, FileMode)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Errorf("directory holds %v (%v), want only the file", entries, err)
	}
}

func TestWriteNeverReplacesWithoutConsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "obie.yaml")
	if err := os.WriteFile(path, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(path, []byte("new\n"), WriteOptions{}); !errors.Is(err, ErrExists) {
		t.Errorf("Write over an existing file: %v, want ErrExists", err)
	}
	if got := readFile(t, path); got != "mine\n" {
		t.Errorf("the existing file was changed to %q", got)
	}
}

func TestWriteReplacesAndKeepsBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "obie.yaml")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, content := range []string{"second\n", "third\n"} {
		backup, err := Write(path, []byte(content), WriteOptions{Replace: true})
		if err != nil {
			t.Fatal(err)
		}
		wantBackup := []string{path + ".bak", path + ".bak.1"}[i]
		if backup != wantBackup {
			t.Errorf("backup = %q, want %q", backup, wantBackup)
		}
		if got := readFile(t, path); got != content {
			t.Errorf("file holds %q, want %q", got, content)
		}
		assertMode(t, path, FileMode)
	}
	if got := readFile(t, path+".bak"); got != "first\n" {
		t.Errorf("first backup holds %q", got)
	}
	if got := readFile(t, path+".bak.1"); got != "second\n" {
		t.Errorf("second backup holds %q", got)
	}
}

func TestWriteRefusesNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obie.yaml")
	if err := os.Symlink(filepath.Join(dir, "elsewhere.yaml"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(path, []byte("x\n"), WriteOptions{Replace: true}); err == nil {
		t.Error("Write replaced a symbolic link")
	}
}

func TestCheckPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obie.yaml")
	if err := CheckPath(path); err != nil {
		t.Errorf("CheckPath of a new file: %v", err)
	}
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckPath(path); err != nil {
		t.Errorf("CheckPath of a regular file: %v", err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckPath(link); err == nil || !strings.Contains(err.Error(), "is not a regular file (a symbolic link?)") {
		t.Errorf("CheckPath of a symbolic link: %v", err)
	}
	if err := CheckPath(path + "\nadmin: {}"); err == nil || !strings.Contains(err.Error(), "line break") {
		t.Errorf("CheckPath of a path with a line break: %v", err)
	}
}

// TestWriteKeepsTheGroupItCannotGive checks that a user who is not root
// writes a file of their own when they may not give it the group.
func TestWriteKeepsTheGroupItCannotGive(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 || os.Getegid() == 0 || slices.Contains(groups, 0) {
		t.Skip("the user may give files to the group root")
	}
	path := filepath.Join(t.TempDir(), "obie.yaml")
	if _, err := Write(path, []byte("x\n"), WriteOptions{Group: "root"}); err != nil {
		t.Fatalf("Write with a group the user is not in: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Gid) == 0 {
		t.Error("the file got the group root")
	}
}

func TestCheckWritable(t *testing.T) {
	dir := t.TempDir()
	if err := CheckWritable(filepath.Join(dir, "missing", "obie.yaml")); err != nil {
		t.Errorf("CheckWritable below a writable directory: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root may write anywhere")
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	err := CheckWritable(filepath.Join(locked, "obie.yaml"))
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("CheckWritable in a read-only directory: %v, want a permission error", err)
	}
	if _, err := Write(filepath.Join(locked, "obie.yaml"), []byte("x\n"), WriteOptions{}); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Write in a read-only directory: %v", err)
	}
}
