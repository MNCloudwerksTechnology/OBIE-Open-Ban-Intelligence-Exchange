package console

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestVerdictsPage: the verdicts view shows the totals (AC5), the scopes
// and states (AC1, AC2), every verdict with its evidence hash and counts,
// how it ended, and links to its publisher and its decision (AC4), and
// labels publishers without weight.
func TestVerdictsPage(t *testing.T) {
	c, b := signedInBrowser(t)
	src := verdictsNode(c)
	resp, page := b.get("/verdicts")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /verdicts = %d", resp.StatusCode)
	}
	wantAll(t, "the verdicts view", page,
		"<title>Verdicts · OBIE console</title>",
		`<p class="updated">Read at <time datetime="2026-09-28T12:00:00Z">2026-09-28 12:00:00 UTC</time></p>`,
		// Totals: this node's verdicts, then those received per publisher.
		`<th scope="row"><span class="peer-name">This node</span><span class="cell-note">published</span></th>`,
		`<td><span class="cell-label">Active</span> <a class="weight" href="/verdicts?from=mine">9</a><span class="cell-note">all count in decisions</span></td>`,
		`<td><span class="cell-label">Revoked</span> <a class="weight" href="/verdicts?from=mine&amp;state=revoked">2</a></td>`,
		`<th scope="row"><a class="peer-name" href="/peers/`+idAlpha+`">alpha</a><span class="peer-id mono" title="`+idAlpha+`">`,
		`<td><span class="cell-label">Active</span> <a class="weight" href="/verdicts?publisher=`+idAlpha+`">120</a><span class="cell-note">118 count in decisions</span></td>`,
		`<td><span class="cell-label">Revoked</span> <span class="weight">0</span></td>`,
		`<td><span class="cell-label">Trust weight</span> <span class="weight">0</span> <span class="badge">No weight</span></td>`,
		`<span class="peer-name">Every other publisher</span><span class="cell-note">received from 4 publishers</span>`,
		`A verdict that was revoked or expired is kept for 24 hours after its expiry, then forgotten.`,
		// The list: whose verdicts, filters, states.
		`<h2 id="list-heading">Active verdicts of every publisher</h2>`,
		`<li><a href="/verdicts" aria-current="page">All publishers</a></li>`,
		`<li><a href="/verdicts?from=mine">This node</a></li>`,
		`<li><a href="/verdicts?from=peers">Received</a></li>`,
		`<form class="filter-form" method="get" action="/verdicts" role="search" aria-label="Filter verdicts">`,
		`<option value="port_scan/tcp">port_scan (tcp)</option>`,
		`<option value="`+testNode.PeerID+`">This node: 9 verdicts</option>`,
		`<li><a href="/verdicts" aria-current="page">Active <span class="filter-count">3</span></a></li>`,
		`<li><a href="/verdicts?state=expired">Expired <span class="filter-count">1</span></a></li>`,
		// A verdict of a named peer, with its evidence hash and counts.
		`<tr class="verdict-main" data-state="active">`,
		`<th scope="row"><a class="mono address" href="/verdicts?address=203.0.113.7" data-copy>203.0.113.7</a></th>`,
		`<a href="/peers/`+idAlpha+`">alpha</a>`,
		`<span class="cell-note">weight 0.7</span>`,
		`<td><span class="cell-label">Verdict</span> ban<span class="cell-note">confidence 0.9</span><span class="cell-note">1,234 events</span></td>`,
		`<span class="cell-note">issued <time datetime="2026-09-28T11:00:00Z"><span class="day">2026-09-28</span> 11:00:00 UTC</time></span>`,
		// Its evidence's hash and its event ID, in full, below it.
		`<tr class="verdict-detail" data-state="active">`,
		`<span>log hash <code class="hash" data-copy>`+testLogHash+`</code></span>`,
		`<span>event <code class="hash">`+eventAlpha+`</code></span>`,
		`<span class="verdict-state" data-state="active">Active</span>`,
		`<span class="cell-note">counts: Yes</span>`,
		`<td><span class="cell-label">Decision</span> <a href="/decisions/203.0.113.7"><span class="decision-state" data-state="block">Block</span></a></td>`,
		// This node's own verdict, without a peer page and without log lines.
		`<span class="peer-name">This node</span>`,
		`<span>no log lines given</span>`,
		// A peer without weight.
		`<span class="badge">No weight</span>`,
		`<span class="cell-note">counts: No: a watch verdict</span>`,
		// A revoked and an expired verdict, and the decision the node no
		// longer holds.
		`<tr class="verdict-main" data-state="revoked">`,
		`<span class="verdict-state" data-state="revoked">Revoked</span>`,
		`<span class="cell-note">why: false_positive</span>`,
		`<span class="cell-note">at <time datetime="2026-09-28T11:00:00Z">2026-09-28 11:00:00 UTC</time></span>`,
		`<span>revocation <code class="hash">`+eventRevoker+`</code></span>`,
		`<tr class="verdict-main" data-state="expired">`,
		`<span class="verdict-state" data-state="expired">Expired</span>`,
		`<a href="/decisions/198.51.100.9"><span class="decision-state" data-state="idle">No decision</span></a>`,
		`<p class="pager-text">5 verdicts</p>`,
	)
	if nav := navOf(t, page); nav[len(nav)-1] != (navItem{Path: "/verdicts", Title: "Verdicts", Current: "page"}) {
		t.Errorf("navigation = %+v, want Verdicts last, marked", nav)
	}
	if strings.Contains(page, "data-refresh") {
		t.Error("the verdicts view refreshes itself; it is read when the page opens")
	}
	if q := src.lastQuery(t); q != (VerdictQuery{State: VerdictActive, Limit: verdictsPageSize}) {
		t.Errorf("query = %+v", q)
	}
}

