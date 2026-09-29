package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

func TestProblemWrite(t *testing.T) {
	var out bytes.Buffer
	problem{id: "x", what: "it broke", why: "because", next: []string{"fix it", "try again"}}.write(&out, "obiectl status")
	want := "obiectl status: it broke\n  Why:  because\n  Next: fix it\n  Next: try again\n"
	if out.String() != want {
		t.Errorf("write = %q, want %q", out.String(), want)
	}
	out.Reset()
	problem{id: "x", what: "it broke"}.write(&out, "obied")
	if out.String() != "obied: it broke\n" {
		t.Errorf("write without why and next = %q", out.String())
	}
}

// TestClientProblems checks that every failure of a call to the node says
// what went wrong, why when it is known, and what to do next.
func TestClientProblems(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.sock")
	stale := staleSocket(t, dir)
	group := socketGroup(stale)
	if group == "" {
		t.Fatalf("the group of %s cannot be told", stale)
	}
	me := currentUserName()
	// The next steps run obiectl against the socket it was given.
	ctl := "sudo obiectl --socket " + stale
	notRunning := fmt.Errorf("%w: nothing is listening on admin socket", admin.ErrDaemonNotRunning)
	api := func(code int, msg string) error {
		return &admin.APIError{Method: "POST", Path: "/v1/x", StatusCode: code, Status: fmt.Sprintf("%d %s", code, http.StatusText(code)), Message: msg}
	}
	tests := []struct {
		name, program, socket string
		// inv is how obiectl was run; the command of program with the
		// default --timeout if it has no arguments.
		inv       invocation
		err       error
		id        string
		what, why string
		next      []string
	}{
		{name: "no socket", program: "obiectl peers", socket: missing, err: notRunning, id: "node-not-running",
			what: "obied is not running: there is no admin socket " + missing,
			why:  "the node was not started, has stopped, or uses another socket",
			next: []string{"start it: sudo systemctl start obied", "sudo journalctl -u obied -n 20", "obiectl --socket <path> peers"}},
		{name: "nothing answers", program: "obiectl status", socket: stale, err: notRunning, id: "node-not-running",
			what: "obied is not running: nothing answers on the admin socket " + stale,
			why:  "the node has stopped, or is just starting or stopping",
			next: []string{"start it: sudo systemctl start obied"}},
		{name: "socket permission", program: "obiectl status", socket: stale, err: fmt.Errorf("%w on admin socket", fs.ErrPermission),
			id:   "admin-permission-denied",
			what: "permission denied: user " + me + " may not use the admin socket " + stale,
			why:  "only root and members of the group " + group + " may control the node",
			next: []string{"with sudo in front of it", "sudo usermod -aG " + group + " " + me + ", then log out and in again"}},
		{name: "refused by the node", program: "obiectl status", socket: stale,
			err: api(http.StatusForbidden, `forbidden: uid 1000 is neither root nor a member of group "obie"`), id: "admin-permission-denied",
			what: "permission denied: user " + me, why: "only root and members of the group " + group},
		{name: "unknown socket group", program: "obiectl status", socket: missing, err: fs.ErrPermission, id: "admin-permission-denied",
			why: "only root and members of the group that owns the socket may control the node", next: []string{"join the group that owns " + missing}},
		{name: "timeout", program: "obiectl decisions", socket: stale, err: fmt.Errorf("get: %w", context.DeadlineExceeded), id: "node-timeout",
			what: "obied did not answer in time (--timeout)", why: "busy", next: []string{ctl + " --timeout 30s decisions", ctl + " --timeout 30s status"}},
		{name: "not a public address", program: "obiectl report", socket: stale,
			err: api(http.StatusUnprocessableEntity, "refused: ipv4:10.0.0.1 is not a public address: 10.0.0.1/32 overlaps special-purpose range 10.0.0.0/8; "+
				"OBIE never publishes internal or special-purpose addresses"),
			id: "address-protected", what: "nothing was reported: ipv4:10.0.0.1 is not a public address: 10.0.0.1/32 overlaps special-purpose range 10.0.0.0/8",
			why: "OBIE never reports private", next: []string{ctl + " explain <address> shows the rule that protects it"}},
		{name: "address from an example", program: "obiectl report", socket: stale,
			err: api(http.StatusUnprocessableEntity, "refused: ipv4:203.0.113.7 is not a public address: 203.0.113.7/32 overlaps special-purpose range 203.0.113.0/24; "+
				"OBIE never publishes internal or special-purpose addresses"),
			id: "address-protected", what: "nothing was reported: ipv4:203.0.113.7 is not a public address",
			why: "OBIE never reports private", next: []string{"reserved for examples", "the attacking address from your log"}},
		{name: "allow-listed", program: "obiectl report", socket: stale,
			err: api(http.StatusUnprocessableEntity, "refused: ipv4:85.20.0.1 overlaps the allow-listed network 85.20.0.0/16 (allowlist.cidrs); allow-listed addresses are never reported"),
			id:  "address-protected", what: "nothing was reported: ipv4:85.20.0.1 overlaps the allow-listed network 85.20.0.0/16 (allowlist.cidrs)",
			next: []string{"remove it from allowlist.cidrs and reload: sudo systemctl reload obied"}},
		{name: "nothing to revoke", program: "obiectl revoke", socket: stale,
			err: api(http.StatusNotFound, "not found: this node has no active verdict on ipv4:85.10.0.7"), id: "not-found",
			what: "this node has no active verdict on ipv4:85.10.0.7", next: []string{ctl + " indicators --mine"}},
		{name: "not found", program: "obiectl show", socket: stale, err: api(http.StatusNotFound, "no such thing"), id: "not-found",
			what: "no such thing", next: []string{ctl + " decisions"}},
		{name: "no override", program: "obiectl unoverride", socket: stale, err: fmt.Errorf("%w on 85.10.0.7", admin.ErrNoOverride),
			id: "no-override", what: "no override on 85.10.0.7", next: []string{ctl + " overrides"}},
		{name: "invalid request", program: "obiectl report", socket: stale, err: api(http.StatusBadRequest, "invalid request: ttl: must not be negative"),
			id: "request-invalid", what: "the node refused the request: ttl: must not be negative", next: []string{"obiectl report --help"}},
		{name: "unavailable", program: "obiectl explain", socket: stale, err: api(http.StatusServiceUnavailable, "the decision engine is not available"),
			id: "node-unavailable", what: "the decision engine is not available", why: "still starting", next: []string{ctl + " status"}},
		{name: "internal error", program: "obiectl report", socket: stale, err: api(http.StatusInternalServerError, "reporting failed; see the obied log"),
			id: "node-failed", what: "reporting failed; see the obied log", next: []string{"sudo journalctl -u obied -n 50"}},
		{name: "other status", program: "obiectl report", socket: stale, err: api(http.StatusTeapot, "tea"),
			id: "node-error", what: "the node answered 418 I'm a teapot: tea", next: []string{"journalctl"}},
		{name: "timeout repeats the arguments", program: "obiectl unoverride", socket: stale,
			inv: invocation{args: []string{"unoverride", "45.10.0.1"}, timeout: 5 * time.Second},
			err: context.DeadlineExceeded, id: "node-timeout", what: "obied did not answer in time",
			next: []string{"try again with more time: " + ctl + " --timeout 30s unoverride 45.10.0.1\n"}},
		{name: "timeout gives more than it had", program: "obiectl decisions", socket: stale,
			inv: invocation{args: []string{"decisions"}, timeout: time.Minute},
			err: context.DeadlineExceeded, id: "node-timeout", what: "obied did not answer in time",
			next: []string{"try again with more time: " + ctl + " --timeout 2m decisions\n"}},
		{name: "timeout pipes the evidence again", program: "obiectl report", socket: stale,
			inv: invocation{args: []string{"report", "--protocol", "ssh", "--reason", "password_bruteforce", "--evidence-file", "-", "45.10.0.1"}, timeout: requestTimeout},
			err: context.DeadlineExceeded, id: "node-timeout", what: "obied did not answer in time",
			next: []string{"try again with more time: <the command that printed the log lines> | " + ctl +
				" --timeout 30s report --protocol ssh --reason password_bruteforce --evidence-file - 45.10.0.1\n"}},
		{name: "default socket", program: "obiectl decisions", socket: config.Default().Admin.Socket, err: context.DeadlineExceeded,
			id: "node-timeout", what: "obied did not answer in time", next: []string{"try again with more time: sudo obiectl --timeout 30s decisions"}},
		{name: "other error", program: "obiectl status", socket: stale, err: errors.New("decode response: unexpected EOF"),
			id: "node-unreachable", what: "cannot talk to obied: decode response: unexpected EOF", next: []string{"sudo obied self-check"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := tt.inv
			if len(inv.args) == 0 {
				inv = invocationOf(context.Background(), tt.program)
			}
			p := clientProblem(tt.program, tt.socket, inv, tt.err)
			if p.id != tt.id || !strings.HasPrefix(p.what, tt.what) || !strings.Contains(p.why, tt.why) || len(p.next) == 0 {
				t.Errorf("problem = %+v, want id %s, what %q, why containing %q and a next step", p, tt.id, tt.what, tt.why)
			}
			next := strings.Join(p.next, "\n")
			for _, want := range tt.next {
				if !strings.Contains(next, want) {
					t.Errorf("next steps %q lack %q", p.next, want)
				}
			}
		})
	}
}

