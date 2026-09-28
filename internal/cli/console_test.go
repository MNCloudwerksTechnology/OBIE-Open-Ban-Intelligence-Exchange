package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

func TestWriteConsoleTable(t *testing.T) {
	for name, tc := range map[string]struct {
		resp admin.ConsoleResponse
		want string
	}{
		"serving": {admin.ConsoleResponse{Enabled: true, Listen: "127.0.0.1:9465", URL: "http://127.0.0.1:9465/", Token: "tok"},
			"Console:  serving at http://127.0.0.1:9465/\nToken:    tok\n"},
		"disabled": {admin.ConsoleResponse{Listen: "127.0.0.1:9465", Token: "tok"},
			"Console:  disabled (console.enabled is false)\nToken:    tok\n"},
		"port taken": {admin.ConsoleResponse{Enabled: true, Listen: "127.0.0.1:9465", Error: "listen tcp 127.0.0.1:9465: bind: address already in use", Token: "tok"},
			"Console:  not serving: listen tcp 127.0.0.1:9465: bind: address already in use\nToken:    tok\n"},
		"not started": {admin.ConsoleResponse{Enabled: true, Listen: "127.0.0.1:9465", Token: "tok"},
			"Console:  enabled, not serving yet\nToken:    tok\n"},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeConsoleTable(&out, &tc.resp); err != nil || out.String() != tc.want {
				t.Errorf("table = %q, %v; want %q", out.String(), err, tc.want)
			}
		})
	}
}

func TestConsoleHint(t *testing.T) {
	for name, tc := range map[string]struct {
		resp admin.ConsoleResponse
		want string
	}{
		"serving": {admin.ConsoleResponse{Enabled: true, Listen: "127.0.0.1:9465", URL: "http://127.0.0.1:9465/"},
			"obiectl: open http://127.0.0.1:9465/ in a browser on this host and sign in with the token. " +
				"From another machine, forward the port first: ssh -L 9465:127.0.0.1:9465 <this host>, then open the same address there."},
		"ipv6": {admin.ConsoleResponse{Enabled: true, Listen: "[::1]:8443", URL: "http://[::1]:8443/"},
			"ssh -L 8443:[::1]:8443 <this host>"},
		"disabled":    {admin.ConsoleResponse{Listen: "127.0.0.1:9465"}, "set console.enabled: true in the configuration and reload obied (sudo systemctl reload obied)"},
		"not serving": {admin.ConsoleResponse{Enabled: true, Listen: "127.0.0.1:9465", Error: "bind"}, "fix the cause and reload obied"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := consoleHint(&tc.resp); !strings.Contains(got, tc.want) {
				t.Errorf("hint = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestConsoleUsage(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"argument":    {[]string{"console", "now"}, ExitUsage, `unexpected argument "now"`},
		"help":        {[]string{"console", "--help"}, ExitOK, "-rotate"},
		"not running": {[]string{"console"}, ExitFailure, "obied is not running"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--socket", "/nonexistent/obie.sock"}, tc.args...)
			if code := RunCtl(args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("exit code = %d, stderr %q; want %d containing %q", code, stderr.String(), tc.code, tc.stderr)
			}
		})
	}
}

// rotatingConsole hands out a new token on every rotation.
type rotatingConsole struct{ tokens []string }

func (c *rotatingConsole) Console() admin.ConsoleResponse {
	return admin.ConsoleResponse{Enabled: true, Listen: "127.0.0.1:9465", URL: "http://127.0.0.1:9465/", Token: c.tokens[len(c.tokens)-1]}
}

func (c *rotatingConsole) RotateConsoleToken() admin.ConsoleResponse {
	c.tokens = append(c.tokens, "token-"+strconv.Itoa(len(c.tokens)))
	return c.Console()
}

func TestObiectlConsoleShowsAndRotates(t *testing.T) {
	dir, err := os.MkdirTemp("", "obie")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "obie.sock")
	srv := admin.New(socket, "obie-test-no-such-group", admin.Info{Console: &rotatingConsole{tokens: []string{"token-0"}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	var stdout, stderr bytes.Buffer
	if code := RunCtl([]string{"--socket", socket, "console"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("obiectl console = %d: %s", code, stderr.String())
	}
	if stdout.String() != "Console:  serving at http://127.0.0.1:9465/\nToken:    token-0\n" ||
		!strings.Contains(stderr.String(), "ssh -L 9465:127.0.0.1:9465") {
		t.Errorf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := RunCtl([]string{"--socket", socket, "console", "--rotate", "--json"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("obiectl console --rotate = %d: %s", code, stderr.String())
	}
	var resp admin.ConsoleResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil || resp.Token != "token-1" {
		t.Errorf("rotated JSON = %s, %v; want token-1", stdout.String(), err)
	}
	if stderr.String() != "obiectl: issued a new console token; the old one no longer works and every browser must sign in again\n" {
		t.Errorf("stderr = %q", stderr.String())
	}
}
