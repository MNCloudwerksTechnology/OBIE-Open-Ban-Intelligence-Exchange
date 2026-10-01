package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestConsoleDecisionsEndToEnd runs obied in enforce mode with the
// console on, reports an address and a network, force-allows another
// address and follows them through the decisions list, the explanations
// and the firewall view (ADR 0022).
func TestConsoleDecisionsEndToEnd(t *testing.T) {
	addr := freeAddr(t)
	n := newTestNodeWith(t, "  mode: enforce\n", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n"+
		"console:\n  enabled: true\n  listen: "+addr+"\n")
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &logs.buf, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &logs)
	})
	ctl := func(args ...string) {
		t.Helper()
		var stderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", n.socket}, args...), io.Discard, &stderr); code != ExitOK {
			t.Fatalf("obiectl %s = %d: %s", strings.Join(args, " "), code, stderr.String())
		}
	}
	ctl("report", "--protocol", "ssh", "--reason", "password_bruteforce", "85.10.20.1")
	ctl("report", "--protocol", "http", "--reason", "http_probe", "85.10.30.0/24")
	ctl("report", "--protocol", "ssh", "--reason", "password_bruteforce", "85.10.20.9")
	ctl("allow", "85.10.20.9", "--note", "our monitoring")

	browser := newConsoleBrowser(t, addr)
	eventually(t, "the console serves", func() bool { return listening(addr) })
	if code, _ := browser.do(http.MethodPost, "/login", url.Values{"token": {consoleOf(t, n).Token}, "next": {"/"}}); code != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", code)
	}
	get := func(path string) string {
		t.Helper()
		code, body := browser.do(http.MethodGet, path, nil)
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, code, body)
		}
		return body
	}
	wantIn := func(what, page string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(page, w) {
				t.Errorf("%s lacks %q:\n%s", what, w, page)
			}
		}
	}

	// AC1, AC2: the list, once the firewall applied both blocks.
	eventually(t, "the firewall applies the blocks", func() bool {
		return strings.Count(get("/decisions?state=block&firewall=applied"), `<span class="peer-state" data-state="ready">Applied</span>`) == 2
	})
	page := get("/decisions?sort=address")
	wantIn("the decisions list", page,
		`<li><a href="/decisions?sort=address" aria-current="page">All <span class="filter-count">3</span></a></li>`,
		`<a class="mono address" href="/decisions/85.10.20.1" data-copy>85.10.20.1</a>`,
		`<a class="mono address" href="/decisions/85.10.30.0/24" data-copy>85.10.30.0/24</a>`,
		`<span class="cell-note">local autoblock</span>`,
		`<span class="reason">http_probe (http)</span>`,
		`<span class="decision-state" data-state="allowed">Allowed</span>`,
		`<span class="cell-note">operator force-allow</span>`,
		`<option value="password_bruteforce/ssh">password_bruteforce (ssh) (2)</option>`)
	page = get("/decisions?q=85.10.30.77")
	wantIn("a search for an address inside the network", page,
		`<a class="mono address" href="/decisions/85.10.30.0/24" data-copy>85.10.30.0/24</a>`,
		`<strong>No verdicts, not blocked</strong>`)
	if strings.Contains(page, `href="/decisions/85.10.20.1"`) {
		t.Errorf("the search lists an address outside it:\n%s", page)
	}

	// AC3: the explanation, like obiectl explain.
	page = get("/decisions/85.10.20.1")
	wantIn("the explanation of a block", page,
		`<strong class="summary-title">Blocked by this node&#39;s own verdict (local autoblock) until`,
		`<th scope="row">This node<span class="peer-id mono"`,
		`<span class="weight">1</span><span class="cell-note">trust.local_weight</span>`,
		`<td><span class="cell-label">Reason</span> password_bruteforce (ssh)</td>`,
		`<dd>Yes: this node&#39;s own ban verdict blocks it alone (decision.local_autoblock).</dd>`,
		`<span class="peer-state" data-state="ready">Applied</span> The firewall&#39;s own entry for it drops its traffic.`)
	page = get("/decisions/85.10.20.9")
	wantIn("the explanation of a force-allow", page,
		`<strong class="summary-title">Allowed: never blocked, whatever the verdicts</strong>`,
		`<dd><strong>Force-allow override</strong>:`,
		`<dd>our monitoring</dd>`)
	page = get("/decisions/85.10.30.77")
	wantIn("the explanation of an address inside the blocked network", page,
		`<strong class="summary-title">No verdicts, not blocked</strong>`,
		`It is not a block, yet the firewall drops its traffic: the entry for the wider network 85.10.30.0/24, a block, contains it and wins.`,
		`<a class="mono address" href="/decisions/85.10.30.0/24" data-copy>85.10.30.0/24</a> <span class="badge">Its entry wins</span>`)
	// AC4: an address the node knows nothing about.
	wantIn("the explanation of an unknown address", get("/decisions/85.10.99.1"),
		`<strong class="summary-title">No verdicts, not blocked</strong>`,
		`<dd>Not protected: no allow-list entry or override covers it.</dd>`)
	wantIn("the explanation of a protected address", get("/decisions/127.0.0.1"),
		`<dd>Protected: allow-listed:`)
	if code, _ := browser.do(http.MethodGet, "/decisions/10.0.0.0/8", nil); code != http.StatusNotFound {
		t.Errorf("GET /decisions/10.0.0.0/8 = %d, want 404", code)
	}

	// AC5: what the firewall applies, and no difference.
	page = get("/enforcement")
	wantIn("the firewall view", page,
		`<strong class="summary-title">2 entries applied for 2 decided blocks.</strong>`,
		`No difference: the firewall applies every decided block, and nothing else.`,
		`<a class="mono address" href="/decisions/85.10.20.1" data-copy>85.10.20.1</a>`,
		`<a class="mono address" href="/decisions/85.10.30.0/24" data-copy>85.10.30.0/24</a>`)

	cancel()
	if code := <-exit; code != ExitOK {
		t.Fatalf("obied exited with %d:\n%s", code, logs.String())
	}
}
