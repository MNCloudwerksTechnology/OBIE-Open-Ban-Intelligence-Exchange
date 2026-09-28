package console

import (
	"errors"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// signedInBrowser starts a console and signs a browser in.
func signedInBrowser(t *testing.T) (*Console, *browser) {
	t.Helper()
	c, b := startConsole(t, &syncBuffer{})
	if resp, _ := b.signIn(c.Token(), "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", resp.StatusCode)
	}
	return c, b
}

var (
	navBlock = regexp.MustCompile(`(?s)<nav class="nav" aria-label="Console">(.*?)</nav>`)
	navLink  = regexp.MustCompile(`<a href="([^"]*)"(?: aria-current="(page|true)")?>([^<]*)</a>`)
)

// navOf returns the navigation links of a page: path, title and whether
// it is marked current.
func navOf(t *testing.T, page string) []navItem {
	t.Helper()
	m := navBlock.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("page without navigation:\n%s", page)
	}
	var items []navItem
	for _, l := range navLink.FindAllStringSubmatch(m[1], -1) {
		items = append(items, navItem{Path: l[1], Title: l[3], Current: l[2]})
	}
	return items
}

func TestNavigationListsExactlyTheViews(t *testing.T) {
	c, b := signedInBrowser(t)
	if len(c.pages) == 0 {
		t.Fatal("no views")
	}
	for _, current := range c.pages {
		resp, page := b.get(current.Path)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("GET %s = %d %s", current.Path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		nav := navOf(t, page)
		if len(nav) != len(c.pages) {
			t.Fatalf("%s: navigation %+v, want the %d views", current.Path, nav, len(c.pages))
		}
		for i, v := range c.pages {
			want := navItem{Path: v.Path, Title: v.Title}
			if v.Path == current.Path {
				want.Current = "page"
			}
			if nav[i] != want {
				t.Errorf("%s: navigation item %d = %+v, want %+v", current.Path, i, nav[i], want)
			}
		}
		if !strings.Contains(page, "<title>"+current.Title+" · OBIE console</title>") {
			t.Errorf("%s: page title missing", current.Path)
		}
	}

	resp, page := b.get("/no/such/view")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(page, "Page not found") ||
		!strings.Contains(page, "<code>/no/such/view</code>") {
		t.Errorf("unknown page = %d:\n%s", resp.StatusCode, page)
	}
	for _, item := range navOf(t, page) {
		if item.Current != "" {
			t.Errorf("not-found page marks %s as current", item.Path)
		}
	}
}

// overviewNode makes c show a node in enforce mode, three hours after its
// start, with peers, data and a capped enforcement.
func overviewNode(c *Console) {
	c.now = func() time.Time { return testNode.StartedAt.Add(3 * time.Hour) }
	c.node.Mode = func() string { return "enforce" }
	c.node.Status = func() []lifecycle.Status { return runningStatuses() }
	c.node.Facts = func() Facts {
		return Facts{
			Peers:     PeerFacts{Connected: 2, Bootstrap: 2, Configured: 3},
			Decisions: DecisionFacts{Block: 11, None: 1190, Allowed: 2, Indicators: 1203, Verdicts: 3410},
			Enforce:   EnforceFacts{Backend: "nftables", MaxEntries: 10, Mode: "enforce", Applied: 10, Blocks: 11, Capped: 1},
			Store:     StoreFacts{Overrides: 3, VerdictRecords: 3500, EventsAccepted: 40},
			Config:    ConfigFacts{LoadedAt: testNode.StartedAt},
		}
	}
}

// region matches the refreshing region of the overview page.
var region = regexp.MustCompile(`(?s)<div class="refresh" data-refresh="/api/overview">\n(.*)\n</div>\n\s*</main>`)

func TestOverviewPage(t *testing.T) {
	c, b := signedInBrowser(t)
	overviewNode(c)
	resp, page := b.get("/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d", resp.StatusCode)
	}
	for _, want := range []string{
		"<title>Overview · OBIE console</title>",
		"<h1>Overview</h1>",
		`<p class="lead">Is this node healthy, and what is it doing? <span data-live hidden>This page updates itself every 5 seconds.</span></p>`,
		`<div class="refresh" data-refresh="/api/overview">`,
		// AC5: when the data was read.
		`<p class="updated" data-tick>Updated <time datetime="2026-09-28T12:00:00Z">2026-09-28 12:00:00 UTC</time></p>`,
		// AC4: conditions with a next step.
		`<div class="summary" data-state="attention">`,
		`<li class="condition" data-level="warning">`,
		`<span class="condition-level">Warning:</span> 1 decided block is not applied: the firewall holds at most 10 entries (enforce.max_entries), and the lowest-score blocks are left out.`,
		`<p class="condition-next"><strong>Next step:</strong> Raise enforce.max_entries and restart obied, if the host can hold more entries.</p>`,
		// AC3: key numbers with their detail.
		`<a class="number-main" href="/peers"><span class="number-label">Peers connected</span> <span class="number-value">2</span></a>`,
		`<p class="number-note">2 of 3 configured connected</p>`,
		`<span class="number-label">Indicators held</span> <span class="number-value">1,203</span>`,
		`<span class="number-label">Decisions: block</span> <span class="number-value">11</span>`,
		`<span class="number-label">Decisions: none</span> <span class="number-value">1,190</span>`,
		`<span class="number-label">Decisions: allowed</span> <span class="number-value">2</span>`,
		`<span class="number-label">Firewall entries</span> <span class="number-value">10</span>`,
		`<p class="number-note">applied by nftables for 11 decided blocks: 1 over enforce.max_entries</p>`,
		`<span class="number-label">Active overrides</span> <span class="number-value">3</span>`,
		// AC2: the readiness of every part.
		`<tr><th scope="row">Mesh</th><td><span class="part-state" data-state="ready">Ready</span></td><td>2 peers connected (2/3 bootstrap peers)</td></tr>`,
		`<tr><th scope="row">Admin interface</th><td><span class="part-state" data-state="ready">Ready</span></td><td></td></tr>`,
		// AC1: identity, version, uptime, configuration, mode.
		`<dd><strong>Enforce</strong>: The node blocks what it decides to block: the nftables backend applies each block to the firewall until the decision expires.</dd>`,
		`<dd><code class="id">` + testNode.PeerID + `</code></dd>`,
		`<dd><code class="id">` + strings.ReplaceAll(testNode.Fingerprint, "+", "&#43;") + `</code></dd>`, // escaped, shown as +
		"<dd>v0.1.0</dd>",
		`<dd><span data-tick>3 h 0 min</span>, since <time datetime="2026-09-28T09:00:00Z">2026-09-28 09:00:00 UTC</time></dd>`,
		`<dd><time datetime="2026-09-28T09:00:00Z">2026-09-28 09:00:00 UTC</time>, at start</dd>`,
		// The shared layout.
		`<span class="mono">12D3KooW…FhGyvd</span> · v0.1.0`,
		`<p class="mode" data-mode="enforce"><span class="visually-hidden">Mode: </span><span data-mode-label>Enforce</span></p>`,
		`<form method="post" action="/logout" class="signout">`,
		`<a class="skip-link" href="#main">Skip to content</a>`,
		`<main id="main" tabindex="-1">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("overview lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, c.Token()) {
		t.Error("a page shows the token")
	}
}

// TestOverviewFragment: the script refreshes the region from a fragment
// endpoint that renders exactly what a reload would show there, behind
// the same session as every page.
func TestOverviewFragment(t *testing.T) {
	c, b := startConsole(t, &syncBuffer{})
	overviewNode(c)
	resp, body := b.get("/api/overview")
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Content-Type") != "application/json" ||
		!strings.Contains(body, `"error":"not signed in`) {
		t.Fatalf("GET /api/overview signed out = %d %s %s, want 401", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if resp, _ := b.signIn(c.Token(), "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", resp.StatusCode)
	}

	resp, fragment := b.get("/api/overview")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" ||
		resp.Header.Get("Content-Security-Policy") != contentSecurityPolicy || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("GET /api/overview = %d, headers %v", resp.StatusCode, resp.Header)
	}
	_, page := b.get("/")
	m := region.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no refreshing region in the page:\n%s", page)
	}
	if fragment != m[1] {
		t.Errorf("fragment differs from the page's region\nfragment:\n%s\nregion:\n%s", fragment, m[1])
	}
	if !strings.HasPrefix(fragment, `<div class="summary"`) || strings.Contains(fragment, "<nav") || strings.Contains(fragment, "<h1") {
		t.Errorf("fragment is more than the region:\n%s", fragment)
	}
	for _, v := range c.pages {
		// The decisions list's region carries when the list was read in its
		// query (ADR 0022).
		if _, page := b.get(v.Path); v.Fragment != "" && !strings.Contains(page, `data-refresh="`+v.Fragment+`"`) &&
			!strings.Contains(page, `data-refresh="`+v.Fragment+`?`) {
			t.Errorf("view %s does not mark its region with its fragment %s", v.Path, v.Fragment)
		}
	}

	// Other web pages cannot read it.
	if resp, _ := b.do(http.MethodGet, "/api/overview", nil, map[string]string{
		"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "cors", "Sec-Fetch-Dest": "empty"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("fetch from another port = %d, want 403", resp.StatusCode)
	}
}

