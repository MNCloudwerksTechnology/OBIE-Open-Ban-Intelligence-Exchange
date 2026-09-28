package console

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testEntries are four entries: one as decided, one whose expiry differs
// from the decision, one on an allowed range and one the node holds no
// decision on.
var testEntries = []FirewallEntry{
	{Range: pfx("198.51.100.0/24"), Expires: decisionsNow.Add(time.Hour), Key: "cidr:198.51.100.0/24", State: StateBlock,
		ExpiresAt: decisionsNow.Add(time.Hour + 2*time.Second)},
	{Range: pfx("203.0.113.0/24"), Expires: decisionsNow.Add(time.Hour), Key: "cidr:203.0.113.0/24", State: StateBlock,
		ExpiresAt: decisionsNow.Add(2 * time.Hour)},
	{Range: pfx("203.0.114.9/32"), Expires: decisionsNow.Add(time.Minute), Key: "ipv4:203.0.114.9", State: StateAllowed},
	{Range: pfx("2001:db8::7/128"), Expires: decisionsNow.Add(time.Minute)},
}

// firewallNode makes c show testEntries in enforce mode, with one decided
// block not applied.
func firewallNode(c *Console) *fakeDecisions {
	src := decisionsNode(c)
	c.node.Mode = func() string { return "enforce" }
	src.entries = testEntries
	src.firewall.Facts = EnforceFacts{Backend: "nftables", MaxEntries: 100000, Mode: "enforce", Applied: 3, Blocks: 5, Covered: 1,
		Capped: 1}
	src.firewall.Pass = &FirewallPass{Mode: "enforce", At: decisionsNow.Add(-time.Minute), Entries: 4, Seq: 7}
	missing := testItems[0]
	missing.Firewall = Coverage{Skipped: SkipMaxEntries, Within: missing.Range}
	src.page = DecisionPage{Items: []DecisionItem{missing}, Total: 1}
	return src
}

func TestFirewallSummary(t *testing.T) {
	pass := &FirewallPass{Mode: "enforce", At: decisionsNow.Add(-time.Minute), Entries: 12, Deferred: 1}
	for _, tc := range []struct {
		name  string
		mode  string
		fw    Firewall
		state string
		title string
	}{
		{"no pass", "enforce", Firewall{}, stateWaiting, "The firewall has not run an enforcement pass yet."},
		{"observe", "observe", Firewall{Pass: &FirewallPass{Mode: "observe"}}, stateWaiting, "Observe mode: the firewall applies nothing, by design."},
		{"switched to observe", "observe", Firewall{Pass: pass}, stateWaiting, "Observe mode: the firewall applies nothing, by design."},
		{"failing", "enforce", Firewall{Pass: pass, Facts: EnforceFacts{Failures: 2, Err: "netlink: operation not permitted",
			RetryIn: 4 * time.Second}}, stateStopped, "Enforcement is failing: failed 2 times in a row."},
		{"failing from the start", "enforce", Firewall{Facts: EnforceFacts{Failures: 1, Err: "nft: no such table"}}, stateStopped,
			"Enforcement is failing: failed."},
		{"skipped", "enforce", Firewall{Pass: pass, Facts: EnforceFacts{Blocks: 15, Refused: 1, Capped: 2}}, stateAttention,
			"12 entries applied; 3 decided blocks are not."},
		{"fine", "enforce", Firewall{Pass: pass, Facts: EnforceFacts{Blocks: 14, Covered: 2}}, stateOK, "12 entries applied for 14 decided blocks."},
	} {
		s := buildFirewallSummary(decisionsNow, tc.mode, &tc.fw)
		if s.Summary.State != tc.state || s.Summary.Title != tc.title {
			t.Errorf("%s: %+v", tc.name, s.Summary)
		}
	}
	failing := Firewall{Pass: pass, Facts: EnforceFacts{Failures: 2, Err: "netlink: operation not permitted", RetryIn: 4 * time.Second}}
	if s := buildFirewallSummary(decisionsNow, "enforce", &failing); s.Summary.Text !=
		"netlink: operation not permitted The next attempt is in 4 s. The numbers below are those of the last successful pass." {
		t.Errorf("failing text = %q", s.Summary.Text)
	}
	fine := Firewall{Pass: pass, Facts: EnforceFacts{Backend: "dryrun", MaxEntries: 100000, Blocks: 14, Covered: 2, Refused: 1}}
	var facts []string
	for _, f := range buildFirewallSummary(decisionsNow, "enforce", &fine).Facts {
		facts = append(facts, f.Label+"="+f.Value)
	}
	if got := strings.Join(facts, " "); got != "Mode=Enforce Backend=dryrun Entry limit=100,000 Last pass=2026-09-28 11:59:00 UTC "+
		"Entries applied=12 Decided blocks=14 Covered=2 Refused=1 Left out=0 Waiting=1" {
		t.Errorf("facts = %s", got)
	}
	observe := Firewall{Pass: &FirewallPass{Mode: "observe", At: decisionsNow}}
	if n := len(buildFirewallSummary(decisionsNow, "observe", &observe).Facts); n != 4 {
		t.Errorf("observe mode shows %d facts, want the settings and the pass", n)
	}
}

