//go:build sandbox

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// checkProject names the sandbox of the check, so that it runs next to
	// a sandbox of the user's.
	checkProject = "obie-sandbox-check"
	// settle is how long a reading command is repeated until its output is
	// what the page shows: verdicts, decisions and the firewall follow a
	// change within a few seconds.
	settle = 30 * time.Second
	// upBound bounds ./sandbox up, which builds the images the first time.
	upBound = 10 * time.Minute
	// stepBound bounds every other command.
	stepBound = 2 * time.Minute
)

// consoleLine is a console of the ./sandbox up output: node, address, token.
var consoleLine = regexp.MustCompile(`(?m)^(node\d)\s+(http://127\.0\.0\.1:(\d+)/)\s+token\s+(\S+)$`)

// console is the web console of one node of the running sandbox.
type console struct {
	base   string
	token  string
	client *http.Client
}

// TestWalkthrough runs documentation/sandbox.md: every step in order
// against a sandbox of its own, the output compared with what the page
// shows, every console page checked for what the page says it shows, and
// after ./sandbox down nothing of the sandbox left (ADR 0029).
func TestWalkthrough(t *testing.T) {
	w := parseWalkthrough(readRepoFile(t, walkthroughPath))
	if len(w.problems) > 0 {
		t.Fatalf("%s is not in the form the check reads: %q", walkthroughPath, w.problems)
	}
	dir, err := filepath.Abs(sandboxDir)
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	env := append(os.Environ(), "OBIE_SANDBOX_PROJECT="+checkProject, "OBIE_SANDBOX_PORT="+strconv.Itoa(port))
	down := false
	t.Cleanup(func() {
		if !down {
			out, _ := command(dir, env, "./sandbox down", stepBound)
			t.Logf("removed the sandbox after a failure:\n%s", out.stderr)
		}
	})

	// The nodes of the consoles the page links, by port, and the consoles
	// of the check's sandbox, by node.
	pageNodes := map[int]string{}
	consoles := map[string]*console{}
	next := 0 // the next console hint
	for i, s := range w.steps {
		t.Logf("line %d: %s", s.line, s.command)
		got := runStep(t, dir, env, s)
		switch s.command {
		case "./sandbox up":
			pageNodes = consolePorts(t, s.want)
			consoles = startedConsoles(t, got)
			checkIsolation(t)
		case "./sandbox down":
			down = true
		}
		for ; next < len(w.hints) && w.hints[next].after == i+1; next++ {
			h := w.hints[next]
			node, ok := pageNodes[h.port]
			if !ok || consoles[node] == nil {
				t.Fatalf("line %d: the console hint links port %d, which is no console of ./sandbox up", h.line, h.port)
			}
			t.Logf("line %d: %s's console page %s", h.line, node, h.path)
			consoles[node].check(t, h)
		}
	}
	if next != len(w.hints) {
		t.Errorf("%d console hints come after the last step and were not checked", len(w.hints)-next)
	}
	checkRemoved(t)
}

// output is what a command printed.
type output struct{ stdout, stderr string }

// command runs a command of the walkthrough in the sandbox directory.
func command(dir string, env []string, cmdline string, bound time.Duration) (output, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", cmdline) // #nosec G204 -- a command of the walkthrough.
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return output{stdout.String(), stderr.String()}, err
}

// runStep runs a step until its output is what the page shows: a command
// that changes something once, a reading one repeatedly for up to settle.
// Only ./sandbox up and down may print to standard error (Docker's
// progress); anything else a command prints there the page would have to
// show.
func runStep(t *testing.T, dir string, env []string, s step) string {
	t.Helper()
	bound := stepBound
	if s.command == "./sandbox up" {
		bound = upBound
	}
	deadline := time.Now().Add(settle)
	for {
		out, err := command(dir, env, s.command, bound)
		quiet := out.stderr == "" || s.command == "./sandbox up" || s.command == "./sandbox down"
		if err == nil && quiet && normalize(out.stdout) == normalize(s.want) {
			return out.stdout
		}
		if changesSomething(s.command) || time.Now().After(deadline) {
			t.Fatalf("line %d: %s: %v\nit printed:\n%s\n%s\nthe page shows:\n%s\n\ncompared as:\n%s\n---\n%s",
				s.line, s.command, err, out.stdout, out.stderr, s.want, normalize(out.stdout), normalize(s.want))
		}
		time.Sleep(time.Second)
	}
}

// consolePorts returns the node of each console port of the ./sandbox up
// output the page shows.
func consolePorts(t *testing.T, want string) map[int]string {
	t.Helper()
	nodes := map[int]string{}
	for _, m := range consoleLine.FindAllStringSubmatch(want, -1) {
		port, _ := strconv.Atoi(m[3])
		nodes[port] = m[1]
	}
	if len(nodes) != len(trustedNodes) {
		t.Fatalf("the page's ./sandbox up output lists %d consoles, want %d", len(nodes), len(trustedNodes))
	}
	return nodes
}