func TestInvocationReadsStdin(t *testing.T) {
	for args, want := range map[string]bool{
		"report --evidence-from-stdin 45.10.0.1":       true,
		"report -evidence-from-stdin=true 45.10.0.1":   true,
		"report --evidence-from-stdin=false 45.10.0.1": false,
		"report --evidence-file=- 45.10.0.1":           true,
		"report --evidence-file - 45.10.0.1":           true,
		"report --evidence-file auth.log 45.10.0.1":    false,
		"report -- --evidence-from-stdin":              false,
		"decisions":                                    false,
	} {
		if got := (invocation{args: strings.Fields(args)}).readsStdin(); got != want {
			t.Errorf("readsStdin(%s) = %v, want %v", args, got, want)
		}
	}
}

func TestDurationFlag(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "30s", 90 * time.Second: "1m30s", 2 * time.Minute: "2m", time.Hour: "1h", 90 * time.Minute: "1h30m",
	} {
		if got := durationFlag(d); got != want {
			t.Errorf("durationFlag(%s) = %q, want %q", d, got, want)
		}
	}
}

// staleSocket returns the path of a Unix socket that nothing listens on
// any more.
func staleSocket(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "stale.sock")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestObiectlAgainstStaleSocket runs obiectl against a socket nothing
// listens on: the node stopped without removing it.
func TestObiectlAgainstStaleSocket(t *testing.T) {
	socket := staleSocket(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := RunCtl([]string{"--socket", socket, "decisions"}, &stdout, &stderr); code != ExitFailure ||
		!strings.HasPrefix(stderr.String(), "obiectl decisions: obied is not running: nothing answers on the admin socket "+socket+"\n  Why:  ") {
		t.Errorf("exit code %d, stderr %q", code, stderr.String())
	}
}

// TestObiectlTimeoutRepeatsTheCommand runs obiectl against a node that
// never answers: the next step is the whole command again, with more time.
func TestObiectlTimeoutRepeatsTheCommand(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "hung.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			// Read the request, never answer, close when obiectl gives up.
			go func() {
				_, _ = io.Copy(io.Discard, conn)
				_ = conn.Close()
			}()
		}
	}()
	var stdout, stderr bytes.Buffer
	code := RunCtl([]string{"--socket", socket, "--timeout", "50ms", "block", "--note", "ssh abuse", "45.10.0.1"}, &stdout, &stderr)
	if code != ExitFailure || !strings.Contains(stderr.String(),
		"\n  Next: try again with more time: sudo obiectl --socket "+socket+" --timeout 30s block --note 'ssh abuse' 45.10.0.1\n") {
		t.Errorf("exit code %d, stderr %q", code, stderr.String())
	}
}
