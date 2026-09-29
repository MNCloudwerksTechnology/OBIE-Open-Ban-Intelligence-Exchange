package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// repoRoot is the repository root, relative to this package.
var repoRoot = filepath.Join("..", "..")

// sandboxDir holds the sandbox: its Compose file, init and script.
var sandboxDir = filepath.Join(repoRoot, "packaging", "sandbox")

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, path)) // #nosec G304 -- repository file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// obied is built once, for the tests that run sandbox-init.sh.
var obied struct {
	once sync.Once
	dir  string
	err  error
	out  []byte
}

// obiedDir returns a directory that holds an obied built from this tree.
func obiedDir(t *testing.T) string {
	t.Helper()
	obied.once.Do(func() {
		obied.dir, obied.err = os.MkdirTemp("", "obie-sandbox-obied-")
		if obied.err != nil {
			return
		}
		build := exec.Command("go", "build", "-o", filepath.Join(obied.dir, "obied"), "./cmd/obied") // #nosec G204 -- builds this repository.
		build.Dir = repoRoot
		obied.out, obied.err = build.CombinedOutput()
	})
	if obied.err != nil {
		t.Fatalf("go build ./cmd/obied: %v\n%s", obied.err, obied.out)
	}
	return obied.dir
}

// TestMain removes the obied built for the tests.
func TestMain(m *testing.M) {
	code := m.Run()
	if obied.dir != "" {
		_ = os.RemoveAll(obied.dir)
	}
	os.Exit(code)
}
