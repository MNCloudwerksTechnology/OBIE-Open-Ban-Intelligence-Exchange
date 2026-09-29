// Package session finds the address the operator's remote (SSH) session
// comes from, so that the setup assistant can offer to protect it and the
// self-check can warn when it is not protected from being blocked
// (ADR 0027).
package session

import (
	"bufio"
	"bytes"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// maxDepth bounds how far up the process tree ClientAddr looks.
const maxDepth = 32

// Env is how ClientAddr learns about the process; nil fields use the real
// process and /proc.
type Env struct {
	// Getenv reads the process's own environment.
	Getenv func(key string) string
	// Getppid returns the parent's process ID.
	Getppid func() int
	// Environ returns the environment process pid started with, as
	// NUL-separated KEY=value entries.
	Environ func(pid int) ([]byte, error)
	// Parent returns the parent process ID of pid.
	Parent func(pid int) (int, error)
}

func (e Env) withDefaults() Env {
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.Getppid == nil {
		e.Getppid = os.Getppid
	}
	if e.Environ == nil {
		e.Environ = procEnviron
	}
	if e.Parent == nil {
		e.Parent = procParent
	}
	return e
}

// ClientAddr returns the address of the SSH client this process runs for,
// and false outside an SSH session. It reads SSH_CONNECTION, else
// SSH_CLIENT, from the process's own environment, else from the
// environment of its nearest ancestor that has one: sudo and su drop the
// variables from the environment they pass on, but keep them in their own.
// Ancestors whose environment cannot be read (another user's processes,
// when not run as root) are skipped.
func ClientAddr(env Env) (netip.Addr, bool) {
	env = env.withDefaults()
	if addr, ok := fromVars(env.Getenv); ok {
		return addr, true
	}
	pid := env.Getppid()
	for depth := 0; depth < maxDepth && pid > 1; depth++ {
		if data, err := env.Environ(pid); err == nil {
			if addr, ok := fromVars(environLookup(data)); ok {
				return addr, true
			}
		}
		parent, err := env.Parent(pid)
		if err != nil || parent == pid {
			break
		}
		pid = parent
	}
	return netip.Addr{}, false
}

// fromVars reads the client address from SSH_CONNECTION ("client port
// server port") or SSH_CLIENT ("client port port").
func fromVars(getenv func(string) string) (netip.Addr, bool) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT"} {
		fields := strings.Fields(getenv(key))
		if len(fields) == 0 {
			continue
		}
		if addr, err := netip.ParseAddr(fields[0]); err == nil {
			return addr.WithZone("").Unmap(), true
		}
	}
	return netip.Addr{}, false
}

// environLookup returns a lookup in NUL-separated KEY=value entries.
func environLookup(data []byte) func(string) string {
	return func(key string) string {
		for _, entry := range bytes.Split(data, []byte{0}) {
			if k, v, ok := bytes.Cut(entry, []byte("=")); ok && string(k) == key {
				return string(v)
			}
		}
		return ""
	}
}

func procEnviron(pid int) ([]byte, error) {
	return os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ") // #nosec G304 -- a /proc path of a process ID.
}

// procParent reads the parent process ID from /proc/<pid>/status.
func procParent(pid int) (int, error) {
	f, err := os.Open("/proc/" + strconv.Itoa(pid) + "/status") // #nosec G304 -- a /proc path of a process ID.
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if value, ok := strings.CutPrefix(sc.Text(), "PPid:"); ok {
			return strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("no PPid in /proc/%d/status", pid)
}

// LockoutWarning is the banner shown when addr, the address of the
// operator's session, is not protected from being blocked; enforcing says
// whether the node is, or will be, in enforce mode.
func LockoutWarning(addr netip.Addr, enforcing bool) string {
	mode := "The node is in observe mode and blocks nothing yet, but it will as soon as you switch to enforce mode."
	if enforcing {
		mode = "The node is in enforce mode: this can happen at any moment."
	}
	lines := []string{
		fmt.Sprintf("LOCKOUT RISK: your SSH session comes from %s, and OBIE does not protect that address.", addr),
		"If this node or a trusted peer reports it, the node blocks it and you lose access to this server.",
		mode,
		fmt.Sprintf("Protect it: add %s to allowlist.cidrs in the configuration and reload the node", netip.PrefixFrom(addr, addr.BitLen())),
		fmt.Sprintf("(sudo systemctl reload obied), or on a running node: sudo obiectl allow %s --note \"my SSH session\"", addr),
	}
	var b strings.Builder
	rule := strings.Repeat("!", 78)
	b.WriteString(rule + "\n")
	for _, l := range lines {
		b.WriteString("!! " + l + "\n")
	}
	b.WriteString(rule + "\n")
	return b.String()
}
