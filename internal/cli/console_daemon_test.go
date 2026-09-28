package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

// consoleOf runs obiectl console --json against n.
func consoleOf(t *testing.T, n testNode, extra ...string) admin.ConsoleResponse {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := append([]string{"--socket", n.socket, "console", "--json"}, extra...)
	if code := RunCtl(args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("obiectl console = %d: %s", code, stderr.String())
	}
	var resp admin.ConsoleResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// consoleBrowser is a browser on the node's host with its own cookies.
type consoleBrowser struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newConsoleBrowser(t *testing.T, addr string) *consoleBrowser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{DisableKeepAlives: true}
	return &consoleBrowser{t: t, base: "http://" + addr, client: &http.Client{Jar: jar, Transport: transport, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (b *consoleBrowser) do(method, path string, form url.Values) (int, string) {
	b.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, b.base+path, body)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

// eventually polls cond until it holds, failing the test after 10 s.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func listening(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		_ = c.Close()
	}
	return err == nil
}

// TestConsoleEndToEnd runs obied with the console off, switches it on and
// off by reloads while the node keeps running, signs a browser in with the
// token from obiectl console and rotates the token.
func TestConsoleEndToEnd(t *testing.T) {
	addr := freeAddr(t)
	mesh := "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n"
	n := newTestNodeWith(t, "", mesh+"console:\n  listen: "+addr+"\n")
	var logs syncBuffer
	reload := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &logs.buf, func(ctx context.Context, args []string) int {
		return runDaemonWith(ctx, reload, args, &bytes.Buffer{}, &logs)
	})

	// Off by default.
	if c := consoleOf(t, n); c.Enabled || c.URL != "" || c.Listen != addr || len(c.Token) != 43 {
		t.Fatalf("console at start = %+v", c)
	}
	if listening(addr) {
		t.Fatal("the console listens although it is off")
	}

	// Switched on by a reload, without restarting anything else.
	setConsole := func(enabled bool) {
		t.Helper()
		cfg, err := os.ReadFile(n.config)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(cfg), "console:\n  enabled: true\n", "console:\n", 1)
		if enabled {
			text = strings.Replace(text, "console:\n", "console:\n  enabled: true\n", 1)
		}
		if err := os.WriteFile(n.config, []byte(text), 0o600); err != nil { // #nosec G703 -- the test's own configuration file.
			t.Fatal(err)
		}
		reload <- struct{}{}
	}
	setConsole(true)
	eventually(t, "the console serves", func() bool { return consoleOf(t, n).URL == "http://"+addr+"/" })

	c := consoleOf(t, n)
	b := newConsoleBrowser(t, addr)
	if code, _ := b.do(http.MethodGet, "/", nil); code != http.StatusSeeOther {
		t.Errorf("GET / signed out = %d, want the sign-in redirect", code)
	}
	if code, _ := b.do(http.MethodPost, "/login", url.Values{"token": {c.Token}, "next": {"/"}}); code != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", code)
	}
	code, page := b.do(http.MethodGet, "/", nil)
	// A node without peers is degraded: the mesh says so.
	if code != http.StatusOK || !strings.Contains(page, `data-health data-state="degraded"`) ||
		!strings.Contains(page, "<li>mesh: 0 peers connected (0/0 bootstrap peers)</li>") ||
		!strings.Contains(page, `data-mode="observe"`) {
		t.Errorf("overview = %d:\n%s", code, page)
	}

	// A new token signs the browser out.
	if rotated := consoleOf(t, n, "--rotate"); rotated.Token == c.Token {
		t.Error("--rotate kept the token")
	}
	if code, _ := b.do(http.MethodGet, "/api/health", nil); code != http.StatusUnauthorized {
		t.Errorf("session after --rotate = %d, want 401", code)
	}

	// Switched off by a reload; the node keeps answering.
	setConsole(false)
	eventually(t, "the console stops", func() bool { return !listening(addr) })
	var status bytes.Buffer
	if code := RunCtl([]string{"--socket", n.socket, "status"}, &status, io.Discard); code != ExitOK ||
		!strings.Contains(status.String(), "console") || !strings.Contains(status.String(), "disabled") {
		t.Errorf("obiectl status after switching the console off = %d:\n%s", code, status.String())
	}

	cancel()
	if code := waitExit(t, exit, &logs.buf); code != ExitOK {
		t.Fatalf("exit code = %d:\n%s", code, logs.String())
	}
	lines := logLines(t, bytes.NewBufferString(logs.String()))
	// Registered first: it starts before and stops after everything else.
	var started, stopped []string
	for _, l := range lines {
		switch l["msg"] {
		case "subsystem started":
			started = append(started, fmt.Sprint(l["subsystem"]))
		case "subsystem stopped":
			stopped = append(stopped, fmt.Sprint(l["subsystem"]))
		}
	}
	if len(started) < 2 || started[0] != "console" || stopped[len(stopped)-1] != "console" {
		t.Errorf("start order %v, stop order %v; want the console first and last", started, stopped)
	}
	// The reloads touched nothing else: every subsystem started once.
	seen := map[string]bool{}
	for _, name := range started {
		if seen[name] {
			t.Errorf("subsystem %s started twice", name)
		}
		seen[name] = true
	}
	for _, msg := range []string{"console serving", "console stopped", "console sign-in",
		"console token rotated; every console session was signed out"} {
		if findLog(lines, "console", msg) == nil {
			t.Errorf("no console log line %q", msg)
		}
	}
	if strings.Contains(logs.String(), c.Token) {
		t.Error("the token was logged")
	}
}

