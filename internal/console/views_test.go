package console

import (
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"

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
	navLink  = regexp.MustCompile(`<a href="([^"]*)"( aria-current="page")?>([^<]*)</a>`)
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
		items = append(items, navItem{Path: l[1], Title: l[3], Current: l[2] != ""})
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
			if want := (navItem{Path: v.Path, Title: v.Title, Current: v.Path == current.Path}); nav[i] != want {
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
		if item.Current {
			t.Errorf("not-found page marks %s as current", item.Path)
		}
	}
}

func TestHomeView(t *testing.T) {
	c, b := signedInBrowser(t)
	mode := "enforce"
	c.node.Mode = func() string { return mode }
	_, page := b.get("/")
	for _, want := range []string{
		"<h1>This node</h1>",
		`<code class="id">` + testNode.PeerID + "</code>",
		"<dd>v0.1.0</dd>",
		"Enforce: blocks are applied by the enforcement backend.",
		`<span class="mono">12D3KooW…FhGyvd</span> · v0.1.0`,
		`<p class="mode" data-mode="enforce"><span class="visually-hidden">Mode: </span><span data-mode-label>Enforce</span></p>`,
		`<form method="post" action="/logout" class="signout">`,
		`<a class="skip-link" href="#main">Skip to content</a>`,
		`<main id="main" tabindex="-1">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("home page lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, c.Token()) {
		t.Error("a page shows the token")
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
	_, page := b.get("/")
	if strings.Contains(page, `<script>alert`) || strings.Contains(page, `<img src=x>`) {
		t.Errorf("node data not escaped:\n%s", page)
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