// TestVerdictsPageViews: this node's revoked verdicts, a publisher's
// expired ones (shown only on request, and said so), and the verdicts on
// one address as obiectl show gives them (AC3).
func TestVerdictsPageViews(t *testing.T) {
	c, b := signedInBrowser(t)
	src := verdictsNode(c)
	_, page := b.get("/verdicts?from=mine&state=revoked")
	wantAll(t, "this node's revoked verdicts", page,
		`<h2 id="list-heading">Revoked verdicts of this node</h2>`,
		`<li><a href="/verdicts?from=mine&amp;state=revoked" aria-current="page">This node</a></li>`,
		`<li><a href="/verdicts?from=mine&amp;state=revoked" aria-current="page">Revoked <span class="filter-count">1</span></a></li>`,
		`<input type="hidden" name="from" value="mine">`,
		`<input type="hidden" name="state" value="revoked">`,
		`<p class="callout">Revoked verdicts: their publisher withdrew them, so they count in no decision any more. The node keeps them for 24 hours after their expiry, then forgets them.</p>`)

	_, page = b.get("/verdicts?publisher=" + url.QueryEscape(idStray) + "&state=expired")
	wantAll(t, "a publisher's expired verdicts", page,
		`<h2 id="list-heading">Expired verdicts of `+shortPeerID(idStray)+`</h2>`,
		`<p class="whose">The verdicts of <a href="/peers/`+idStray+`">`+shortPeerID(idStray)+`</a> <code class="id">`+idStray+
			`</code>, trust weight <span class="weight">0</span>. <span class="badge">No weight</span> Its verdicts never count in decisions.</p>`,
		`<p class="callout">Expired verdicts: they reached their expiry unrevoked`,
		`<a href="/verdicts?state=expired">Clear filters</a>`)
	if strings.Contains(page, `aria-current="page">All publishers`) {
		t.Error("a scope is current while one publisher is listed")
	}
	if q := src.lastQuery(t); q.Publisher != idStray || q.State != VerdictExpired {
		t.Errorf("query = %+v", q)
	}

	_, page = b.get("/verdicts?address=203.0.113.7")
	wantAll(t, "the verdicts on one address", page,
		`<h2 id="list-heading">Active verdicts of every publisher on 203.0.113.7</h2>`,
		`<input id="verdicts-address" name="address" type="search" value="203.0.113.7"`,
		`<p>The verdicts on <a class="mono" href="/decisions/203.0.113.7" data-copy>203.0.113.7</a> itself, as <code>sudo obiectl show 203.0.113.7</code> lists the active ones.`,
		`<a href="/decisions/203.0.113.7">Its decision and the full explanation</a>`)
	if q := src.lastQuery(t); q.Range != pfx("203.0.113.7/32") || q.Publisher != "" || q.Except != "" {
		t.Errorf("query = %+v", q)
	}

	src.list = VerdictList{States: map[string]int{VerdictActive: 3}}
	_, page = b.get("/verdicts?state=expired")
	wantAll(t, "no expired verdict", page,
		`<p class="empty">No expired verdict is kept. The node keeps a verdict that ended for 24 hours after its expiry.</p>`)
}

// TestVerdictsPageFailures: failed reads are shown as such, and the rest
// of the page works.
func TestVerdictsPageFailures(t *testing.T) {
	c, b := signedInBrowser(t)
	src := verdictsNode(c)
	src.err, src.totalsErr = errors.New("store is not open"), errors.New("store is not open")
	resp, page := b.get("/verdicts?state=revoked")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /verdicts = %d", resp.StatusCode)
	}
	wantAll(t, "failed reads", page,
		`<p class="callout" data-level="warning">The totals could not be read: store is not open</p>`,
		`<p class="callout" data-level="warning">The verdicts could not be read: store is not open</p>`,
		`<form class="filter-form"`)

	_, page = b.get("/verdicts?address=not-an-address")
	wantAll(t, "a bad address", page,
		`<p class="callout" data-level="warning">&#34;not-an-address&#34; is not an IP address or network; the list is not narrowed to it</p>`)

	c.node.Verdicts = nil
	_, page = b.get("/verdicts")
	wantAll(t, "no verdict source", page, `The verdicts could not be read: the node passes the console no verdicts`)
}

// TestVerdictsPageEscapesData: publisher names and verdict fields come from
// the configuration and the network; they are escaped.
func TestVerdictsPageEscapesData(t *testing.T) {
	c, b := signedInBrowser(t)
	src := verdictsNode(c)
	evil := `<script>alert("x")</script>`
	c.node.Peers = func() PeerSet {
		return PeerSet{Peers: []Peer{{ID: idAlpha, Name: evil, Publisher: true, Weight: 1}},
			Verdicts: map[string]VerdictCount{idAlpha: {Held: 1}}}
	}
	src.categories = map[string]int{evil: 1}
	src.list = VerdictList{Items: []VerdictItem{{Range: pfx("203.0.113.7/32"), Publisher: idAlpha, Action: evil, Reason: evil,
		Protocol: evil, LogHash: evil, EventID: evil, State: VerdictRevoked, Revocation: &VerdictRevocation{ID: evil, Reason: evil}}}}
	src.totals.ByPublisher = map[string]VerdictCounts{idAlpha: {Active: 1}}
	_, page := b.get("/verdicts?publisher=" + url.QueryEscape(idAlpha) + "&address=" + url.QueryEscape(evil))
	if strings.Contains(page, "<script>alert") {
		t.Errorf("verdict data not escaped:\n%s", page)
	}
	if !strings.Contains(page, `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`) {
		t.Errorf("the publisher's name is not shown:\n%s", page)
	}
}
