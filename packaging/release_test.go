package packaging_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// describedRelease is how documentation/capabilities.md names the release
// it describes.
var describedRelease = regexp.MustCompile(`\*\*OBIE (\d+\.\d+\.\d+)\*\*`)

// staleOverview is the start of release.sh's refusal to build a release
// that the capability overview does not describe.
const staleOverview = "documentation/capabilities.md does not describe OBIE"

// TestReleaseRefusesStaleCapabilities checks that release.sh refuses to
// build a final release whose version the capability overview does not
// name, before it builds anything, and lets the overview's own version and
// pre-releases (such as CI's 0.0.0-ci) pass.
func TestReleaseRefusesStaleCapabilities(t *testing.T) {
	data, err := os.ReadFile("../documentation/capabilities.md")
	if err != nil {
		t.Fatal(err)
	}
	m := describedRelease.FindSubmatch(data)
	if m == nil {
		t.Fatal("documentation/capabilities.md names no release as **OBIE x.y.z**")
	}
	described := string(m[1])

	for _, tc := range []struct {
		version string
		refused bool
	}{
		{"99.0.0", true},
		{described, false},
		{"99.0.0-rc.1", false},
		{"0.0.0-ci", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			out := t.TempDir()
			cmd := exec.Command("sh", "release.sh", out)
			// GO=false: the first build fails, unless the overview stopped
			// the script before.
			cmd.Env = append(os.Environ(), "VERSION="+tc.version, "GO=false", "CYCLONEDX_GOMOD=/nonexistent")
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("release.sh with GO=false succeeded:\n%s", output)
			}
			if refused := strings.Contains(string(output), staleOverview); refused != tc.refused {
				t.Errorf("VERSION=%s: refused = %v, want %v:\n%s", tc.version, refused, tc.refused, output)
			}
		})
	}
}
