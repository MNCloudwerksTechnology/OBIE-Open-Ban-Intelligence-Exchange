//go:build privileged && linux

// The privileged variant enforces with nftables. TestMain re-executes the
// test binary under `unshare -rn` (or `unshare -n` as root), so the host's
// firewall and loopback are never touched; inside, every node programs a
// network namespace of its own. If no namespace can be created, the
// nftables test is skipped. See CONTRIBUTING.md.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/daemon"
)

// netnsEnv marks the re-executed test binary running in its own namespace.
const netnsEnv = "OBIE_E2E_NETNS"

var isolated = os.Getenv(netnsEnv) == "1"

func TestMain(m *testing.M) {
	if !isolated {
		if code, ok := reexecIsolated(); ok {
			os.Exit(code)
		}
	} else if out, err := exec.Command("ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		// The nodes talk over 127.0.0.1, which is down in a new namespace.
		fmt.Fprintf(os.Stderr, "bring up lo in the test namespace: %v: %s\n", err, out)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// reexecIsolated runs the test binary again in a new network namespace and
// returns its exit code; ok is false if no namespace can be created.
func reexecIsolated() (code int, ok bool) {
	for _, flags := range [][]string{{"-rn"}, {"-n"}} {
		if exec.Command("unshare", append(flags, "true")...).Run() != nil { // #nosec G204 -- fixed arguments.
			continue
		}
		args := append(append(flags, "--"), os.Args...)
		cmd := exec.Command("unshare", args...) // #nosec G204 G702 -- re-executes this test binary.
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		cmd.Env = append(os.Environ(), netnsEnv+"=1")
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), true
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "re-exec in a network namespace: %v\n", err)
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// TestSignalToEnforcementNFTables runs the core scenario on the nftables
// backend: A, B and C each program the firewall of their own network
// namespace. In C's namespace, X is a local address with a TCP listener;
// connections from X must pass until C blocks X, be dropped while it does
// and pass again after A revokes its verdict.
func TestSignalToEnforcementNFTables(t *testing.T) {
	if !isolated {
		t.Skip("cannot create a network namespace (unshare -rn / unshare -n); not touching the host firewall")
	}
	start := time.Now()
	names := []string{"A", "B", "C"}
	netns := map[string]int{}
	for _, name := range names {
		netns[name] = newNetNS(t)
	}
	x := newTarget(t, netns["C"], ipX)
	within(t, time.Second, "X reaches C before any report", x.reachable)

	c := newCluster(t, clusterOptions{
		backend: config.BackendNFTables,
		hooks:   func(name string) daemon.Testing { return daemon.Testing{NFTablesNetNS: netns[name]} },
	}, names...)
	t.Logf("cluster of 3 nodes ready after %s", time.Since(start).Round(time.Millisecond))

	steps := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"A reports X: A blocks alone, X still reaches C", func(t *testing.T) {
			stepLocalAutoblock(t, c)
			holds(t, quietPeriod, "X keeps reaching C", x.reachable)
		}},
		{"B reports X: C drops X's packets", func(t *testing.T) {
			stepQuorumBlock(t, c)
			within(t, enforcementBound, "C drops connections from X", x.dropped)
		}},
		{"A revokes X: X reaches C again", func(t *testing.T) {
			stepRevoke(t, c)
			within(t, enforcementBound, "X reaches C again", x.reachable)
		}},
	}
	for _, s := range steps {
		if !t.Run(s.name, s.run) {
			t.FailNow()
		}
	}
	if took := time.Since(start); took > suiteBound {
		t.Errorf("the end-to-end test took %s, want at most %s", took.Round(time.Millisecond), suiteBound)
	}
}

// newNetNS creates a network namespace and returns a file descriptor of
// it, closed at the end of tb.
func newNetNS(tb testing.TB) int {
	tb.Helper()
	type result struct {
		fd  int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		// The thread stays locked: it ends with the goroutine instead of
		// running other goroutines in the new namespace.
		runtime.LockOSThread()
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			ch <- result{err: err}
			return
		}
		fd, err := unix.Open("/proc/thread-self/ns/net", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		ch <- result{fd: fd, err: err}
	}()
	r := <-ch
	if r.err != nil {
		tb.Fatalf("create a network namespace: %v", r.err)
	}
	tb.Cleanup(func() { _ = unix.Close(r.fd) })
	return r.fd
}

// inNetNS runs f on a thread in the network namespace fd: sockets it
// opens and commands it starts belong to that namespace.
func inNetNS(fd int, f func() error) error {
	errc := make(chan error, 1)
	go func() {
		// The thread stays locked: it ends with the goroutine instead of
		// running other goroutines in the namespace.
		runtime.LockOSThread()
		if err := unix.Setns(fd, unix.CLONE_NEWNET); err != nil {
			errc <- fmt.Errorf("enter network namespace: %w", err)
			return
		}
		errc <- f()
	}()
	return <-errc
}

// target is an address in a node's network namespace that accepts TCP
// connections from itself, which pass that node's input chain.
type target struct {
	netns int
	ip    string
	addr  string
}

// dialTimeout bounds one connection attempt; a dropped SYN makes it expire.
const dialTimeout = 250 * time.Millisecond

// newTarget brings up lo in the namespace netns, adds ip to it and
// listens on ip.
func newTarget(t *testing.T, netns int, ip string) *target {
	t.Helper()
	var ln net.Listener
	err := inNetNS(netns, func() error {
		for _, args := range [][]string{{"link", "set", "lo", "up"}, {"addr", "add", ip + "/32", "dev", "lo"}} {
			if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil { // #nosec G204 -- fixed commands.
				return fmt.Errorf("ip %v: %w: %s", args, err, out)
			}
		}
		var err error
		ln, err = net.Listen("tcp", net.JoinHostPort(ip, "0"))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return &target{netns: netns, ip: ip, addr: ln.Addr().String()}
}

// connect opens a TCP connection from the target's address to itself.
func (x *target) connect() error {
	return inNetNS(x.netns, func() error {
		d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(x.ip)}, Timeout: dialTimeout}
		conn, err := d.Dial("tcp", x.addr)
		if err != nil {
			return err
		}
		return conn.Close()
	})
}

// reachable checks that a connection from the target's address succeeds.
func (x *target) reachable(context.Context) error {
	if err := x.connect(); err != nil {
		return fmt.Errorf("connection from %s: %w", x.ip, err)
	}
	return nil
}

// dropped checks that a connection from the target's address times out,
// its packets dropped by the firewall.
func (x *target) dropped(context.Context) error {
	err := x.connect()
	var netErr net.Error
	switch {
	case errors.As(err, &netErr) && netErr.Timeout():
		return nil
	case err == nil:
		return fmt.Errorf("connection from %s was accepted", x.ip)
	default:
		return fmt.Errorf("connection from %s failed without a timeout: %w", x.ip, err)
	}
}
