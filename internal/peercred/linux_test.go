//go:build linux

package peercred

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestUnix(t *testing.T) {
	dir, err := os.MkdirTemp("", "obie")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "peer.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	server, _ := connect(t, ln, "unix", path)
	cred, err := Unix(server)
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
	tcpServer, _ := connect(t, tcp, "tcp", tcp.Addr().String())
	if _, err := Unix(tcpServer); err == nil {
		t.Error("Unix on a TCP connection: no error")
	}
}

func TestLoopbackTCP(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0"} {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			t.Logf("%s: %v (no IPv6 loopback?)", addr, err)
			continue
		}
		server, _ := connect(t, ln, "tcp", ln.Addr().String())
		cred, err := LoopbackTCP(server)
		if err != nil {
			t.Errorf("%s: LoopbackTCP = %v", addr, err)
		} else if int(cred.UID) != os.Getuid() || cred.GID != UnknownGID || cred.PID != 0 {
			t.Errorf("%s: LoopbackTCP = %+v, want uid %d and an unknown gid", addr, cred, os.Getuid())
		}
		_ = ln.Close()
	}
}

// connect dials ln and returns both ends; they are closed when the test
// ends.
func connect(t *testing.T, ln net.Listener, network, addr string) (server, client net.Conn) {
	t.Helper()
	client, err := net.Dial(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server, err = ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, client
}
