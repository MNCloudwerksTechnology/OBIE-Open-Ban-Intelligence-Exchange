package console

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// fakeRules is a RuleSource with fixed data that records the lookups.
type fakeRules struct {
	active, expired       []Override
	activeErr, expiredErr error
	allow                 Allowlist
	protectionErr         error
	cfg                   Configuration

	mu     sync.Mutex
	lookup []netip.Prefix
}

func (f *fakeRules) Overrides(expired bool) ([]Override, error) {
	if expired {
		return append([]Override(nil), f.expired...), f.expiredErr
	}
	return append([]Override(nil), f.active...), f.activeErr
}

func (f *fakeRules) OverrideRetention() time.Duration { return 7 * 24 * time.Hour }

func (f *fakeRules) Allowlist() Allowlist { return f.allow }

// Protection finds the protected entry covering p, else reports none.
func (f *fakeRules) Protection(p netip.Prefix) (Protection, error) {
	f.mu.Lock()
	f.lookup = append(f.lookup, p)
	f.mu.Unlock()
	if f.protectionErr != nil {
		return Protection{}, f.protectionErr
	}
	pr := Protection{Range: p, Decidable: p.Bits() >= 16}
	for _, e := range f.allow.Entries {
		if e.Range.Overlaps(p) {
			pr.Overlapping = append(pr.Overlapping, e)
			if pr.Ruling.Rule == "" {
				pr.Ruling = Ruling{Effect: "allow", Rule: ruleAllowlist, Source: e.Source, Protected: sourceProtected(e.Source),
					Match: e.Range.String(), Label: e.Label}
			}
		}
	}
	return pr, nil
}

func (f *fakeRules) Configuration() Configuration { return f.cfg }

// rulesNode makes c show fixed overrides, allow-list and configuration.
func rulesNode(c *Console) *fakeRules {
	src := &fakeRules{
		active:  testOverrides(),
		expired: []Override{{Range: netip.MustParsePrefix("203.0.113.9/32"), Action: ruleForceBlock, Note: "old", CreatedAt: ruleNow.Add(-48 * time.Hour), ExpiresAt: ruleNow.Add(-24 * time.Hour)}},
		allow:   testAllowlist(),
		cfg: Configuration{Path: "/etc/obie/obie.yaml", Load: ConfigFacts{LoadedAt: ruleNow.Add(-time.Hour)},
			Settings: testSettings()},
	}
	c.now = func() time.Time { return ruleNow }
	c.node.Status = func() []lifecycle.Status { return runningStatuses() }
	c.node.Rules = src
	return src
}

// TestOverridesPage: every override in effect with its rule, note, set and
// end time and a link to its decision; a force-block without effect says
// why; expired ones are listed on request (AC1).
func TestOverridesPage(t *testing.T) {
	c, b := signedInBrowser(t)
	rulesNode(c)
	resp, page := b.get("/overrides")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /overrides = %d", resp.StatusCode)
	}
	wantAll(t, "the overrides view", page,
		"<title>Overrides · OBIE console</title>",
		`<a href="/overrides" aria-current="page">Overrides</a>`,
		`<h2 id="list-heading">Overrides in effect</h2>`,
		`<form class="filter-form" method="get" action="/overrides" role="search" aria-label="Filter overrides">`,
		`<option value="block">Always block</option>`,
		`<li><a href="/overrides" aria-current="page">In effect <span class="filter-count">3</span></a></li>`,
		`<li><a href="/overrides?state=expired">Expired <span class="filter-count">1</span></a></li>`,
		`<th scope="row"><a class="mono address" href="/decisions/198.51.100.0/24" data-copy>198.51.100.0/24</a></th>`,
		`<td><span class="cell-label">Rule</span> <span class="override-kind" data-kind="allow">Always allow</span></td>`,
		`<td><span class="cell-label">Note</span> partner</td>`,
		`<td><span class="cell-label">Ends</span> Never</td>`,
		`<td><span class="cell-label">Rule</span> <span class="override-kind" data-kind="block">Always block</span></td>`,
		`<td><span class="cell-label">Set</span> <time datetime="2026-09-28T10:00:00Z"><span class="day">2026-09-28</span> 10:00:00 UTC</time></td>`,
		`<td><span class="cell-label">Ends</span> <time datetime="2026-09-29T10:00:00Z"><span class="day">2026-09-29</span> 10:00:00 UTC</time></td>`,
		`<span class="no-effect">No effect: it is protected by the built-in range 192.168.0.0/16 (Private), which not even an override blocks.</span>`,
		`<td><span class="cell-label">Note</span> <span class="cell-note">No note</span></td>`,
		`<a href="/allowlist">Is an address protected?</a>`)
	if strings.Contains(page, "data-refresh") {
		t.Error("the overrides view refreshes itself; it is read when the page opens")
	}

	_, page = b.get("/overrides?state=expired&kind=block&address=203.0.113.0/24")
	wantAll(t, "the expired overrides", page,
		`<h2 id="list-heading">Expired always-block overrides on or around 203.0.113.0/24</h2>`,
		`<li><a href="/overrides?address=203.0.113.0%2F24&amp;kind=block&amp;state=expired" aria-current="page">Expired <span class="filter-count">1</span></a></li>`,
		`<p class="callout">Overrides that reached their expiry: they do nothing any more. The node keeps them for 7 days after their expiry, then forgets them.</p>`,
		`<input type="hidden" name="state" value="expired">`,
		`<option value="block" selected>Always block</option>`,
		`<th scope="col">Ended</th>`,
		`<td><span class="cell-label">Note</span> old</td>`,
		`<a href="/overrides?state=expired">Clear filters</a>`)

	_, page = b.get("/overrides?address=nonsense")
	wantAll(t, "a bad address", page, `<p class="callout" data-level="warning">&#34;nonsense&#34; is not an IP address or network; the list is not narrowed to it</p>`)
}

