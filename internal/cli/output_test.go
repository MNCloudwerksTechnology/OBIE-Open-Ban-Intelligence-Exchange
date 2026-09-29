package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                       "0s",
		1500 * time.Millisecond: "1s",
		time.Hour + 2*time.Minute + 3*time.Second: "1h2m3s",
		72 * time.Hour:               "3d",
		76*time.Hour + 5*time.Minute: "3d4h5m0s",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestLimitMustNotBeNegative(t *testing.T) {
	for _, command := range []string{"decisions", "enforced"} {
		var stdout, stderr bytes.Buffer
		if code := RunCtl([]string{"--socket", "/nonexistent/obie.sock", command, "--limit", "-1"}, &stdout, &stderr); code != ExitUsage ||
			!strings.Contains(stderr.String(), "--limit must be 0 (every row) or more, got -1") {
			t.Errorf("%s: exit code %d, stderr %q", command, code, stderr.String())
		}
	}
}

// TestNoColor keeps color out of the output of both tools and of the
// self-check: labels are words, so that output piped into another program
// and read without color means the same.
func TestNoColor(t *testing.T) {
	for _, dir := range []string{".", filepath.Join("..", "selfcheck"), filepath.Join("..", "setup")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			data, err := os.ReadFile(file) // #nosec G304 -- a source file of the repository.
			if err != nil {
				t.Fatal(err)
			}
			for _, esc := range []string{`\x1b`, `\033`, `\u001b`, `\u001B`, "\x1b"} {
				if strings.Contains(string(data), esc) {
					t.Errorf("%s writes an escape sequence (%q); OBIE's output has no color", file, esc)
				}
			}
		}
	}
}
