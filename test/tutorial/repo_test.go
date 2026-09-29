package tutorial

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot is the repository root, relative to this package.
var repoRoot = filepath.Join("..", "..")

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, path)) // #nosec G304 -- repository file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