func TestOverridesPageStates(t *testing.T) {
	c, b := signedInBrowser(t)
	src := rulesNode(c)
	src.active, src.expired = nil, nil
	_, page := b.get("/overrides")
	wantAll(t, "no override", page, `<p class="empty">You have set no override: the verdicts and the allow-list decide.`)

	src.activeErr = errors.New("store closed")
	c.node.Status = func() []lifecycle.Status {
		return []lifecycle.Status{{Name: partStore, State: lifecycle.StateStopped}}
	}
	_, page = b.get("/overrides")
	wantAll(t, "a failing store", page,
		`<p class="callout">The store is not running: the overrides cannot be read until it runs again.</p>`,
		`<p class="callout" data-level="warning">The overrides could not be read: store closed</p>`)

	c.node.Rules = nil
	if _, page = b.get("/overrides"); !strings.Contains(page, "The overrides could not be read: the node passes the console no") {
		t.Errorf("without rules:\n%s", page)
	}
}

// TestAllowlistPage: every entry grouped by origin, the files with what
// was loaded, when and their state now, and the warnings (AC2, edge case
// 3).
func TestAllowlistPage(t *testing.T) {
	c, b := signedInBrowser(t)
	rulesNode(c)
	resp, page := b.get("/allowlist")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /allowlist = %d", resp.StatusCode)
	}
	wantAll(t, "the allow-list view", page,
		"<title>Allow-list · OBIE console</title>",
		`<a href="/allowlist" aria-current="page">Allow-list</a>`,
		`; loaded with the running configuration at <time datetime="2026-09-28T11:00:00Z">2026-09-28 11:00:00 UTC</time></p>`,
		`<h2 id="lookup-heading">Is this address protected?</h2>`,
		`<form class="filter-form" method="get" action="/allowlist" role="search" aria-label="Is this address protected?">`,
		`<h2 id="warnings-heading">Not protected</h2>`,
		`<p class="callout" data-level="warning">The bootstrap peer /dns4/gone.example/tcp/4001/p2p/12D3 did not resolve: no such host.`,
		`<h2 id="group-builtin">Built-in ranges</h2>`,
		`<p class="group-text"><span class="protected-mark">Protected</span> Special-purpose address space`,
		`<th scope="row">Private</th>`,
		`<li><span class="mono">10.0.0.0/8</span> <span class="cell-note">RFC 1918</span></li>`,
		`<th scope="row">Documentation</th>`,
		`<h2 id="group-self">This node&#39;s addresses</h2>`,
		`<tr><th scope="row"><span class="mono address" data-copy>185.0.9.1</span></th><td><span class="cell-label">From</span> <span class="mono">/ip4/0.0.0.0/tcp/4001</span></td></tr>`,
		`<h2 id="group-bootstrap">Bootstrap peers</h2>`,
		`<h2 id="group-cidrs">Configured networks</h2>`,
		`<td><span class="cell-label">From</span> allowlist.cidrs</td>`,
		`<h2 id="group-file-1">Allow-list file <code>/etc/obie/allow.txt</code></h2>`,
		`<p class="callout" data-file-state="changed">2 entries loaded at 2026-09-28 11:00:00 UTC. It changed since it was loaded and now holds 3 entries: they are not active until a reload applies them.</p>`,
		`<span class="mono">line 2</span>`,
		`<p class="callout" data-level="warning" data-file-state="warning">1 entry loaded at 2026-09-28 11:00:00 UTC. It now holds 3 lines the node rejects.`,
		`<tr><th scope="row">2</th><td><span class="cell-label">Entry</span> <code>185.0.5.0/33</code></td><td><span class="cell-label">Why it is rejected</span> invalid CIDR</td></tr>`,
		`<p class="note">1 more line is rejected, too; <code>obied --check-config</code> names the first.</p>`,
		`<p class="note">14 entries in all.`)
	for _, group := range []string{"group-builtin", "group-self", "group-bootstrap", "group-cidrs", "group-file-1", "group-file-2"} {
		if !strings.Contains(page, `aria-labelledby="`+group+`"`) {
			t.Errorf("no section %s", group)
		}
	}
	if strings.Contains(page, `class="lookup-answer"`) || strings.Contains(page, "data-refresh") {
		t.Error("the page answers a lookup nobody asked, or refreshes itself")
	}
}

