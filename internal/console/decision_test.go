package console

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// consensusExplanation is 203.0.113.7, blocked by consensus of three
// publishers, inside the force-blocked 203.0.113.0/24 whose entry applies
// it.
func consensusExplanation() Explanation {
	return Explanation{
		Range: pfx("203.0.113.7/32"), State: StateBlock, Score: 2.4000000000000004, Threshold: 1.8,
		Contributors: 3, Quorum: 2, LocalAutoblock: true,
		Reason:    "consensus: score 2.4 >= threshold 1.8, 3 >= quorum 2",
		ExpiresAt: decisionsNow.Add(2 * time.Hour), EvaluatedAt: decisionsNow,
		Verdicts: []Contribution{
			{PeerID: testNode.PeerID, Local: true, Action: "ban", Reason: "password_bruteforce", Protocol: "ssh", Weight: 1,
				Confidence: 1, Score: 1, Contributes: true, IssuedAt: decisionsNow.Add(-time.Hour), ExpiresAt: decisionsNow.Add(3 * time.Hour)},
			{PeerID: idAlpha, Name: "alpha", Listed: true, Action: "ban", Reason: "password_bruteforce", Protocol: "ssh", Weight: 0.7,
				Confidence: 1, Score: 0.7, Contributes: true, IssuedAt: decisionsNow.Add(-time.Hour), ExpiresAt: decisionsNow.Add(2 * time.Hour)},
			{PeerID: idOther, Action: "ban", Reason: "port_scan", Protocol: "tcp", Weight: 0.7, Confidence: 1, Score: 0.7,
				Contributes: true, IssuedAt: decisionsNow.Add(-time.Hour), ExpiresAt: decisionsNow.Add(2 * time.Hour)},
			{PeerID: idStray, Action: "watch", Reason: "http_probe", Protocol: "http", Confidence: 0.4,
				IssuedAt: decisionsNow.Add(-time.Hour), ExpiresAt: decisionsNow.Add(time.Hour)},
		},
		Kept: true, KeptState: StateBlock, KeptAt: decisionsNow.Add(-2 * time.Minute),
		Around:   []DecisionItem{testItems[1]},
		Firewall: Coverage{Applied: true, Entry: pfx("203.0.113.0/24"), EntryExpires: decisionsNow.Add(time.Hour)},
	}
}

// explainNode makes c explain the test ranges: 203.0.113.7 as
// consensusExplanation, 198.51.100.0/24 as a network below consensus,
// 198.51.100.200 as an address the node knows nothing about and
// 2001:db8::1 as its own address, in enforce mode.
func explainNode(c *Console) *fakeDecisions {
	src := decisionsNode(c)
	c.node.Mode = func() string { return "enforce" }
	src.explained[pfx("203.0.113.7/32")] = consensusExplanation()
	src.explained[pfx("198.51.100.0/24")] = Explanation{Range: pfx("198.51.100.0/24"),
		State: StateNone, Score: 0.7, Threshold: 1.8, Contributors: 1, Quorum: 2, EvaluatedAt: decisionsNow,
		Verdicts: consensusExplanation().Verdicts[1:2], Kept: true, KeptState: StateNone}
	src.explained[pfx("198.51.100.200/32")] = Explanation{Range: pfx("198.51.100.200/32"),
		State: StateNone, Threshold: 1.8, Quorum: 2, Reason: "no active verdicts", EvaluatedAt: decisionsNow}
	src.explained[pfx("2001:db8::1/128")] = Explanation{Range: pfx("2001:db8::1/128"),
		State: StateAllowed, EvaluatedAt: decisionsNow, Ruling: Ruling{Effect: "allow", Rule: ruleAllowlist, Source: "self",
			Protected: true, Match: "2001:db8::1/128", Label: "this node's address", Reason: "allow-listed: 2001:db8::1/128 (this node's address)"}}
	return src
}

