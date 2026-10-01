//go:build !(privileged && linux)

package e2e

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package if a goroutine outlives its tests: every node
// must stop everything it started.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
