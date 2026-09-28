package daemon

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
)

// TestRunTestingHooks runs a node with the test hooks: the ops endpoints
// on a port chosen by the OS and the bound addresses reported.
func TestRunTestingHooks(t *testing.T) {
	// Unix socket paths are limited to about 100 bytes; t.TempDir can exceed that.
	dir, err := os.MkdirTemp("", "obie")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	cfg := config.Default()
	cfg.Node.StateDir = filepath.Join(dir, "state")
	cfg.Admin.Socket = filepath.Join(dir, "obie.sock")
	cfg.Admin.SocketGroup = "obie-test-no-such-group"
	cfg.Mesh.Listen = []string{"/ip4/127.0.0.1/tcp/0"}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan Endpoints, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, &cfg, logging.New(&syncBuffer{}, slog.LevelInfo), Options{Env: noHost, Testing: Testing{
			AllowDocumentationRanges: true,
			MetricsListen:            "127.0.0.1:0",
			Started:                  func(e Endpoints) { started <- e },
		}})
	}()
	select {
	case e := <-started:
		if len(e.Mesh) != 1 || !strings.HasPrefix(e.Mesh[0], "/ip4/127.0.0.1/tcp/") || strings.HasSuffix(e.Mesh[0], "/tcp/0") {
			t.Errorf("mesh endpoints = %v, want one bound TCP address", e.Mesh)
		}
		if _, port, err := net.SplitHostPort(e.Metrics); err != nil || port == "0" {
			t.Errorf("metrics endpoint = %q, want a bound port", e.Metrics)
		}
	case err := <-done:
		t.Fatalf("Run returned before starting: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("Started was not called")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run = %v", err)
	}
}