// TestDecisionExplanation: AC3 — every verdict with its publisher, trust
// weight, confidence, reason and expiry; how they add up; the operator's
// rules; the firewall and which entry wins.
func TestDecisionExplanation(t *testing.T) {
	c, b := signedInBrowser(t)
	explainNode(c)
	resp, page := b.get("/decisions/203.0.113.7")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /decisions/203.0.113.7 = %d:\n%s", resp.StatusCode, page)
	}
	wantAll(t, "the explanation", page,
		"<title>Decision on 203.0.113.7 · OBIE console</title>",
		`<p class="back"><a href="/decisions">All decisions</a></p>`,
		`<h1><span class="mono" data-copy>203.0.113.7</span></h1>`,
		`<code>obiectl explain 203.0.113.7</code>`,
		`<div class="refresh" data-refresh="/api/decisions/203.0.113.7">`,
		`<div class="summary" data-state="attention">`,
		`<strong class="summary-title">Blocked by consensus until 2026-09-28 14:00:00 UTC</strong> Score 2.4 of threshold 1.8, 3 publishers of quorum 2.`,
		// Every verdict.
		`<th scope="row">This node<span class="peer-id mono" title="`+testNode.PeerID+`">`,
		`<span class="weight">1</span><span class="cell-note">trust.local_weight</span>`,
		`<th scope="row"><a href="/peers/`+idAlpha+`">alpha</a>`,
		`<span class="weight">0.7</span><span class="cell-note">trust.publishers</span>`,
		`<span class="cell-note">trust.default_weight</span>`,
		`<td><span class="cell-label">Reason</span> password_bruteforce (ssh)</td>`,
		`<td><span class="cell-label">Reason</span> port_scan (tcp)</td>`,
		`<span class="cell-note">No: a watch verdict</span>`,
		// AC3: every verdict on it in the verdicts view (ADR 0023).
		`<a href="/verdicts?address=203.0.113.7">Every verdict on it, with its evidence hash, and the revoked and expired ones</a>`,
		`<td><span class="cell-label">Expires</span> <time datetime="2026-09-28T15:00:00Z">2026-09-28 15:00:00 UTC</time><span class="cell-note">issued <time datetime="2026-09-28T11:00:00Z">2026-09-28 11:00:00 UTC</time></span></td>`,
		// How they add up.
		`<dd><strong>2.4</strong> of threshold 1.8: <span class="check" data-met="yes">reached</span></dd>`,
		`<dd><strong>3</strong> counting, of quorum 2: <span class="check" data-met="yes">reached</span></dd>`,
		`<dd>No: on (decision.local_autoblock), but this node holds no ban verdict of its own on it that decides.</dd>`,
		`<dd>consensus: score 2.4 &gt;= threshold 1.8, 3 &gt;= quorum 2</dd>`,
		// The operator's rules.
		`<dd>No allow-list entry or override covers it: the verdicts decide.</dd>`,
		`<dd>Not protected: no allow-list entry or override covers it.</dd>`,
		// The firewall, and the network whose entry wins.
		`<span class="peer-state" data-state="ready">Applied</span> The firewall&#39;s entry for the wider network 203.0.113.0/24 drops its traffic; it needs no entry of its own.`,
		`<dd><a class="mono" href="/decisions/203.0.113.0/24">203.0.113.0/24</a>, until <time datetime="2026-09-28T13:00:00Z">2026-09-28 13:00:00 UTC</time></dd>`,
		`<dd>Last enforcement pass: 2026-09-28 11:59:00 UTC, in enforce mode. <a href="/enforcement">What the firewall applies</a></dd>`,
		`<a class="mono address" href="/decisions/203.0.113.0/24" data-copy>203.0.113.0/24</a> <span class="badge">Its entry wins</span>`,
		`<span class="decision-state" data-state="block">Block</span><span class="cell-note">operator force-block</span>`,
	)
	if strings.Contains(page, "Decisions inside this network") {
		t.Error("an address links to the decisions inside it")
	}
	if nav := navOf(t, page); nav[2] != (navItem{Path: "/decisions", Title: "Decisions", Current: "true"}) {
		t.Errorf("navigation = %+v, want Decisions marked as containing the page", nav)
	}

	// The refreshing region is the whole explanation, evaluated again.
	m := peersRegion.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no refreshing region:\n%s", page)
	}
	if _, fragment := b.get("/api/decisions/203.0.113.7"); fragment != m[1] {
		t.Errorf("fragment differs from the page's region\nfragment:\n%s\nregion:\n%s", fragment, m[1])
	}
}

