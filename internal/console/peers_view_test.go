package console

import (
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// peersNode makes c show the test peers at peersNow, with a running mesh,
// and counts how often it reads a peer's verdicts.
func peersNode(c *Console) *atomic.Int32 {
	var reads atomic.Int32
	c.now = func() time.Time { return peersNow }
	c.node.Status = func() []lifecycle.Status { return runningStatuses() }
	c.node.Peers = func() PeerSet { return testPeers }
	c.node.PeerVerdicts = func(id, after string, limit int) (VerdictPage, error) {
		reads.Add(1)
		return VerdictPage{Verdicts: []Verdict{
			{Key: "ipv4:203.0.113.7", Address: "203.0.113.7", Action: "ban", Confidence: 0.9, Reason: "password_bruteforce",
				Protocol: "ssh", ExpiresAt: peersNow.Add(23 * time.Hour)},
		}, Next: "ipv4:203.0.113.7"}, nil
	}
	return &reads
}

// wantAll checks that page contains every string in want.
func wantAll(t *testing.T, what, page string, want ...string) {
	t.Helper()
	missing := false
	for _, w := range want {
		if !strings.Contains(page, w) {
			t.Errorf("%s lacks %q", what, w)
			missing = true
		}
	}
	if missing {
		t.Logf("%s:\n%s", what, page)
	}
}

// TestPeersPage: the peers view lists every known peer with how it is
// configured and connected, its trust, verdicts and events (AC1–AC3),
// filters and sort links (AC5).
func TestPeersPage(t *testing.T) {
	c, b := signedInBrowser(t)
	peersNode(c)
	resp, page := b.get("/peers")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /peers = %d", resp.StatusCode)
	}
	wantAll(t, "the peers view", page,
		"<title>Peers · OBIE console</title>",
		"<h1>Peers</h1>",
		`<div class="refresh" data-refresh="/api/peers">`,
		`<p class="updated" data-tick>Updated <time datetime="2026-09-28T12:00:00Z">2026-09-28 12:00:00 UTC</time></p>`,
		// AC5: filters with counts and sort links.
		`<nav class="filters" aria-label="Show peers">`,
		`<li><a href="/peers" aria-current="page">All <span class="filter-count">4</span></a></li>`,
		`<li><a href="/peers?show=connected">Connected <span class="filter-count">2</span></a></li>`,
		`<li><a href="/peers?show=disconnected">Disconnected <span class="filter-count">2</span></a></li>`,
		`<li><a href="/peers?show=untrusted">Untrusted <span class="filter-count">2</span></a></li>`,
		`<th scope="col" aria-sort="ascending"><a href="/peers">Peer</a></th>`,
		`<th scope="col"><a href="/peers?sort=trust">Trust weight</a></th>`,
		`<th scope="col"><a href="/peers?sort=rejected">Events, last hour</a></th>`,
		// AC1, AC2: a connected bootstrap peer that is a trusted publisher.
		`<a class="peer-name" href="/peers/`+idAlpha+`">alpha</a>`,
		`<span class="peer-id mono" title="`+idAlpha+`">12D3KooW…rAa1ph</span>`,
		`<span class="roles"><span class="role">Bootstrap peer</span><span class="role">Trusted publisher</span></span>`,
		`<ul class="addrs"><li>/ip4/192.0.2.1/tcp/4001</li><li>/ip4/192.0.2.1/udp/4001/quic-v1</li></ul>`,
		`<span class="peer-state" data-state="ready">Connected</span>`,
		`<span class="cell-note">since <time datetime="2026-09-28T10:00:00Z">2026-09-28 10:00:00 UTC</time></span>`,
		`<span class="weight">0.7</span>`+"\n        "+`<span class="cell-note">trust.publishers</span>`,
		// AC3: verdicts and events with the reasons for rejection.
		`<span class="weight">120</span>`+"\n        "+`<span class="cell-note">118 count in decisions</span>`,
		`<span>57 accepted</span>`,
		`<span class="rejected">3 rejected</span>`,
		`<span class="cell-note">2 invalid signature, 1 expired or dated in the future</span>`+"\n        "+`<span class="cell-note">4 already known</span>`,
		// Edge case: an unreachable bootstrap peer, with when it was last seen.
		`<span class="peer-state" data-state="warning">Disconnected</span>`,
		`<span class="cell-note">last seen <time datetime="2026-09-28T11:30:00Z">2026-09-28 11:30:00 UTC</time></span>`,
		`<span class="cell-note">last dial failed <time datetime="2026-09-28T11:59:00Z">2026-09-28 11:59:00 UTC</time></span>`,
		`<span class="cell-note">not connected since obied started</span>`,
		// Edge case: a connected peer without trust has no influence.
		`<a class="peer-name" href="/peers/`+idStray+`">Unnamed peer</a>`,
		`<span class="role">Not configured</span>`,
		`<span class="weight">0</span>`+"\n        "+`<span class="cell-note">default weight</span>`+"\n        "+`<span class="badge">No influence on decisions</span>`,
		`<p class="pager-text">4 peers</p>`,
		`<p class="note">This node also holds 5 active verdicts from 1 other publisher that are neither configured nor connected. They carry the default weight 0: no influence on decisions.</p>`,
	)
	if nav := navOf(t, page); len(nav) < 2 || nav[1] != (navItem{Path: "/peers", Title: "Peers", Current: "page"}) {
		t.Errorf("navigation = %+v, want Peers after Overview, marked", nav)
	}
}

