package cli

import (
	"bytes"
	"path/filepath"
	"testing"
)

// examplePath is the documented example configuration, relative to this package.
var examplePath = filepath.Join("..", "..", "documentation", "examples", "obie.yaml")

func TestExampleConfigPassesCheckConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := RunDaemon([]string{"--config", examplePath, "--check-config"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("obied --check-config on %s: exit code %d, stderr:\n%s", examplePath, code, stderr.String())
	}
}
