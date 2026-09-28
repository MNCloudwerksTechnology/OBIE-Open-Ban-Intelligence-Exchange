package mesh

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package if a goroutine outlives its tests.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