// peersRegion matches the refreshing region of the peers view.
var peersRegion = regexp.MustCompile(`(?s)<div class="refresh" data-refresh="[^"]*">\n(.*)\n</div>\n\s*</main>`)

// TestPeersFragment: the region refreshes from a fragment with the same
// filter, order and page, and renders exactly what the page shows there.
func TestPeersFragment(t *testing.T) {
	c, b := signedInBrowser(t)
	peersNode(c)
	_, page := b.get("/peers?show=connected&sort=trust")
	wantAll(t, "the filtered peers view", page,
		`<div class="refresh" data-refresh="/api/peers?show=connected&amp;sort=trust">`,
		`<li><a href="/peers?show=connected&amp;sort=trust" aria-current="page">Connected <span class="filter-count">2</span></a></li>`,
		`<th scope="col" aria-sort="descending"><a href="/peers?show=connected&amp;sort=trust">Trust weight</a></th>`,
		`<p class="pager-text">2 peers</p>`)
	if strings.Contains(page, idBravo) {
		t.Error("the connected filter lists a disconnected peer")
	}
	m := peersRegion.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no refreshing region:\n%s", page)
	}
	resp, fragment := b.get("/api/peers?show=connected&sort=trust")
	if resp.StatusCode != http.StatusOK || fragment != m[1] {
		t.Errorf("GET /api/peers = %d; fragment differs from the region\nfragment:\n%s\nregion:\n%s", resp.StatusCode, fragment, m[1])
	}
}

// TestPeersPageWithoutPeers: a node without peers, or without the
// function to read them, still gets a page that says why it is empty.
func TestPeersPageWithoutPeers(t *testing.T) {
	c, b := signedInBrowser(t)
	c.node.Peers = nil
	c.node.Status = func() []lifecycle.Status { return []lifecycle.Status{{Name: partMesh, State: lifecycle.StateStopped}} }
	resp, page := b.get("/peers")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /peers = %d", resp.StatusCode)
	}
	wantAll(t, "the empty peers view", page,
		`<p class="callout">The mesh is not running: no peer is connected. The configured peers are listed as configured.</p>`,
		`<p class="empty">This node knows no peer: none is configured in mesh.bootstrap or trust.publishers, and none is connected.`)
	if strings.Contains(page, "<table") {
		t.Error("an empty list shows a table")
	}
}