// TestDecisionChangesWhileOpen: a decision that changes while its page is
// open shows changed with the next refresh.
func TestDecisionChangesWhileOpen(t *testing.T) {
	c, b := signedInBrowser(t)
	src := explainNode(c)
	_, before := b.get("/api/decisions/198.51.100.0/24")
	src.mu.Lock()
	ex := src.explained[pfx("198.51.100.0/24")]
	ex.State, ex.ExpiresAt, ex.Score, ex.Contributors = StateBlock, decisionsNow.Add(time.Hour), 1.9, 2
	src.explained[pfx("198.51.100.0/24")] = ex
	src.mu.Unlock()
	_, after := b.get("/api/decisions/198.51.100.0/24")
	if !strings.Contains(before, "Not blocked: below consensus") || !strings.Contains(after, "Blocked by consensus until 2026-09-28 13:00:00 UTC") {
		t.Errorf("before:\n%s\nafter:\n%s", before, after)
	}
	wantAll(t, "the refreshed explanation", after,
		"The node still holds it as None; the decision engine re-evaluates it within seconds.")
}

// TestDecisionRanges: IPv4 and IPv6 addresses and networks are explained
// at their own path; a network links to the decisions inside it.
func TestDecisionRanges(t *testing.T) {
	c, b := signedInBrowser(t)
	explainNode(c)
	resp, page := b.get("/decisions/198.51.100.0/24")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET a network = %d", resp.StatusCode)
	}
	wantAll(t, "a network", page,
		`<h1><span class="mono" data-copy>198.51.100.0/24</span></h1>`,
		"on this network:",
		`<div class="refresh" data-refresh="/api/decisions/198.51.100.0/24">`,
		`<p><a href="/decisions?q=198.51.100.0%2F24&amp;sort=address">Decisions inside this network</a></p>`,
		`<p>The node holds no decision on a network that contains it.</p>`)
	// A range typed otherwise is shown in its canonical form.
	if _, page := b.get("/decisions/198.51.100.9/24"); !strings.Contains(page, `<h1><span class="mono" data-copy>198.51.100.0/24</span></h1>`) {
		t.Error("a network with host bits is not shown as the network")
	}
	resp, page = b.get("/decisions/2001:db8::1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET an IPv6 address = %d", resp.StatusCode)
	}
	wantAll(t, "a protected IPv6 address", page,
		`<h1><span class="mono" data-copy>2001:db8::1</span></h1>`,
		`<strong class="summary-title">Allowed: never blocked, whatever the verdicts</strong>`,
		`<dd><strong>Protected address</strong>: A protected allow-list entry (this node&#39;s own address) covers it.`,
		`<dd><code>2001:db8::1/128</code></dd>`,
		`<dd>this node&#39;s address</dd>`,
		`<dd>Protected: allow-listed: 2001:db8::1/128 (this node&#39;s address). Nothing blocks it, not even a force-block.</dd>`)
}

// TestDecisionUnknownAddress: AC4 — an address the node knows nothing
// about has no verdicts, is not blocked, and says whether it would be
// protected.
func TestDecisionUnknownAddress(t *testing.T) {
	c, b := signedInBrowser(t)
	explainNode(c)
	_, page := b.get("/decisions/198.51.100.200")
	wantAll(t, "an unknown address", page,
		`<strong class="summary-title">No verdicts, not blocked</strong> The node holds no active verdict on it.`,
		`<p class="note">The node holds no decision on it: no active verdict and no force-block. It is evaluated here as it would be.</p>`,
		`<p class="empty">No verdicts: no publisher, this node included, holds an active verdict on it. <a href="/verdicts?address=198.51.100.200">Revoked and expired verdicts on it</a></p>`,
		`<dd>Not protected: no allow-list entry or override covers it.</dd>`,
		`<span class="peer-state" data-state="idle">Not applied</span> It is not a block, and no firewall entry covers it.`)
	_, page = b.get("/decisions/192.0.2.1")
	wantAll(t, "an unknown protected address", page,
		`<dd>Protected: allow-listed: 192.0.2.0/24 (TEST-NET-1, built-in). Nothing blocks it, not even a force-block.</dd>`)
}