// TestFirewallDifferences: AC5 — every difference between the decided
// blocks and the applied entries.
func TestFirewallDifferences(t *testing.T) {
	missing := testItems[0]
	missing.Firewall = Coverage{Skipped: SkipAllowlist, Within: missing.Range}
	fw := Firewall{Pass: enforcingPass, ExpiryTolerance: 5 * time.Second}
	p := buildFirewall(firewallInput{now: decisionsNow, mode: "enforce", firewall: fw, entries: testEntries,
		missing: DecisionPage{Items: []DecisionItem{missing}, Total: 25}})
	if p.Same || p.Observe != "" {
		t.Errorf("page = %+v", p)
	}
	for _, tc := range []struct {
		name string
		got  differences
		want string
	}{
		{"missing", p.Missing, "25 [203.0.113.7 Refused by the allow-list right before apply] " +
			"/decisions?firewall=not_applied&state=block"},
		{"stray", p.Stray, "2 [203.0.114.9 Allowed the node allows this range; the next pass removes the entry | 2001:db8::7 No decision " +
			"the node holds no decision on this range: an entry left behind, or added by hand; the next pass removes it] "},
		{"expiry", p.Expiry, "1 [203.0.113.0/24 Applied until 2026-09-28 13:00:00 UTC decided until 2026-09-28 14:00:00 UTC] "},
	} {
		var rows []string
		for _, r := range tc.got.Rows {
			rows = append(rows, r.Address+" "+r.Label+" "+r.Note)
		}
		if got := fmt.Sprintf("%d [%s] %s", tc.got.Count, strings.Join(rows, " | "), tc.got.More); got != tc.want {
			t.Errorf("%s = %s\nwant %s", tc.name, got, tc.want)
		}
	}
	var differs []string
	for _, e := range p.Entries {
		differs = append(differs, fmt.Sprintf("%s:%v", e.Address, e.Differs))
	}
	if got := strings.Join(differs, " "); got != "198.51.100.0/24:false 203.0.113.0/24:true 203.0.114.9:true 2001:db8::7:true" {
		t.Errorf("entries = %s", got)
	}

	same := buildFirewall(firewallInput{now: decisionsNow, mode: "enforce", firewall: fw, entries: testEntries[:1]})
	if !same.Same || same.Missing.Count+same.Stray.Count+same.Expiry.Count != 0 {
		t.Errorf("no difference = %+v", same)
	}
	failed := buildFirewall(firewallInput{now: decisionsNow, mode: "enforce", firewall: fw, entriesErr: errors.New("netlink: busy")})
	if failed.Same || failed.EntriesErr != "netlink: busy" {
		t.Errorf("unreadable entries = %+v", failed)
	}
}

// TestFirewallObserve: in observe mode the view says that nothing is
// applied by design, and compares nothing.
func TestFirewallObserve(t *testing.T) {
	fw := Firewall{Pass: &FirewallPass{Mode: "observe", At: decisionsNow}}
	p := buildFirewall(firewallInput{now: decisionsNow, mode: "observe", firewall: fw,
		missing: DecisionPage{Items: testItems[:1], Total: 3}})
	if !strings.HasPrefix(p.Observe, "Observe mode: the firewall applies nothing, by design.") || p.Missing.Count != 0 || p.Same {
		t.Errorf("observe page = %+v", p)
	}
}