// startedConsoles returns the consoles ./sandbox up printed.
func startedConsoles(t *testing.T, out string) map[string]*console {
	t.Helper()
	consoles := map[string]*console{}
	for _, m := range consoleLine.FindAllStringSubmatch(out, -1) {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		consoles[m[1]] = &console{base: strings.TrimSuffix(m[2], "/"), token: m[4],
			client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	}
	return consoles
}

// check signs in to the console, if it has not yet, and waits until the
// page of the hint shows every phrase the hint sets in bold.
func (c *console) check(t *testing.T, h consoleHint) {
	t.Helper()
	deadline := time.Now().Add(settle)
	for {
		text, err := c.page(h.path)
		missing := slices.DeleteFunc(slices.Clone(h.phrases), func(p string) bool { return strings.Contains(text, p) })
		if err == nil && len(missing) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("line %d: the console page %s: %v; it does not show %q:\n%s", h.line, h.path, err, missing, text)
		}
		time.Sleep(time.Second)
	}
}

// page returns the text of a console page, signed in with the token.
func (c *console) page(path string) (string, error) {
	resp, err := c.client.Get(c.base + path)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(resp.Request.URL.Path, "/login") {
		if err := c.signIn(); err != nil {
			return "", err
		}
		return "", errors.New("signed in; reading the page again")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %s", resp.Status)
	}
	return pageText(string(body)), nil
}

// signIn signs in to the console as its sign-in page does.
func (c *console) signIn() error {
	req, err := http.NewRequest(http.MethodPost, c.base+"/login", strings.NewReader(url.Values{"token": {c.token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", c.base)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if strings.HasPrefix(resp.Request.URL.Path, "/login") {
		return fmt.Errorf("signing in with the token of ./sandbox up failed: %s", resp.Status)
	}
	return nil
}

var (
	scriptOrStyle = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	htmlTag       = regexp.MustCompile(`<[^>]*>`)
)

// pageText returns the text of an HTML page, whitespace collapsed.
func pageText(page string) string {
	page = scriptOrStyle.ReplaceAllString(page, " ")
	page = htmlTag.ReplaceAllString(page, " ")
	return strings.Join(strings.Fields(html.UnescapeString(page)), " ")
}

// docker runs a docker command for the checks around the walkthrough.
func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput() // #nosec G204 -- fixed docker commands.
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

var projectFilter = "label=com.docker.compose.project=" + checkProject

// checkIsolation checks every container of the running sandbox: none is
// privileged, shares a namespace of the host, mounts a host path or
// device, or keeps a capability beyond what the init container needs to
// hand files to nonroot.
func checkIsolation(t *testing.T) {
	t.Helper()
	ids := strings.Fields(docker(t, "ps", "-aq", "--filter", projectFilter))
	if len(ids) == 0 {
		t.Fatal("the sandbox has no containers")
	}
	var containers []struct {
		Name       string
		HostConfig struct {
			Privileged   bool
			NetworkMode  string
			PidMode      string
			IpcMode      string
			UsernsMode   string
			CgroupnsMode string
			Devices      []json.RawMessage
			CapAdd       []string
			CapDrop      []string
		}
		Mounts []struct{ Type, Source string }
	}
	if err := json.Unmarshal([]byte(docker(t, append([]string{"inspect"}, ids...)...)), &containers); err != nil {
		t.Fatal(err)
	}
	for _, c := range containers {
		hc := c.HostConfig
		if hc.Privileged || hc.NetworkMode == "host" || !slices.Contains(hc.CapDrop, "ALL") {
			t.Errorf("%s: privileged %v, network %s, dropped capabilities %q", c.Name, hc.Privileged, hc.NetworkMode, hc.CapDrop)
		}
		if hc.PidMode == "host" || hc.IpcMode == "host" || hc.UsernsMode == "host" || hc.CgroupnsMode == "host" || len(hc.Devices) > 0 {
			t.Errorf("%s: pid %q, ipc %q, userns %q, cgroupns %q, %d devices; want no namespace and no device of the host",
				c.Name, hc.PidMode, hc.IpcMode, hc.UsernsMode, hc.CgroupnsMode, len(hc.Devices))
		}
		for _, m := range c.Mounts {
			if m.Type != "volume" {
				t.Errorf("%s mounts %s %s; want named volumes only", c.Name, m.Type, m.Source)
			}
		}
		for _, capability := range hc.CapAdd {
			if !slices.Contains(initCaps, strings.TrimPrefix(capability, "CAP_")) {
				t.Errorf("%s keeps the capability %s", c.Name, capability)
			}
		}
	}
}

// checkRemoved checks that ./sandbox down left nothing of the sandbox.
func checkRemoved(t *testing.T) {
	t.Helper()
	for _, kind := range []string{"container", "volume", "network"} {
		args := []string{kind, "ls", "-q", "--filter", projectFilter}
		if kind == "container" {
			args = []string{"ps", "-aq", "--filter", projectFilter}
		}
		if left := strings.TrimSpace(docker(t, args...)); left != "" {
			t.Errorf("./sandbox down left %ss: %s", kind, left)
		}
	}
	if left := strings.TrimSpace(docker(t, "images", "-q", checkProject)); left != "" {
		t.Errorf("./sandbox down left the images %s", left)
	}
}