// TestDecisionNotFound: text that is no address, or a range the node
// cannot decide on, is not found; a failed read is shown on the page.
func TestDecisionNotFound(t *testing.T) {
	c, b := signedInBrowser(t)
	src := explainNode(c)
	for _, path := range []string{"/decisions/example.org", "/decisions/10.0.0.0/8", "/decisions/"} {
		resp, page := b.get(path)
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(page, "It is not an address or network the node can decide on.") {
			t.Errorf("GET %s = %d:\n%s", path, resp.StatusCode, page)
		}
	}
	if resp, body := b.get("/api/decisions/example.org"); resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, `<p class="callout" data-level="warning">&#34;example.org&#34; is not an IP address or network</p>`) {
		t.Errorf("GET the region of no address = %d %q", resp.StatusCode, body)
	}

	src.explained = nil
	c.node.Decisions = failingExplain{src}
	resp, page := b.get("/decisions/192.0.2.99")
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "The decision could not be evaluated: read verdicts: store closed") {
		t.Errorf("GET with a failing store = %d:\n%s", resp.StatusCode, page)
	}
	c.node.Decisions = nil
	if resp, page := b.get("/decisions/192.0.2.99"); resp.StatusCode != http.StatusOK ||
		!strings.Contains(page, "The node passes the console no decisions.") {
		t.Errorf("GET without a source = %d", resp.StatusCode)
	}
}

// failingExplain fails to read any verdicts.
type failingExplain struct{ *fakeDecisions }

func (failingExplain) Explain(netip.Prefix) (Explanation, error) {
	return Explanation{}, errors.New("read verdicts: store closed")
}