// TestAllowlistLookup: the lookup names the rule that protects the
// address, with the entry and a link to its decision (AC5).
func TestAllowlistLookup(t *testing.T) {
	c, b := signedInBrowser(t)
	src := rulesNode(c)
	_, page := b.get("/allowlist?address=192.168.1.10")
	wantAll(t, "the lookup of a protected address", page,
		`<input id="lookup-address" name="address" type="search" value="192.168.1.10"`,
		`<div class="lookup-answer" data-state="protected">`,
		`<p class="lookup-title">Yes: 192.168.1.10 is protected by the built-in range 192.168.0.0/16 (Private). It is never blocked, not even by an always-block override.</p>`,
		`<dd><strong>Protected address</strong>: A protected allow-list entry (built in: special-purpose addresses) covers it.`,
		`<dt>Matching</dt>`,
		`<dd><code>192.168.0.0/16</code></dd>`,
		`<dd>private (RFC 1918)</dd>`,
		`<p><a href="/decisions/192.168.1.10">Its decision and the full explanation</a>`)

	_, page = b.get("/allowlist?address=185.0.1.7")
	wantAll(t, "the lookup of an operator entry", page,
		`<div class="lookup-answer" data-state="allowed">`,
		`<dd><strong>Allow-list</strong>: The operator&#39;s allow-list entry (allowlist.files) covers it.`,
		`<dd>/etc/obie/allow.txt:2</dd>`)

	_, page = b.get("/allowlist?address=203.0.113.7")
	wantAll(t, "the lookup of an unprotected address", page,
		`<div class="lookup-answer" data-state="none">`,
		`No: no allow-list entry or override covers 203.0.113.7. The verdicts decide whether it is blocked.`)

	_, page = b.get("/allowlist?address=10.0.0.0/8")
	if strings.Contains(page, "Its decision and the full explanation") {
		t.Error("the lookup links to the decision on a network the node cannot decide on")
	}

	_, page = b.get("/allowlist?address=not-an-address")
	wantAll(t, "a bad lookup", page, `<p class="callout" data-level="warning">&#34;not-an-address&#34; is not an IP address or network</p>`)
	src.mu.Lock()
	lookups := len(src.lookup)
	src.mu.Unlock()
	if lookups != 4 {
		t.Errorf("%d lookups, want 4: a bad address is not looked up", lookups)
	}

	src.protectionErr = errors.New("store closed")
	_, page = b.get("/allowlist?address=203.0.113.7")
	wantAll(t, "a failed lookup", page, `<p class="callout" data-level="warning">The lookup failed: store closed</p>`)

	c.node.Rules = nil
	if _, page = b.get("/allowlist"); !strings.Contains(page, "The allow-list could not be read: the node passes the console no") {
		t.Errorf("without rules:\n%s", page)
	}
}

