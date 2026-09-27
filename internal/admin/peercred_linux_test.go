//go:build linux

package admin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

func TestReadPeerCred(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := net.Dial("unix", path)
		if err == nil {
			defer func() { _ = c.Close() }()
			_, _ = c.Read(make([]byte, 1))
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	cred, err := readPeerCred(c)
	if err != nil {
		t.Fatal(err)
	}
	if int(cred.UID) != os.Getuid() || int(cred.GID) != os.Getgid() || int(cred.PID) != os.Getpid() {
		t.Errorf("peer credentials = %+v, want uid %d gid %d pid %d", cred, os.Getuid(), os.Getgid(), os.Getpid())
	}

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tcp.Close() }()
	go func() {
		if c, err := net.Dial("tcp", tcp.Addr().String()); err == nil {
			_ = c.Close()
		}
	}()
	tc, err := tcp.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tc.Close() }()
	if _, err := readPeerCred(tc); err == nil {
		t.Error("readPeerCred on a TCP connection: no error")
	}
}

// TestSocketRefusesPeersOutsidePolicy serves the admin API with a policy
// that admits neither the test's user nor its group.
func TestSocketRefusesPeersOutsidePolicy(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root is always admitted")
	}
	path := socketPath(t)
	policy := accessPolicy{selfUID: os.Getuid() + 1, group: "obie", gid: -1}
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
	policy := accessPolicy{selfUID: os.Getuid() + 1, group: "testers", gid: os.Getgid(), groupsOf: userGroups}
	s := newServer(path, "obie-no-such-group", policy, testInfo(lifecycle.Status{Name: "admin", State: lifecycle.StateRunning, Ready: true}), discardLogger())
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	if _, err := NewClient(path).Status(context.Background()); err != nil {
		t.Errorf("Status as a member of the socket group = %v", err)
	}
}