// peerRegion matches the refreshing region of a peer's page, which the
// verdicts follow.
var peerRegion = regexp.MustCompile(`(?s)<div class="refresh" data-refresh="[^"]*">\n(.*)\n</div>\n<section`)

// TestPeerPage: opening a peer shows its facts and the verdicts the node
// holds from it (AC4); the facts refresh, the verdicts are read once.
func TestPeerPage(t *testing.T) {
	c, b := signedInBrowser(t)
	reads := peersNode(c)
	resp, page := b.get("/peers/" + idAlpha)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /peers/alpha = %d", resp.StatusCode)
	}
	wantAll(t, "alpha's page", page,
		"<title>Peer alpha · OBIE console</title>",
		`<p class="back"><a href="/peers">All peers</a></p>`,
		"<h1>alpha</h1>",
		`<div class="refresh" data-refresh="/api/peers/`+idAlpha+`">`,
		`<dd><code class="id">`+idAlpha+`</code></dd>`,
		`<dd>alpha</dd>`,
		`<span class="peer-state" data-state="ready">Connected</span> since <time datetime="2026-09-28T10:00:00Z">2026-09-28 10:00:00 UTC</time>; round trip 12.3ms`,
		`<span class="cell-note">of the open connections</span>`,
		`<dd><strong>0.7</strong>, set in trust.publishers.</dd>`,
		`<dd>120: 118 count in decisions</dd>`,
		`<li>57 accepted</li>`,
		`<li><span class="rejected">3 rejected</span>: 2 invalid signature, 1 expired or dated in the future</li>`,
		`<li>4 already known (duplicates are normal in gossip)</li>`,
		// AC4: its verdicts, and the commands while the verdict view does not exist.
		`<h2 id="verdicts-heading">Verdicts held from this peer</h2>`,
		`Every verdict on one address: <code>obiectl show &lt;address&gt;</code>.`,
		`All of them, page by page: <code>obiectl indicators --publisher `+idAlpha+`</code>`,
		`<th scope="row"><span class="mono">203.0.113.7</span></th>`,
		`<td><span class="cell-label">Reason</span> password_bruteforce (ssh)</td>`,
		`<td><span class="cell-label">Expires</span> <time datetime="2026-09-29T11:00:00Z">2026-09-29 11:00:00 UTC</time></td>`,
		`<td><span class="cell-label">Counts in decisions</span> <span>Yes</span></td>`,
		`<a href="/peers/`+idAlpha+`?after=ipv4%3A203.0.113.7" rel="next">Next verdicts</a>`,
	)
	if nav := navOf(t, page); len(nav) < 2 || nav[1].Current != "true" {
		t.Errorf("navigation = %+v, want Peers marked as containing the page", nav)
	}
	if n := reads.Load(); n != 1 {
		t.Errorf("the page read the verdicts %d times, want once", n)
	}

	m := peerRegion.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no refreshing region:\n%s", page)
	}
	resp, fragment := b.get("/api/peers/" + idAlpha)
	if resp.StatusCode != http.StatusOK || fragment != m[1] {
		t.Errorf("GET /api/peers/alpha = %d; fragment differs from the region\nfragment:\n%s\nregion:\n%s", resp.StatusCode, fragment, m[1])
	}
	if n := reads.Load(); n != 1 {
		t.Errorf("the fragment read the verdicts: %d reads, want 1", n)
	}

	// Once the verdict view exists, the verdicts link into it.
	c.pages = append(c.pages, view{Path: "/verdicts", Title: "Verdicts"})
	_, page = b.get("/peers/" + idAlpha + "?after=ipv4%3A1.2.3.4")
	wantAll(t, "alpha's page with a verdict view", page,
		`<th scope="row"><a class="mono" href="/verdicts?address=203.0.113.7">203.0.113.7</a></th>`,
		`<a href="/verdicts?publisher=`+idAlpha+`">All its verdicts in the verdict view</a>`,
		`<a href="/peers/`+idAlpha+`">First page</a>`)
}

