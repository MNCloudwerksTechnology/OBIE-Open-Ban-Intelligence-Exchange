package session

import (
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"
)

// procs is a fake process tree: each process's parent and environment.
type procs map[int]struct {
	parent  int
	environ string // entries separated by NUL; "!" means unreadable
}

func (p procs) env(own map[string]string, ppid int) Env {
	return Env{
		Getenv:  func(key string) string { return own[key] },
		Getppid: func() int { return ppid },
		Environ: func(pid int) ([]byte, error) {
			proc, ok := p[pid]
			if !ok || proc.environ == "!" {
				return nil, os.ErrPermission
			}
			return []byte(proc.environ), nil
		},
		Parent: func(pid int) (int, error) {
			proc, ok := p[pid]
			if !ok {
				return 0, errors.New("no such process")
			}
			return proc.parent, nil
		},
	}
}

func TestClientAddr(t *testing.T) {
	// sshd (100) -> shell (200) -> sudo (300) -> obied, the sudo started as
	// root by an unprivileged shell.
	tree := procs{
		300: {parent: 200, environ: "HOME=/home/alice\x00SSH_CONNECTION=85.10.0.7 51234 192.0.2.10 22\x00TERM=xterm"},
		200: {parent: 100, environ: "SSH_CONNECTION=85.10.0.9 51234 192.0.2.10 22"},
		100: {parent: 1, environ: "!"},
	}
	tests := []struct {
		name string
		own  map[string]string
		ppid int
		tree procs
		want string // "" for no session
	}{
		{"own SSH_CONNECTION", map[string]string{"SSH_CONNECTION": "198.51.100.7 1 192.0.2.10 22"}, 300, tree, "198.51.100.7"},
		{"own SSH_CLIENT", map[string]string{"SSH_CLIENT": "2001:db8::7 51234 22"}, 300, tree, "2001:db8::7"},
		{"IPv4-mapped is unmapped", map[string]string{"SSH_CLIENT": "::ffff:85.10.0.7 51234 22"}, 1, nil, "85.10.0.7"},
		{"zone is dropped", map[string]string{"SSH_CLIENT": "fe80::7%eth0 51234 22"}, 1, nil, "fe80::7"},
		{"under sudo: the nearest ancestor", nil, 300, tree, "85.10.0.7"},
		{"unreadable ancestors are skipped", nil, 400, procs{
			400: {parent: 300, environ: "!"}, 300: tree[300], 200: tree[200], 100: tree[100]}, "85.10.0.7"},
		{"local console", nil, 500, procs{500: {parent: 1, environ: "HOME=/root"}}, ""},
		{"invalid address is ignored", map[string]string{"SSH_CONNECTION": "not-an-ip 1 2 3"}, 1, nil, ""},
		{"a loop in the tree ends the search", nil, 600, procs{600: {parent: 600, environ: "X=1"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ClientAddr(tt.tree.env(tt.own, tt.ppid))
			if tt.want == "" {
				if ok {
					t.Errorf("ClientAddr = %s, want no session", got)
				}
				return
			}
			if !ok || got != netip.MustParseAddr(tt.want) {
				t.Errorf("ClientAddr = %s, %v; want %s", got, ok, tt.want)
			}
		})
	}
}

// TestClientAddrDepthIsBounded checks that a very deep tree is not walked
// to its end.
func TestClientAddrDepthIsBounded(t *testing.T) {
	tree := procs{}
	for pid := 2; pid < 2+2*maxDepth; pid++ {
		tree[pid] = struct {
			parent  int
			environ string
		}{parent: pid + 1, environ: ""}
	}
	tree[2+2*maxDepth] = struct {
		parent  int
		environ string
	}{parent: 1, environ: "SSH_CONNECTION=85.10.0.7 1 2 3"}
	if addr, ok := ClientAddr(tree.env(nil, 2)); ok {
		t.Errorf("ClientAddr walked %d levels up to %s", 2*maxDepth, addr)
	}
}

// TestProcReaders checks the readers of the real /proc on this process.
func TestProcReaders(t *testing.T) {
	parent, err := procParent(os.Getpid())
	if err != nil || parent != os.Getppid() {
		t.Errorf("procParent = %d, %v; want %d", parent, err, os.Getppid())
	}
	data, err := procEnviron(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("PATH"); path != "" && environLookup(data)("PATH") != path {
		t.Errorf("environ lacks PATH=%s: %q", path, strings.ReplaceAll(string(data), "\x00", " "))
	}
	if _, err := procParent(-1); err == nil {
		t.Error("procParent of no process succeeded")
	}
}

// TestClientAddrRealProcess checks the defaults against this process.
func TestClientAddrRealProcess(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "203.0.113.9 51234 192.0.2.10 22")
	if got, ok := ClientAddr(Env{}); !ok || got != netip.MustParseAddr("203.0.113.9") {
		t.Errorf("ClientAddr = %s, %v", got, ok)
	}
}

func TestLockoutWarning(t *testing.T) {
	addr := netip.MustParseAddr("85.10.0.7")
	// The banner runs obiectl as it is given, e.g. with the node's socket.
	ctl := "sudo obiectl --socket /run/node-a.sock"
	observe, enforce := LockoutWarning(addr, false, ctl), LockoutWarning(addr, true, ctl)
	for _, want := range []string{"LOCKOUT RISK", "85.10.0.7, and OBIE does not protect", "add 85.10.0.7/32 to allowlist.cidrs",
		"or on a running node: sudo obiectl --socket /run/node-a.sock allow 85.10.0.7"} {
		if !strings.Contains(observe, want) || !strings.Contains(enforce, want) {
			t.Errorf("warning lacks %q:\n%s", want, observe)
		}
	}
	if !strings.Contains(observe, "observe mode") || !strings.Contains(enforce, "at any moment") {
		t.Errorf("warnings do not name the mode:\n%s\n%s", observe, enforce)
	}
	for _, line := range strings.Split(strings.TrimSuffix(enforce, "\n"), "\n") {
		if !strings.HasPrefix(line, "!!") {
			t.Errorf("line %q is not marked", line)
		}
	}
}
