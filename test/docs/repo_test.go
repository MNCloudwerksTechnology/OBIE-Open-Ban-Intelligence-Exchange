package docs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is the repository root, relative to this package.
var repoRoot = filepath.Join("..", "..")

// skippedDirs are not part of the repository's own documentation or code.
var skippedDirs = map[string]bool{".git": true, "bin": true, "dist": true, "node_modules": true, "website": true, ".agentic": true}

// walkRepo calls fn for every file below the repository root whose name
// has the suffix, skipping skippedDirs.
func walkRepo(t *testing.T, suffix string, fn func(path string, data []byte)) {
	t.Helper()
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, suffix) {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G304 G122 -- repository file.
		if err != nil {
			return err
		}
		fn(path, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, path)) // #nosec G304 -- repository file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
