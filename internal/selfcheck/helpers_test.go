package selfcheck

import (
	"net"
	"os"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

// createKey creates the node key in stateDir and returns its peer ID.
func createKey(t *testing.T, stateDir string) string {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := identity.Create(stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	return key.PeerID()
}

// listenUnix listens on a Unix socket at path until the test ends.
func listenUnix(t *testing.T, path string) net.Listener {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}