// TestConfigurationPage: the load status, what changed on disk, and every
// setting by section with its value, the defaults marked, when a change
// takes effect and its explanation; secrets redacted (AC3, AC4, edge
// case 1).
func TestConfigurationPage(t *testing.T) {
	c, b := signedInBrowser(t)
	rulesNode(c)
	resp, page := b.get("/configuration")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /configuration = %d", resp.StatusCode)
	}
	wantAll(t, "the configuration view", page,
		"<title>Configuration · OBIE console</title>",
		`<a href="/configuration" aria-current="page">Configuration</a>`,
		`<dd><code data-copy>/etc/obie/obie.yaml</code></dd>`,
		`<dd><time datetime="2026-09-28T11:00:00Z">2026-09-28 11:00:00 UTC</time>, at start</dd>`,
		`<dd>2 settings are left to their defaults: the file does not set them.</dd>`,
		`<p class="condition-title"><span class="condition-level">Last reload:</span> The configuration was not reloaded since obied started.</p>`,
		`<li class="condition" data-level="warning" data-state="changed">`,
		`<span class="condition-level">File on disk:</span> The file on disk changed since it was loaded: 2 settings differ from the running configuration and are not active yet.`,
		`<strong>Not active until a reload:</strong> <code>node.mode</code>. Reload obied to apply it: <code>sudo systemctl reload obied</code>`,
		`<strong>Waiting for a restart:</strong> <code>mesh.bootstrap</code>. A reload does not apply it;`,
		`<h3 id="section-node"><code>node</code></h3>`,
		`<p class="group-text">The node itself: where it keeps its state and whether it blocks.</p>`,
		`<tr data-pending>`,
		`<th scope="row"><code class="setting-key">node.mode</code><span class="cell-note">Observe or enforce.</span></th>`,
		`<code class="value">observe</code>`,
		`<span class="pending"><strong>Not active until a reload.</strong> On disk: <code class="value">enforce</code></span>`,
		`<td><span class="cell-label">A change applies on</span> <span class="applied" data-applied="reload">Reload</span></td>`,
		`<code class="value">/var/lib/obie</code> <span class="default-mark">Default</span>`,
		`<td><span class="cell-label">A change applies on</span> <span class="applied" data-applied="restart">Restart</span></td>`,
		`<span class="empty-value">None</span> <span class="default-mark">Default</span>`,
		`<span class="pending"><strong>Waits for a restart.</strong> On disk: <ul class="value-list"><li><code>/ip4/192.0.2.1/tcp/4001/p2p/12D3</code></li></ul></span>`,
		`<span class="secret">Set, not shown</span>`)
	if strings.Contains(page, "data-refresh") {
		t.Error("the configuration view refreshes itself; it is read when the page opens")
	}
}

// TestConfigurationPageLoads: a rejected reload says that the previous
// configuration is still active, and an invalid file on disk is a warning
// (AC4, edge case 2).
func TestConfigurationPageLoads(t *testing.T) {
	c, b := signedInBrowser(t)
	src := rulesNode(c)
	src.cfg.Settings = src.cfg.Settings[1:2]
	src.cfg.Load = ConfigFacts{LoadedAt: ruleNow.Add(-time.Hour), Reloaded: true, RejectedAt: ruleNow, Rejected: "invalid configuration: decision.quorum"}
	src.cfg.DiskErr = "invalid configuration: decision.quorum"
	_, page := b.get("/configuration")
	wantAll(t, "a rejected reload", page,
		`<dd><time datetime="2026-09-28T11:00:00Z">2026-09-28 11:00:00 UTC</time>, by a reload</dd>`,
		`<li class="condition" data-level="warning" data-state="warning">`,
		`<span class="condition-level">Last reload:</span> The last reload, at 2026-09-28 12:00:00 UTC, was rejected: invalid configuration: decision.quorum. The configuration loaded at 2026-09-28 11:00:00 UTC is still active.`,
		`<strong>Next step:</strong> Fix the file, check it, then reload obied again. <code>obied --check-config</code>`,
		`<span class="condition-level">File on disk:</span> The file on disk cannot be loaded now: invalid configuration: decision.quorum. A reload would be rejected, and the running configuration kept.`,
		`<dd>1 setting is left to its default: the file does not set it.</dd>`)

	src.cfg = Configuration{Load: ConfigFacts{LoadedAt: ruleNow}}
	_, page = b.get("/configuration")
	wantAll(t, "a node without a file", page, `<dd>None</dd>`,
		`<li class="condition" data-level="note" data-state="none">`)

	c.node.Rules = nil
	if _, page = b.get("/configuration"); !strings.Contains(page, "The configuration could not be read: the node passes the console no") {
		t.Errorf("without rules:\n%s", page)
	}
}
