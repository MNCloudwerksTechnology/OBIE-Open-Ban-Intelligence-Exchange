package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConsoleVerdictsEndToEnd runs obied with the console on, reports two
// addresses, one with evidence, revokes one, and follows them through the
// verdicts view: this node's active and revoked verdicts, the totals and
// the verdicts on one address (ADR 0023).
func TestConsoleVerdictsEndToEnd(t *testing.T) {
	addr := freeAddr(t)
	n := newTestNodeWith(t, "", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n"+
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
	lines := "sshd[1]: Failed password for root from 85.10.20.1\nsshd[2]: Failed password for root from 85.10.20.1"
	evidence := filepath.Join(t.TempDir(), "evidence.log")
	if err := os.WriteFile(evidence, []byte(lines+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(lines))
	logHash := "sha256:" + hex.EncodeToString(sum[:])
	ctl("report", "--protocol", "ssh", "--reason", "password_bruteforce", "--events", "5", "--evidence-file", evidence, "85.10.20.1")
	ctl("report", "--protocol", "http", "--reason", "http_probe", "85.10.20.2")
	ctl("revoke", "--reason", "test_traffic", "85.10.20.2")

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

	// AC1, AC5: this node's verdicts, with the evidence's hash and count,
	// and the totals.
	eventually(t, "the engine holds the verdict", func() bool {
		return strings.Contains(get("/verdicts?from=mine"), `data-copy>85.10.20.1</a>`)
	})
	page := get("/verdicts?from=mine")
	wantIn("this node's verdicts", page,
		`<h2 id="list-heading">Active verdicts of this node</h2>`,
		`<th scope="row"><a class="mono address" href="/verdicts?address=85.10.20.1" data-copy>85.10.20.1</a></th>`,
		`<span class="peer-name">This node</span>`,
		`<td><span class="cell-label">Reason</span> password_bruteforce (ssh)</td>`,
		`<span class="cell-note">5 events</span>`,
		`<code class="hash" data-copy>`+logHash+`</code>`,
		`<span class="cell-note">counts: Yes</span>`,
		`<a href="/decisions/85.10.20.1"><span class="decision-state" data-state="block">Block</span></a>`,
		`<li><a href="/verdicts?from=mine&amp;state=revoked">Revoked <span class="filter-count">1</span></a></li>`,
		`<td><span class="cell-label">Active</span> <a class="weight" href="/verdicts?from=mine">1</a>`,
		`<td><span class="cell-label">Revoked</span> <a class="weight" href="/verdicts?from=mine&amp;state=revoked">1</a></td>`)
	if strings.Contains(page, "Failed password") {
		t.Errorf("the evidence itself is shown:\n%s", page)
	}

	// AC1: the revoked verdict, and why.
	wantIn("this node's revoked verdicts", get("/verdicts?from=mine&state=revoked"),
		`<a class="mono address" href="/verdicts?address=85.10.20.2" data-copy>85.10.20.2</a>`,
		`<span class="verdict-state" data-state="revoked">Revoked</span>`,
		`<span class="cell-note">why: test_traffic</span>`,
		`<td><span class="cell-label">Reason</span> http_probe (http)</td>`)

	// AC3: every verdict on one address.
	wantIn("the verdicts on one address", get("/verdicts?address=85.10.20.1"),
		`<h2 id="list-heading">Active verdicts of every publisher on 85.10.20.1</h2>`,
		`<code>sudo obiectl show 85.10.20.1</code>`,
		`<code class="hash" data-copy>`+logHash+`</code>`)
	if page := get("/verdicts?address=85.10.20.2"); strings.Contains(page, `<tr class="verdict-main"`) {
		t.Errorf("the revoked verdict is listed as active:\n%s", page)
	}

	cancel()
	if code := <-exit; code != ExitOK {
		t.Fatalf("obied exited with %d:\n%s", code, logs.String())
	}
}
