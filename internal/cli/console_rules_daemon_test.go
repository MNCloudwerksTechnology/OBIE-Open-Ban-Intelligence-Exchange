package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConsoleRulesEndToEnd runs obied with the console on, an allow-list
// file and overrides, and follows the overrides, allow-list and
// configuration views through a file changed on disk, an allow-list file
// that goes missing, a rejected reload and a successful one (ADR 0024).
func TestConsoleRulesEndToEnd(t *testing.T) {
	addr := freeAddr(t)
	allowFile := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(allowFile, []byte("# partners\n185.0.1.0/24\n185.0.2.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n := newTestNodeWith(t, "  mode: observe\n", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n"+
		"allowlist:\n  cidrs: [185.0.3.0/24]\n  files: ["+allowFile+"]\n"+
		"console:\n  enabled: true\n  listen: "+addr+"\n")
	var logs syncBuffer
	reload := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &logs.buf, func(ctx context.Context, args []string) int {
		return runDaemonWith(ctx, reload, args, &bytes.Buffer{}, &logs)
	})
	ctl := func(args ...string) {
		t.Helper()
		var stderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", n.socket}, args...), io.Discard, &stderr); code != ExitOK {
			t.Fatalf("obiectl %s = %d: %s", strings.Join(args, " "), code, stderr.String())
		}
	}
	ctl("block", "--note", "oops", "10.0.0.5")
	ctl("allow", "--note", "partner", "185.0.9.0/24")
	ctl("block", "--ttl", "2h", "185.0.3.9")

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

	// AC1: the overrides, and why a force-block has no effect.
	wantIn("the overrides view", get("/overrides"),
		`<li><a href="/overrides" aria-current="page">In effect <span class="filter-count">3</span></a></li>`,
		`<a class="mono address" href="/decisions/10.0.0.5" data-copy>10.0.0.5</a>`,
		`<span class="no-effect">No effect: it is a protected address (built in: special-purpose addresses, 10.0.0.0/8), which not even an override blocks.</span>`,
		`<td><span class="cell-label">Note</span> oops</td>`,
		`<span class="override-kind" data-kind="allow">Always allow</span>`,
		`<td><span class="cell-label">Note</span> partner</td>`,
		`<td><span class="cell-label">Ends</span> Never</td>`,
		`<a class="mono address" href="/decisions/185.0.3.9" data-copy>185.0.3.9</a>`)

	// AC2, AC5: the allow-list by origin, with the file, and the lookup.
	wantIn("the allow-list view", get("/allowlist"),
		`<h2 id="group-builtin">Built-in ranges</h2>`,
		`<th scope="row">Loopback</th>`,
		`<tr><th scope="row"><span class="mono address" data-copy>127.0.0.1</span></th><td><span class="cell-label">From</span> <span class="mono">/ip4/127.0.0.1/tcp/0</span></td></tr>`,
		`<tr><th scope="row"><span class="mono address" data-copy>185.0.3.0/24</span></th><td><span class="cell-label">From</span> allowlist.cidrs</td></tr>`,
		`<h2 id="group-file-1">Allow-list file <code>`+allowFile+`</code></h2>`,
		`data-file-state="ok">2 entries loaded at `,
		`. Unchanged since it was loaded.</p>`,
		`<span class="mono">line 3</span>`)
	wantIn("the lookup of a protected address", get("/allowlist?address=10.0.0.5"),
		`<div class="lookup-answer" data-state="protected">`,
		`Yes: 10.0.0.5 is protected by built in: special-purpose addresses 10.0.0.0/8.`)
	wantIn("the lookup of a force-blocked address", get("/allowlist?address=185.0.3.9"),
		`<div class="lookup-answer" data-state="blocked">`,
		`No: your always-block override blocks 185.0.3.9, until `,
		`It overrules the allow-list entries that cover it.`,
		`<li><span class="mono">185.0.3.0/24</span> <span class="cell-note">allowlist.cidrs</span></li>`)
	wantIn("the lookup of a force-allowed address", get("/allowlist?address=185.0.9.1"),
		`Yes, by your always-allow override on 185.0.9.0/24: 185.0.9.1 is not blocked, whatever the verdicts, until you remove the override.`)

	// AC3, AC4: the configuration, the defaults marked.
	page := get("/configuration")
	wantIn("the configuration view", page,
		`<dd><code data-copy>`+n.config+`</code></dd>`,
		`, at start</dd>`,
		`The configuration was not reloaded since obied started.`,
		`The file on disk matches the running configuration.`,
		`<th scope="row"><code class="setting-key">node.mode</code><span class="cell-note">`,
		`<code class="value">observe</code>`,
		`<code class="value">info</code> <span class="default-mark">Default</span>`,
		`<ul class="value-list"><li><code>185.0.3.0/24</code></li></ul>`)
	if strings.Contains(page, `<code class="value">observe</code> <span class="default-mark">`) {
		t.Error("node.mode, which the file sets, is marked as a default")
	}

	// Edge case 1: the file changed on disk but is not reloaded.
	original, err := os.ReadFile(n.config)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(original), "  mode: observe\n", "  mode: enforce\n", 1) + "log:\n  level: debug\n"
	if err := os.WriteFile(n.config, []byte(changed), 0o600); err != nil { // #nosec G703 -- the test's own configuration file.
		t.Fatal(err)
	}
	wantIn("the file changed on disk", get("/configuration"),
		`The file on disk changed since it was loaded: 2 settings differ from the running configuration and are not active yet.`,
		`<strong>Not active until a reload:</strong> <code>node.mode</code>.`,
		`<strong>Waiting for a restart:</strong> <code>log.level</code>.`,
		`<span class="pending"><strong>Not active until a reload.</strong> On disk: <code class="value">enforce</code></span>`)

	// Edge case 3: the allow-list file goes missing.
	if err := os.Remove(allowFile); err != nil {
		t.Fatal(err)
	}
	wantIn("a missing allow-list file", get("/allowlist"),
		`<p class="callout" data-level="warning" data-file-state="warning">2 entries loaded at `,
		`It cannot be read now: allow-list file: open `+allowFile+`: no such file or directory. The entries loaded stay in effect`)

	// Edge case 2: the reload is rejected; the previous configuration stays.
	reload <- struct{}{}
	eventually(t, "the rejected reload", func() bool { return strings.Contains(get("/configuration"), "was rejected: allow-list: ") })
	wantIn("a rejected reload", get("/configuration"),
		`is still active.`,
		`<strong>Next step:</strong> Fix the file, check it, then reload obied again.`,
		`<span class="pending"><strong>Not active until a reload.</strong> On disk: <code class="value">enforce</code></span>`)

	// A reload with the file back applies node.mode; log.level waits.
	if err := os.WriteFile(allowFile, []byte("185.0.1.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reload <- struct{}{}
	eventually(t, "the reload", func() bool { return strings.Contains(get("/configuration"), "succeeded.") })
	page = get("/configuration")
	wantIn("after the reload", page,
		`, by a reload</dd>`,
		`<strong>Waiting for a restart:</strong> <code>log.level</code>.`,
		`<code class="value">enforce</code>`)
	if strings.Contains(page, "Not active until a reload") {
		t.Error("node.mode is still not active after the reload")
	}
	wantIn("the reloaded allow-list", get("/allowlist"), `data-file-state="ok">1 entry loaded at `)

	cancel()
	if code := waitExit(t, exit, &logs.buf); code != ExitOK {
		t.Fatalf("exit code = %d:\n%s", code, logs.String())
	}
}
