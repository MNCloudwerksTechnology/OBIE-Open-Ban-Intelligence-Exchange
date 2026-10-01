//go:build linux

package admin

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/peercred"
)

// TestSocketRefusesPeersOutsidePolicy serves the admin API with a policy
// that admits neither the test's user nor its group.
func TestSocketRefusesPeersOutsidePolicy(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root is always admitted")
	}
	path := socketPath(t)
	policy := peercred.Policy{SelfUID: os.Getuid() + 1, Group: "obie", GID: -1}
	s := newServer(path, "obie-no-such-group", policy, testInfo(lifecycle.Status{Name: "admin", State: lifecycle.StateRunning, Ready: true}), discardLogger())
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_, err := NewClient(path).Status(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden || !strings.Contains(apiErr.Message, "neither root nor a member") {
		t.Errorf("Status as an unauthorized peer = %v, want 403", err)
	}

	// With the default policy the same user, obied's own, is served.
	allowed := socketPath(t)
	startServer(t, allowed, "obie-no-such-group", discardLogger())
	if _, err := NewClient(allowed).Status(context.Background()); err != nil {
		t.Errorf("Status as obied's own user = %v", err)
	}
}

func TestSocketAdmitsGroupMembers(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root is always admitted")
	}
	path := socketPath(t)
	policy := peercred.Policy{SelfUID: os.Getuid() + 1, Group: "testers", GID: os.Getgid()}
	s := newServer(path, "obie-no-such-group", policy, testInfo(lifecycle.Status{Name: "admin", State: lifecycle.StateRunning, Ready: true}), discardLogger())
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	if _, err := NewClient(path).Status(context.Background()); err != nil {
		t.Errorf("Status as a member of the socket group = %v", err)
	}
}
