package docs

import (
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/selfcheck"
)

const setupGuidePath = "documentation/operations/setup.md"

// TestSetupGuideListsEveryCheck keeps the setup and self-check guide in
// step with the self-check: every check has a row with its ID, in the order
// the self-check reports them, and every exit status is explained.
func TestSetupGuideListsEveryCheck(t *testing.T) {
	doc := readRepoFile(t, setupGuidePath)
	last := -1
	for _, id := range selfcheck.IDs {
		i := strings.Index(doc, " (`"+id+"`) |")
		switch {
		case i < 0:
			t.Errorf("the guide has no row for the check %s", id)
		case i < last:
			t.Errorf("the check %s is not in the order of the report", id)
		default:
			last = i
		}
	}
	for _, row := range []string{"| 0 | no problem", "| 1 | at least one problem", "| 2 | wrong usage", "| 3 | the report could not be written"} {
		if !strings.Contains(doc, row) {
			t.Errorf("the guide does not explain the exit status %q", row)
		}
	}
}
