package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
)

// TestRunTestingHooks runs a node with the test hooks: the ops endpoints
// on a port chosen by the OS, the bound addresses reported and the store
// handed out.
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
	var db *store.DB
	go func() {
		done <- Run(ctx, &cfg, logging.New(&syncBuffer{}, slog.LevelInfo), Options{Env: noHost, Testing: Testing{
			AllowDocumentationRanges: true,
			MetricsListen:            "127.0.0.1:0",
			Started:                  func(e Endpoints) { started <- e },
			Store:                    func(s *store.DB) { db = s },
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
		if db == nil || db.Ready() != nil {
			t.Errorf("Store hook gave %v, want the node's open store", db)
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

// TestRunRefusesCorruptStore checks that a damaged event store stops obied
// at start with a message naming the damage and the remedy.
func TestRunRefusesCorruptStore(t *testing.T) {
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
	db := filepath.Join(cfg.Node.StateDir, "db")
	if err := os.MkdirAll(db, 0o700); err != nil {
		t.Fatal(err)
	}
	// A table without the MANIFEST that lists it.
	if err := os.WriteFile(filepath.Join(db, "000001.sst"), []byte("table"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = Run(ctx, &cfg, logging.New(&syncBuffer{}, slog.LevelInfo), Options{Env: noHost, Testing: Testing{MetricsListen: "127.0.0.1:0"}})
	if err == nil || !errors.Is(err, store.ErrCorrupt) || !strings.Contains(err.Error(), "MANIFEST is missing") ||
		!strings.Contains(err.Error(), "mv "+db+" "+db+".corrupt") {
		t.Errorf("Run = %v, want the corrupt store named with its remedy", err)
	}
	if _, err := os.Stat(cfg.Admin.Socket); !os.IsNotExist(err) {
		t.Errorf("admin socket created although the store did not open: %v", err)
	}
}