// TestDecisionFirewallView: the explanation says whether the firewall
// applies the range and, if not, why.
func TestDecisionFirewallView(t *testing.T) {
	observe := &FirewallPass{Mode: "observe", At: decisionsNow.Add(-time.Minute)}
	block := func(cov Coverage) Explanation {
		return Explanation{Range: pfx("203.0.113.7/32"), State: StateBlock, EvaluatedAt: decisionsNow, Kept: true,
			KeptAt: decisionsNow.Add(-time.Hour), ExpiresAt: decisionsNow.Add(time.Hour), Firewall: cov}
	}
	fresh := block(Coverage{})
	fresh.Kept = false
	for _, tc := range []struct {
		name  string
		ex    Explanation
		fw    Firewall
		mode  string
		label string
		text  string
	}{
		{"no pass", block(Coverage{}), Firewall{}, "enforce", "Not known yet", "The firewall has not run an enforcement pass yet."},
		{"observe", block(Coverage{}), Firewall{Pass: observe}, "observe", "Not applied", "The node is in observe mode: the firewall applies nothing, by design."},
		{"switched to observe", block(Coverage{Applied: true, Entry: pfx("203.0.113.7/32")}), Firewall{Pass: enforcingPass}, "observe",
			"Not applied", "The node is in observe mode"},
		{"own entry", block(Coverage{Applied: true, Entry: pfx("203.0.113.7/32")}), Firewall{Pass: enforcingPass}, "enforce",
			"Applied", "The firewall's own entry for it drops its traffic."},
		{"a wider block wins", Explanation{Range: pfx("203.0.113.7/32"), State: StateAllowed,
			Firewall: Coverage{Applied: true, Entry: pfx("203.0.113.0/24")}}, Firewall{Pass: enforcingPass}, "enforce",
			"Applied", "It is not a block, yet the firewall drops its traffic: the entry for the wider network 203.0.113.0/24, a block, contains it and wins."},
		{"refused", block(Coverage{Skipped: SkipAllowlist, Within: pfx("203.0.113.7/32")}), Firewall{Pass: enforcingPass}, "enforce",
			"Refused", "The allow-list refused the block right before it was applied"},
		{"capped", block(Coverage{Skipped: SkipMaxEntries, Within: pfx("203.0.0.0/16")}),
			Firewall{Pass: enforcingPass, Facts: EnforceFacts{MaxEntries: 100000}}, "enforce", "Left out",
			"enforce.max_entries (100,000) is reached: the blocks with a higher score, and the operator's force-blocks, take the entries. It is left out with the wider network 203.0.0.0/16."},
		{"deferred", block(Coverage{Deferred: true}), Firewall{Pass: enforcingPass}, "enforce", "Waiting", "It waits until an entry it overlaps expires"},
		{"kept before the pass", block(Coverage{}), Firewall{Pass: enforcingPass}, "enforce", "Not applied yet",
			"Not applied yet: the next pass applies it."},
		{"not kept yet", fresh, Firewall{Pass: enforcingPass}, "enforce", "Not applied yet",
			"Not applied yet: decided after the last pass; the next one applies it."},
		{"not a block", Explanation{Range: pfx("203.0.113.7/32"), State: StateNone}, Firewall{Pass: enforcingPass}, "enforce",
			"Not applied", "It is not a block, and no firewall entry covers it."},
		{"not a block inside a capped network", Explanation{Range: pfx("203.0.113.7/32"), State: StateNone,
			Firewall: Coverage{Skipped: SkipMaxEntries, Within: pfx("203.0.0.0/16")}}, Firewall{Pass: enforcingPass}, "enforce",
			"Not applied", "It is not a block, and no firewall entry covers it."},
	} {
		v := newFirewallView(&tc.ex, &tc.fw, tc.mode, decisionsNow)
		if v.Label != tc.label || !strings.HasPrefix(v.Text, tc.text) {
			t.Errorf("%s: %q %q\nwant %q %q", tc.name, v.Label, v.Text, tc.label, tc.text)
		}
	}
	failing := Firewall{Pass: enforcingPass, Facts: EnforceFacts{Failures: 3, Err: "netlink: operation not permitted"}}
	ex := block(Coverage{})
	if v := newFirewallView(&ex, &failing, "enforce", decisionsNow); v.Pass !=
		"Last enforcement pass: 2026-09-28 11:59:00 UTC, in enforce mode. The passes since failed: netlink: operation not permitted." {
		t.Errorf("Pass = %q", v.Pass)
	}
}

func TestVerdictAndRulingViews(t *testing.T) {
	zero := Contribution{PeerID: idCharlie, Action: "ban", Weight: 0}
	if v := newVerdictView(&zero, testNode.PeerID, map[string]string{idCharlie: "charlie"}); v.Publisher != "charlie" ||
		v.Counts != "No: weight 0" || v.WeightFrom != "trust.default_weight" || v.Href != "/peers/"+idCharlie {
		t.Errorf("verdict of a zero weight = %+v", v)
	}
	for _, tc := range []struct {
		r    Ruling
		rule string
	}{
		{Ruling{Rule: ruleAllowlist, Source: "config"}, "Allow-list"},
		{Ruling{Rule: ruleAllowlist, Source: "bootstrap", Protected: true}, "Protected address"},
		{Ruling{Rule: ruleForceAllow}, "Force-allow override"},
		{Ruling{Rule: ruleForceBlock}, "Force-block override"},
		{Ruling{}, ""},
	} {
		if v := newRulingView(&tc.r); v.Rule != tc.rule || v.Text == "" {
			t.Errorf("ruling %+v = %+v", tc.r, v)
		}
	}
	for _, tc := range []struct {
		ex   Explanation
		want string
	}{
		{Explanation{Autoblock: true}, "Yes: this node's own ban verdict blocks it alone"},
		{Explanation{LocalAutoblock: true}, "No: on"},
		{Explanation{}, "No: off"},
	} {
		if got := autoblockText(&tc.ex); !strings.HasPrefix(got, tc.want) {
			t.Errorf("autoblockText = %q, want %q", got, tc.want)
		}
	}
}