// TestPeerPageStates: an unreachable bootstrap peer, an untrusted
// connected peer, a publisher known only by its verdicts, and peers the
// node does not know.
func TestPeerPageStates(t *testing.T) {
	c, b := signedInBrowser(t)
	peersNode(c)
	_, page := b.get("/peers/" + idBravo)
	wantAll(t, "bravo's page", page,
		`<span class="peer-state" data-state="warning">Disconnected</span> last seen <time datetime="2026-09-28T11:30:00Z">`,
		`<dt>Last failed dial</dt>`,
		`<span class="mono dial-error">failed to dial: connection refused</span>`,
		`<span class="cell-note">from mesh.bootstrap</span>`,
		`<dd><strong>0</strong>, the default weight (trust.default_weight): the peer is not listed in trust.publishers. <span class="badge">No influence on decisions</span> Its verdicts are held, but never count.</dd>`,
		`None: the peer sent this node no event in the last hour.`)

	_, page = b.get("/peers/" + idStray)
	wantAll(t, "the stray peer's page", page,
		"<title>Peer 12D3KooW…rayPee · OBIE console</title>",
		`<h1>Unnamed peer <span class="mono">12D3KooW…rayPee</span></h1>`,
		`<dd>None: the peer is not listed in trust.publishers</dd>`,
		`<span class="role">Not configured</span></span> It connected to this node on its own.</dd>`)

	resp, page := b.get("/peers/" + idOther)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /peers/other = %d", resp.StatusCode)
	}
	wantAll(t, "the other publisher's page", page,
		`It is neither configured nor connected: other peers relayed its verdicts.`,
		`<dd>5: none counts in decisions</dd>`)

	for _, id := range []string{"12D3KooWUnknown", testNode.PeerID} {
		resp, page := b.get("/peers/" + id)
		if resp.StatusCode != http.StatusNotFound ||
			!strings.Contains(page, "This node knows no such peer: it is neither configured nor connected, and the node holds no verdict of it.") {
			t.Errorf("GET /peers/%s = %d:\n%s", id, resp.StatusCode, page)
		}
	}
	// An open page of a peer that went away says so on its next refresh.
	resp, fragment := b.get("/api/peers/12D3KooWUnknown")
	if resp.StatusCode != http.StatusOK || !strings.Contains(fragment, "This node knows this peer no longer") {
		t.Errorf("GET /api/peers/unknown = %d %q", resp.StatusCode, fragment)
	}
}

// TestPeersPagesEscapePeerData: names, addresses, dial errors and verdicts
// come from the configuration and the network; they are escaped.
func TestPeersPagesEscapePeerData(t *testing.T) {
	c, b := signedInBrowser(t)
	peersNode(c)
	evil := `<script>alert("x")</script>`
	c.node.Peers = func() PeerSet {
		return PeerSet{Peers: []Peer{{ID: idAlpha, Name: evil, Publisher: true, Addrs: []string{`"><img src=x>`},
			DialError: evil, DialFailedAt: peersNow, Events: EventCounts{Rejected: map[string]int{"<b>new</b>": 1}}}},
			Verdicts: map[string]VerdictCount{idAlpha: {Held: 1}}}
	}
	c.node.PeerVerdicts = func(string, string, int) (VerdictPage, error) {
		return VerdictPage{Verdicts: []Verdict{{Address: evil, Action: evil, Reason: evil, Protocol: evil}}}, nil
	}
	for _, path := range []string{"/peers", "/api/peers", "/peers/" + idAlpha, "/api/peers/" + idAlpha} {
		_, page := b.get(path)
		if strings.Contains(page, `<script>alert`) || strings.Contains(page, `<img src=x`) || strings.Contains(page, `<b>new`) {
			t.Errorf("%s: peer data not escaped:\n%s", path, page)
		}
		if !strings.Contains(page, `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`) {
			t.Errorf("%s: the peer's name is not shown:\n%s", path, page)
		}
	}
}