func TestPageEntries(t *testing.T) {
	rows := make([]entryRow, 120)
	for i := range rows {
		rows[i].Address = fmt.Sprint(i)
	}
	shown, pg := pageEntries(rows, 2)
	if len(shown) != 50 || shown[0].Address != "50" || pg != (pager{Text: "Entries 51–100 of 120, page 2 of 3",
		Prev: "/enforcement#entries", Next: "/enforcement?page=3#entries"}) {
		t.Errorf("page 2 = %d rows from %s, %+v", len(shown), shown[0].Address, pg)
	}
	if shown, pg := pageEntries(rows, 99); len(shown) != 20 || pg.Next != "" || pg.Prev != "/enforcement?page=2#entries" {
		t.Errorf("a page beyond the last = %d rows, %+v", len(shown), pg)
	}
	if shown, pg := pageEntries(rows[:3], 0); len(shown) != 3 || pg != (pager{Text: "3 entries"}) {
		t.Errorf("one page = %d rows, %+v", len(shown), pg)
	}
	if shown, pg := pageEntries(nil, 1); len(shown) != 0 || pg != (pager{}) {
		t.Errorf("no entries = %+v", pg)
	}
}

// firewallRegion matches the refreshing region of the firewall view.
var firewallRegion = regexp.MustCompile(`(?s)<div class="refresh" data-refresh="/api/enforcement">\n(.*?)\n</div>\n\n<section`)

// TestFirewallPage: AC5 — the view lists what the backend applies and
// highlights the differences; its summary refreshes.
func TestFirewallPage(t *testing.T) {
	c, b := signedInBrowser(t)
	src := firewallNode(c)
	resp, page := b.get("/enforcement")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /enforcement = %d", resp.StatusCode)
	}
	if got := src.lastQuery(t); got != (DecisionQuery{State: StateBlock, Firewall: FirewallNotApplied, Limit: differencesShown}) {
		t.Errorf("query of the blocks not applied = %+v", got)
	}
	wantAll(t, "the firewall view", page,
		"<title>Firewall · OBIE console</title>",
		"<h1>Firewall</h1>",
		`<div class="refresh" data-refresh="/api/enforcement">`,
		`<strong class="summary-title">4 entries applied; 1 decided block is not.</strong>`,
		`<dt>Backend</dt>`+"\n  "+`<dd>nftables <span class="cell-note">enforce.backend</span></dd>`,
		`<dd><a href="/decisions?state=block">5</a> <span class="cell-note">that the pass considered</span></dd>`,
		`<h3>1 decided block is not applied</h3>`,
		`<li><a class="mono" href="/decisions/203.0.113.7" data-copy>203.0.113.7</a>: Left out, over enforce.max_entries</li>`,
		`<h3>2 entries have no decided block</h3>`,
		`<h3>1 entry expires at another time than decided</h3>`,
		`<th scope="row"><a class="mono address" href="/decisions/2001:db8::7" data-copy>2001:db8::7</a></th>`,
		`No decision <span class="badge">Differs</span>`,
		`<p class="pager-text">4 entries</p>`,
	)
	if nav := navOf(t, page); len(nav) < 4 || nav[3] != (navItem{Path: "/enforcement", Title: "Firewall", Current: "page"}) {
		t.Errorf("navigation = %+v, want Firewall after Decisions, marked", nav)
	}
	m := firewallRegion.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no refreshing region:\n%s", page)
	}
	if _, fragment := b.get("/api/enforcement"); fragment != m[1] {
		t.Errorf("fragment differs from the page's region\nfragment:\n%s\nregion:\n%s", fragment, m[1])
	}

	src.entriesErr, src.entries = errors.New("netlink: operation not permitted"), nil
	_, page = b.get("/enforcement")
	wantAll(t, "the firewall view with unreadable entries", page,
		`<p class="callout" data-level="warning">The firewall's entries could not be read: netlink: operation not permitted</p>`)

	c.node.Mode = func() string { return "observe" }
	src.entriesErr = nil
	_, page = b.get("/enforcement")
	wantAll(t, "the firewall view in observe mode", page,
		`<strong class="summary-title">Observe mode: the firewall applies nothing, by design.</strong>`,
		`<p class="callout">Observe mode: the firewall applies nothing, by design.`,
		`<p class="empty">The backend applies no entry: observe mode, by design.</p>`)

	c.node.Decisions = nil
	if resp, page := b.get("/enforcement"); resp.StatusCode != http.StatusOK || !strings.Contains(page, "has not run an enforcement pass yet") {
		t.Errorf("GET /enforcement without a source = %d", resp.StatusCode)
	}
}
