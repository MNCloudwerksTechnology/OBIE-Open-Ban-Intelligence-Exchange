// Package docker_test checks the configuration baked into the container
// image.
package docker_test

import (
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

func TestImageConfigIsValid(t *testing.T) {
	cfg, err := config.Load("obie.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// The image has no obie group and cannot program the host firewall.
	if cfg.Admin.SocketGroup != "nonroot" || cfg.Enforce.Backend != config.BackendDryRun {
		t.Errorf("admin.socket_group = %q, enforce.backend = %q; want nonroot and dryrun",
			cfg.Admin.SocketGroup, cfg.Enforce.Backend)
	}
	if cfg.Node.StateDir != "/var/lib/obie" || cfg.Admin.Socket != "/run/obie/obie.sock" {
		t.Errorf("node.state_dir = %q, admin.socket = %q; the Dockerfile creates /var/lib/obie and /run/obie",
			cfg.Node.StateDir, cfg.Admin.Socket)
	}
}