// TestOverviewLinksOnlyToBuiltViews: a number links to its view once the
// console has it, and names the obiectl command until then.
func TestOverviewLinksOnlyToBuiltViews(t *testing.T) {
	c, b := signedInBrowser(t)
	overviewNode(c)
	_, page := b.get("/")
	for _, path := range []string{"/verdicts", "/overrides"} {
		if strings.Contains(page, `href="`+path) {
			t.Errorf("the overview links to %s, which the console does not serve", path)
		}
	}
	for _, cmd := range []string{"obiectl indicators", "obiectl overrides"} {
		if !strings.Contains(page, "Details: <code>"+cmd+"</code>") {
			t.Errorf("the overview does not name %q", cmd)
		}
	}
	// The peers view (ADR 0021), the decisions and the firewall view
	// (ADR 0022) exist.
	for _, want := range []string{
		`<a class="number-main" href="/peers"><span class="number-label">Peers connected</span>`,
		`<a class="number-main" href="/decisions?state=block">`,
		`<a class="number-main" href="/decisions?state=allowed">`,
		`<a class="number-main" href="/enforcement">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the overview lacks %q", want)
		}
	}
	for _, cmd := range []string{"obiectl peers", "obiectl decisions --state block", "obiectl enforced"} {
		if strings.Contains(page, "Details: <code>"+cmd+"</code>") {
			t.Errorf("the overview names %q of a view it links to", cmd)
		}
	}
}

// TestOverviewWithoutFacts: a node that passes no facts still gets a page.
func TestOverviewWithoutFacts(t *testing.T) {
	c, b := signedInBrowser(t)
	c.node.Facts = nil
	if resp, page := b.get("/"); resp.StatusCode != http.StatusOK || !strings.Contains(page, "<h1>Overview</h1>") {
		t.Errorf("GET / without facts = %d", resp.StatusCode)
	}
}

func TestEveryPageShowsTheHealth(t *testing.T) {
	c, b := signedInBrowser(t)
	running := lifecycle.Status{Name: "console", State: lifecycle.StateRunning, Ready: true}
	for name, tc := range map[string]struct {
		statuses []lifecycle.Status
		state    string
		label    string
		problems []string
	}{
		"starting": {[]lifecycle.Status{running, {Name: "store", State: lifecycle.StateStarting}}, HealthStarting, "Starting", nil},
		"ready":    {[]lifecycle.Status{running, {Name: "store", State: lifecycle.StateRunning, Ready: true}}, HealthReady, "Ready", nil},
		"degraded": {[]lifecycle.Status{running, {Name: "mesh", State: lifecycle.StateRunning, Ready: true, Detail: "degraded: 0 peers connected (0/1 bootstrap peers)"},
			{Name: "enforce", State: lifecycle.StateRunning, Error: "apply <nft> failed"}},
			HealthDegraded, "Degraded", []string{"mesh: 0 peers connected (0/1 bootstrap peers)", "enforce: apply &lt;nft&gt; failed"}},
		"shutting down": {[]lifecycle.Status{running, {Name: "store", State: lifecycle.StateStopping}}, HealthStopping, "Shutting down", nil},
	} {
		t.Run(name, func(t *testing.T) {
			c.node.Status = func() []lifecycle.Status { return tc.statuses }
			for _, path := range append(viewPaths(c), "/missing") {
				_, page := b.get(path)
				want := `<p class="health" data-health data-state="` + tc.state + `" role="status"><span class="health-dot" aria-hidden="true"></span><span class="visually-hidden">Node health: </span><span data-health-label>` + tc.label + `</span></p>`
				if !strings.Contains(page, want) {
					t.Errorf("%s: no health indicator %q:\n%s", path, want, page)
				}
				hidden := strings.Contains(page, `<div class="notice" data-health-problems hidden>`)
				if hidden != (len(tc.problems) == 0) {
					t.Errorf("%s: problems notice hidden %v, want %v", path, hidden, len(tc.problems) == 0)
				}
				for _, p := range tc.problems {
					if !strings.Contains(page, "<li>"+p+"</li>") {
						t.Errorf("%s: problem %q not listed", path, p)
					}
				}
			}
		})
	}
}

func viewPaths(c *Console) []string {
	var paths []string
	for _, v := range c.pages {
		paths = append(paths, v.Path)
	}
	return paths
}

func TestPagesEscapeNodeData(t *testing.T) {
	c, b := signedInBrowser(t)
	c.node.Version = `<script>alert("x")</script>`
	c.node.Mode = func() string { return `"><img src=x>` }
	c.node.Status = func() []lifecycle.Status {
		return []lifecycle.Status{{Name: partStore, State: lifecycle.StateRunning, Error: `<script>alert("store")</script>`}}
	}
	c.node.Facts = func() Facts {
		return Facts{
			Store:  StoreFacts{OverridesErr: errors.New(`<img src=x onerror=alert(1)>`)},
			Config: ConfigFacts{Rejected: `<script>alert("reload")</script>`, RestartKeys: []string{`<b>store</b>`}},
		}
	}
	for _, path := range []string{"/", "/api/overview"} {
		_, page := b.get(path)
		if strings.Contains(page, `<script>alert`) || strings.Contains(page, `<img src=x`) || strings.Contains(page, `<b>store`) {
			t.Errorf("%s: node data not escaped:\n%s", path, page)
		}
		if !strings.Contains(page, `&lt;script&gt;alert(&#34;reload&#34;)&lt;/script&gt;`) {
			t.Errorf("%s: the rejected reload is not shown:\n%s", path, page)
		}
	}
}

func TestAssets(t *testing.T) {
	_, b := startConsole(t, &syncBuffer{}) // assets need no session: the sign-in page uses them
	for path, contentType := range map[string]string{
		"/assets/console.css": "text/css; charset=utf-8",
		"/assets/console.js":  "text/javascript; charset=utf-8",
		"/assets/favicon.svg": "image/svg+xml",
	} {
		resp, body := b.get(path)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != contentType || body == "" {
			t.Errorf("GET %s = %d %q (%d bytes), want %s", path, resp.StatusCode, resp.Header.Get("Content-Type"), len(body), contentType)
		}
		if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("Content-Security-Policy") != contentSecurityPolicy {
			t.Errorf("GET %s: headers %v", path, resp.Header)
		}
	}
	for _, path := range []string{"/assets/", "/assets/missing.css", "/assets/../templates/layout.html"} {
		if resp, _ := b.get(path); resp.StatusCode == http.StatusOK {
			t.Errorf("GET %s = 200, want no content", path)
		}
	}
}

// externalRef matches a reference to another origin: an absolute or
// protocol-relative URL in an attribute, a CSS url() or @import, or a
// script's fetch.
var externalRef = regexp.MustCompile(`(?i)((src|href|action|content)\s*=\s*["']?\s*(https?:)?//)|url\(\s*["']?\s*(https?:)?//|@import|fetch\(\s*["'](https?:)?//|https?://`)

// xmlns declares SVG's namespace; it is a name, not something loaded.
var xmlns = regexp.MustCompile(`xmlns="http://www\.w3\.org/2000/svg"`)

func TestNothingLoadsFromOutsideTheNode(t *testing.T) {
	for _, files := range []fs.FS{assetFiles, templateFiles} {
		err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(files, path)
			if err != nil {
				return err
			}
			if m := externalRef.FindString(xmlns.ReplaceAllString(string(data), "")); m != "" {
				t.Errorf("%s refers to another origin: %q", path, m)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(contentSecurityPolicy, "http") || strings.Contains(contentSecurityPolicy, "*") ||
		strings.Contains(contentSecurityPolicy, "unsafe") {
		t.Errorf("the CSP allows more than the console's own origin: %s", contentSecurityPolicy)
	}
}

// inlineCode matches script or style inside a page: a script element
// without src, a style element or attribute, an event handler attribute or
// a javascript: URL. The CSP would block them.
var inlineCode = regexp.MustCompile(`(?i)<script(\s[^>]*)?>\s*[^<\s]|<script(?:(?:\s(?:type|defer)(?:="[^"]*")?)*)>|<style|\sstyle\s*=|\son[a-z]+\s*=|javascript:`)

func TestNoInlineScriptOrStyle(t *testing.T) {
	err := fs.WalkDir(templateFiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(templateFiles, path)
		if err != nil {
			return err
		}
		if m := inlineCode.FindString(string(data)); m != "" {
			t.Errorf("%s has inline code, which the CSP blocks: %q", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// markupFromString matches the ways a script turns a string into markup
// or code in the page.
var markupFromString = regexp.MustCompile(`innerHTML\s*=[^=]|outerHTML|insertAdjacentHTML|document\.write|\beval\(|new Function|createContextualFragment|setTimeout\(\s*['"]`)

// TestScriptInsertsOnlyInertFragments: the script refreshes regions with
// nodes from an inert document that DOMParser built from the console's
// own fragment, and turns no string into markup otherwise (ADR 0020).
func TestScriptInsertsOnlyInertFragments(t *testing.T) {
	script, err := fs.ReadFile(assetFiles, "assets/console.js")
	if err != nil {
		t.Fatal(err)
	}
	if m := markupFromString.FindString(string(script)); m != "" {
		t.Errorf("console.js turns a string into markup: %q", m)
	}
	for _, want := range []string{"new DOMParser().parseFromString(html, 'text/html')", "getAttribute('data-refresh')",
		"credentials: 'same-origin'", "document.hidden", "querySelectorAll('[data-tick]')", "preventScroll: true",
		// A filter and a column heading may share an href: the focused
		// occurrence is restored (ADR 0021).
		"linksTo(region, href).indexOf(el)", "links[focused.index]"} {
		if !strings.Contains(string(script), want) {
			t.Errorf("console.js lacks %q", want)
		}
	}
	for _, s := range []string{`el.innerHTML = s`, `el.outerHTML`, `el.insertAdjacentHTML('beforeend', s)`, `document.write(s)`,
		`eval(s)`, `setTimeout('go()', 1)`} {
		if !markupFromString.MatchString(s) {
			t.Errorf("markupFromString misses %q", s)
		}
	}
	if markupFromString.MatchString(`return copy.innerHTML.trim();`) || markupFromString.MatchString(`a.innerHTML === b`) {
		t.Error("markupFromString flags reading innerHTML")
	}
}

// TestDetectorsCatchViolations keeps the two checks above from passing
// vacuously.
func TestDetectorsCatchViolations(t *testing.T) {
	for _, s := range []string{
		`<link rel="stylesheet" href="https://cdn.example/x.css">`,
		`<script src="//cdn.example/x.js"></script>`,
		`body { background: url(//cdn.example/x.png) }`,
		`@import "fonts.css";`,
		`fetch('https://telemetry.example/')`,
	} {
		if !externalRef.MatchString(s) {
			t.Errorf("externalRef misses %q", s)
		}
	}
	for _, s := range []string{`<link href="/assets/console.css">`, `<a href="/">`, `url(/assets/x.svg)`} {
		if externalRef.MatchString(s) {
			t.Errorf("externalRef flags the local %q", s)
		}
	}
	for _, s := range []string{
		`<script>alert(1)</script>`,
		`<script type="module">`,
		`<style>body{}</style>`,
		`<div style="color:red">`,
		`<button onclick="go()">`,
		`<a href="javascript:go()">`,
	} {
		if !inlineCode.MatchString(s) {
			t.Errorf("inlineCode misses %q", s)
		}
	}
	if inlineCode.MatchString(`<script src="/assets/console.js" defer></script>`) {
		t.Error("inlineCode flags the external script")
	}
}