// TestConsoleOverviewEndToEnd runs obied with the console on and reads its
// overview: the node's identity, mode and parts, the empty state of a node
// that has just started, and what the reloads recorded.
func TestConsoleOverviewEndToEnd(t *testing.T) {
	addr := freeAddr(t)
	n := newTestNodeWith(t, "", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\nconsole:\n  enabled: true\n  listen: "+addr+"\n")
	var logs syncBuffer
	reload := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &logs.buf, func(ctx context.Context, args []string) int {
		return runDaemonWith(ctx, reload, args, &bytes.Buffer{}, &logs)
	})

	var stdout bytes.Buffer
	if code := RunCtl([]string{"--socket", n.socket, "identity", "--json"}, &stdout, io.Discard); code != ExitOK {
		t.Fatalf("obiectl identity = %d", code)
	}
	var id admin.IdentityResponse
	if err := json.Unmarshal(stdout.Bytes(), &id); err != nil {
		t.Fatal(err)
	}
	b := newConsoleBrowser(t, addr)
	eventually(t, "the console serves", func() bool { return listening(addr) })
	if code, _ := b.do(http.MethodPost, "/login", url.Values{"token": {consoleOf(t, n).Token}, "next": {"/"}}); code != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", code)
	}

	// A node without peers or data that has just started says what will
	// come, once enforcement has made its first pass.
	fragment := func() string {
		code, body := b.do(http.MethodGet, "/api/overview", nil)
		if code != http.StatusOK {
			t.Fatalf("GET /api/overview = %d: %s", code, body)
		}
		return body
	}
	eventually(t, "the first enforcement pass", func() bool { return strings.Contains(fragment(), "<td>observing</td>") })
	code, page := b.do(http.MethodGet, "/", nil)
	for _, want := range []string{
		"<h1>Overview</h1>",
		`<code class="id">` + id.PeerID + "</code>",
		`<code class="id">` + strings.ReplaceAll(id.Fingerprint, "+", "&#43;") + "</code>",
		`<strong>Observe</strong>: The node decides and shows what it would block, but blocks nothing`,
		`, at start. <a href="/configuration">The configuration it runs with</a></dd>`,
		`<strong class="summary-title">Just started</strong>`,
		`<h2 id="starting-heading">This node has just started</h2>`,
		`<span class="number-label">Peers connected</span> <span class="number-value">None yet</span>`,
		`<p class="number-note">no peer is configured in mesh.bootstrap</p>`,
		`<span class="number-label">Indicators held</span> <span class="number-value">None yet</span>`,
		`<span class="number-label">Firewall entries</span> <span class="number-value">None</span>`,
		`<span class="number-label">Active overrides</span> <span class="number-value">0</span>`,
		`<a class="number-main" href="/peers"><span class="number-label">Peers connected</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("overview (%d) lacks %q:\n%s", code, want, page)
		}
	}
	if !strings.Contains(page, `<tr><th scope="row">Mesh</th><td><span class="part-state" data-state="waiting">Waiting for peers</span></td>`) {
		t.Error("overview does not show the mesh waiting for peers")
	}
	for _, name := range []string{"Store", "Decision engine", "Enforcement", "Admin interface"} {
		if !strings.Contains(page, `<tr><th scope="row">`+name+`</th><td><span class="part-state" data-state="ready">Ready</span></td>`) {
			t.Errorf("overview does not show %s ready", name)
		}
	}

	// A rejected reload is a condition; the node keeps its configuration.
	original, err := os.ReadFile(n.config)
	if err != nil {
		t.Fatal(err)
	}
	writeFile := func(text string) {
		t.Helper()
		if err := os.WriteFile(n.config, []byte(text), 0o600); err != nil { // #nosec G703 -- the test's own configuration file.
			t.Fatal(err)
		}
		reload <- struct{}{}
	}
	writeFile(string(original) + "decision:\n  quorum: 0\n")
	eventually(t, "the overview shows the rejected reload", func() bool {
		return strings.Contains(fragment(), "<span class=\"condition-level\">Warning:</span> The configuration reload at ")
	})

	// A later reload clears it, and names what waits for a restart.
	writeFile(string(original) + "log:\n  level: debug\n")
	eventually(t, "the overview shows the reload", func() bool {
		f := fragment()
		return strings.Contains(f, ", by a reload. <a href=\"/configuration\">") && !strings.Contains(f, "was rejected") &&
			strings.Contains(f, "<span class=\"condition-level\">Note:</span> Changes to log.level wait for a restart")
	})

	cancel()
	if code := waitExit(t, exit, &logs.buf); code != ExitOK {
		t.Fatalf("exit code = %d:\n%s", code, logs.String())
	}
}

// TestConsolePortInUse: the node starts and stays ready without the
// console, and says why.
func TestConsolePortInUse(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	n := newTestNodeWith(t, "", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\nconsole:\n  enabled: true\n  listen: "+taken.Addr().String()+"\n")
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &logs.buf, func(ctx context.Context, args []string) int {
		return runDaemonWith(ctx, nil, args, &bytes.Buffer{}, &logs)
	})

	c := consoleOf(t, n)
	if !c.Enabled || c.URL != "" || !strings.Contains(c.Error, "address already in use") {
		t.Errorf("console = %+v, want enabled, not serving, with the bind error", c)
	}
	resp, err := http.Get("http://" + n.metrics + "/readyz") // #nosec G107 -- test daemon URL.
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /readyz = %d, want 200: the console never makes the node unready", resp.StatusCode)
	}

	cancel()
	if code := waitExit(t, exit, &logs.buf); code != ExitOK {
		t.Fatalf("exit code = %d:\n%s", code, logs.String())
	}
	l := findLog(logLines(t, bytes.NewBufferString(logs.String())), "console", "console not started; the node runs without it")
	if l == nil || !strings.Contains(fmt.Sprint(l["error"]), "address already in use") {
		t.Errorf("no log line saying why the console did not start:\n%s", logs.String())
	}
}

// TestConsolePeersEndToEnd runs two nodes: B bootstraps to A, trusts it,
// and also lists an unreachable bootstrap peer C. A reports attacks; B's
// console shows both peers, the events A sent, the verdicts B holds from
// it and C as disconnected without influence (WP-1684).
func TestConsolePeersEndToEnd(t *testing.T) {
	meshA := freeAddr(t)
	host, port, err := net.SplitHostPort(meshA)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestNodeWith(t, "", fmt.Sprintf("mesh:\n  listen: [/ip4/%s/tcp/%s]\n", host, port))
	keyA, err := identity.Create(a.stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	keyC, err := identity.Create(filepath.Join(t.TempDir(), "c"), false)
	if err != nil {
		t.Fatal(err)
	}
	_, closedPort, err := net.SplitHostPort(freeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)
	b := newTestNodeWith(t, "", fmt.Sprintf(
		"mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n  bootstrap: [/ip4/%s/tcp/%s/p2p/%s, /ip4/127.0.0.1/tcp/%s/p2p/%s]\n"+
			"trust:\n  publishers:\n    - {peer_id: %s, name: alpha, weight: 0.6}\n"+
			"console:\n  enabled: true\n  listen: %s\n",
		host, port, keyA.PeerID(), closedPort, keyC.PeerID(), keyA.PeerID(), addr))

	var stderrA, logsB syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exitA := startDaemon(ctx, t, a, &stderrA.buf, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderrA)
	})
	exitB := startDaemon(ctx, t, b, &logsB.buf, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &logsB)
	})

	browser := newConsoleBrowser(t, addr)
	eventually(t, "B's console serves", func() bool { return listening(addr) })
	if code, _ := browser.do(http.MethodPost, "/login", url.Values{"token": {consoleOf(t, b).Token}, "next": {"/"}}); code != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", code)
	}
	peers := func() string {
		code, body := browser.do(http.MethodGet, "/api/peers", nil)
		if code != http.StatusOK {
			t.Fatalf("GET /api/peers = %d: %s", code, body)
		}
		return body
	}
	eventually(t, "B connects to A", func() bool { return strings.Contains(peers(), `data-state="ready">Connected</span>`) })

	// A reports attacks until one of its verdicts reaches B.
	held := regexp.MustCompile(`<span class="cell-note">(all count|counts) in decisions</span>`)
	for i := 1; !held.MatchString(peers()); i++ {
		if i > 50 {
			t.Fatalf("no verdict of A reached B:\n%s", peers())
		}
		var stderr bytes.Buffer
		if code := RunCtl([]string{"--socket", a.socket, "report", "--protocol", "ssh", "--reason", "password_bruteforce",
			fmt.Sprintf("85.10.20.%d", i)}, io.Discard, &stderr); code != ExitOK {
			t.Fatalf("obiectl report = %d: %s", code, stderr.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	eventually(t, "C's dial fails", func() bool { return strings.Contains(peers(), "last dial failed") })

	code, page := browser.do(http.MethodGet, "/peers", nil)
	for _, want := range []string{
		"<h1>Peers</h1>",
		`<li><a href="/peers" aria-current="page">All <span class="filter-count">2</span></a></li>`,
		`<a class="peer-name" href="/peers/` + keyA.PeerID() + `">alpha</a>`,
		`<span class="roles"><span class="role">Bootstrap peer</span><span class="role">Trusted publisher</span></span>`,
		`<li>/ip4/` + host + `/tcp/` + port + `</li>`,
		`<span class="weight">0.6</span>`,
		`<a class="peer-name" href="/peers/` + keyC.PeerID() + `">Unnamed peer</a>`,
		`<span class="peer-state" data-state="warning">Disconnected</span>`,
		`<span class="cell-note">not connected since obied started</span>`,
		`<li>/ip4/127.0.0.1/tcp/` + closedPort + `</li>`,
		`<span class="badge">No influence on decisions</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("peers view (%d) lacks %q:\n%s", code, want, page)
		}
	}
	if !regexp.MustCompile(`<span>[1-9][0-9]* accepted</span>`).MatchString(page) {
		t.Errorf("the peers view counts no accepted event of A:\n%s", page)
	}
	if _, untrusted := browser.do(http.MethodGet, "/peers?show=untrusted", nil); strings.Contains(untrusted, `href="/peers/`+keyA.PeerID()+`"`) ||
		!strings.Contains(untrusted, `href="/peers/`+keyC.PeerID()+`"`) {
		t.Errorf("the untrusted filter lists A or misses C:\n%s", untrusted)
	}

	code, page = browser.do(http.MethodGet, "/peers/"+keyA.PeerID(), nil)
	for _, want := range []string{
		"<h1>alpha</h1>",
		`<dd><strong>0.6</strong>, set in trust.publishers.</dd>`,
		`<h2 id="verdicts-heading">Verdicts held from this peer</h2>`,
		`<td><span class="cell-label">Reason</span> password_bruteforce (ssh)</td>`,
		`<td><span class="cell-label">Counts in decisions</span> <span>Yes</span></td>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("A's page (%d) lacks %q:\n%s", code, want, page)
		}
	}
	if !regexp.MustCompile(`<th scope="row"><a class="mono" href="/verdicts\?address=85\.10\.20\.[0-9]+">85\.10\.20\.[0-9]+</a></th>`).MatchString(page) {
		t.Errorf("A's page lists none of its verdicts:\n%s", page)
	}
	// The verdicts A published arrive in B's verdicts view as received
	// verdicts of a named, trusted publisher (ADR 0023).
	code, page = browser.do(http.MethodGet, "/verdicts?from=peers", nil)
	for _, want := range []string{
		`<h2 id="list-heading">Active verdicts received from other publishers</h2>`,
		`<a href="/peers/` + keyA.PeerID() + `">alpha</a>`,
		`<span class="cell-note">weight 0.6</span>`,
		`<td><span class="cell-label">Reason</span> password_bruteforce (ssh)</td>`,
		`<span class="cell-note">counts: Yes</span>`,
		`<a class="weight" href="/verdicts?publisher=` + keyA.PeerID() + `">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("B's received verdicts (%d) lack %q:\n%s", code, want, page)
		}
	}
	if code, _ := browser.do(http.MethodGet, "/peers/12D3KooWNoSuchPeer", nil); code != http.StatusNotFound {
		t.Errorf("GET /peers/<unknown> = %d, want 404", code)
	}

	cancel()
	for name, exit := range map[string]<-chan int{"A": exitA, "B": exitB} {
		if code := waitExit(t, exit, &logsB.buf); code != ExitOK {
			t.Errorf("obied %s exit code = %d", name, code)
		}
	}
}
