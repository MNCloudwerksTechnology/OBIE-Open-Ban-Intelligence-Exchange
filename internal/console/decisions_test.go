package console

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// fakeDecisions is a DecisionSource that returns fixed data and records
// the queries it answers.
type fakeDecisions struct {
	mu         sync.Mutex
	queries    []DecisionQuery
	page       DecisionPage
	generation uint64
	categories map[string]int
	// explained are the explanations by range; another range is no
	// indicator.
	explained  map[netip.Prefix]Explanation
	explains   int
	firewall   Firewall
	entries    []FirewallEntry
	entriesErr error
}

func (f *fakeDecisions) Decisions(q DecisionQuery) DecisionPage {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	return f.page
}

func (f *fakeDecisions) Generation() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.generation
}

func (f *fakeDecisions) Categories() map[string]int { return f.categories }

func (f *fakeDecisions) Explain(p netip.Prefix) (Explanation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.explains++
	ex, ok := f.explained[p]
	if !ok {
		return Explanation{}, fmt.Errorf("%w: %q is broader than /16", ErrNoIndicator, p.String())
	}
	return ex, nil
}

func (f *fakeDecisions) Firewall() Firewall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.firewall
}

func (f *fakeDecisions) FirewallEntries(context.Context) ([]FirewallEntry, error) {
	return f.entries, f.entriesErr
}

// lastQuery returns the query the source answered last.
func (f *fakeDecisions) lastQuery(t *testing.T) DecisionQuery {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queries) == 0 {
		t.Fatal("no decisions were read")
	}
	return f.queries[len(f.queries)-1]
}

var (
	decisionsNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	pfx          = netip.MustParsePrefix
	// enforcingPass is a pass in enforce mode a minute ago.
	enforcingPass = &FirewallPass{Mode: "enforce", At: decisionsNow.Add(-time.Minute), Entries: 2, Seq: 7}
	// testItems are an address blocked by consensus inside a force-blocked
	// network, a protected IPv6 address and an address below consensus.
	testItems = []DecisionItem{
		{Key: "ipv4:203.0.113.7", Range: pfx("203.0.113.7/32"), State: StateBlock, Score: 2.4000000000000004, Threshold: 1.8,
			Contributors: 3, Quorum: 2, Categories: []string{"password_bruteforce/ssh", "port_scan/tcp"}, Verdicts: 3,
			DecidedAt: decisionsNow.Add(-2 * time.Minute), ExpiresAt: decisionsNow.Add(2 * time.Hour), Cursor: "decided:1,ipv4:203.0.113.7",
			Firewall: Coverage{Applied: true, Entry: pfx("203.0.113.0/24"), EntryExpires: decisionsNow.Add(time.Hour)}},
		{Key: "cidr:203.0.113.0/24", Range: pfx("203.0.113.0/24"), State: StateBlock, Threshold: 1.8, Quorum: 2,
			Rule: ruleForceBlock, Source: "override", DecidedAt: decisionsNow.Add(-3 * time.Minute), ExpiresAt: decisionsNow.Add(time.Hour),
			Firewall: Coverage{Applied: true, Entry: pfx("203.0.113.0/24"), EntryExpires: decisionsNow.Add(time.Hour)}},
		{Key: "ipv6:2001:db8::1", Range: pfx("2001:db8::1/128"), State: StateAllowed, Score: 1, Threshold: 1.8, Contributors: 1,
			Quorum: 2, Rule: ruleAllowlist, Source: "self", Categories: []string{"http_probe"}, Verdicts: 1,
			DecidedAt: decisionsNow.Add(-4 * time.Minute)},
		{Key: "ipv4:198.51.100.9", Range: pfx("198.51.100.9/32"), State: StateNone, Score: 0.5, Threshold: 1.8, Contributors: 1,
			Quorum: 2, Categories: []string{"port_scan/tcp"}, Verdicts: 1, DecidedAt: decisionsNow.Add(-5 * time.Minute),
			Cursor: "decided:5,ipv4:198.51.100.9"},
	}
	testDecisionPage = DecisionPage{Items: testItems, Total: 4, States: map[string]int{StateBlock: 2, StateAllowed: 1, StateNone: 1},
		Generation: 41}
)

// decisionsNode makes c show the test decisions, with a running engine
// and a firewall in enforce mode.
func decisionsNode(c *Console) *fakeDecisions {
	src := &fakeDecisions{
		page:       testDecisionPage,
		generation: 41,
		categories: map[string]int{"password_bruteforce/ssh": 1, "port_scan/tcp": 2, "http_probe": 1},
		explained: map[netip.Prefix]Explanation{
			pfx("203.0.113.7/32"): {Key: "ipv4:203.0.113.7", Range: pfx("203.0.113.7/32"), State: StateBlock, Score: 2.4,
				Threshold: 1.8, Contributors: 3, Quorum: 2, ExpiresAt: decisionsNow.Add(2 * time.Hour), Verdicts: make([]Contribution, 3)},
			pfx("192.0.2.1/32"): {Key: "ipv4:192.0.2.1", Range: pfx("192.0.2.1/32"), State: StateAllowed,
				Ruling: Ruling{Effect: "allow", Rule: ruleAllowlist, Source: "builtin", Protected: true,
					Reason: "allow-listed: 192.0.2.0/24 (TEST-NET-1, built-in)"}},
		},
		firewall: Firewall{Facts: EnforceFacts{Backend: "nftables", MaxEntries: 100000, Mode: "enforce", Applied: 2},
			Pass: enforcingPass, ExpiryTolerance: 5 * time.Second},
	}
	c.now = func() time.Time { return decisionsNow }
	c.node.Status = func() []lifecycle.Status { return runningStatuses() }
	c.node.Peers = func() PeerSet { return testPeers }
	c.node.Decisions = src
	return src
}

func TestParseDecisionsQuery(t *testing.T) {
	long := strings.Repeat("x", maxQueryValue+1)
	for _, tc := range []struct {
		query string
		want  decisionsQuery
	}{
		{"", decisionsQuery{sort: sortDecided}},
		{"state=block&firewall=not_applied&sort=score&reason=port_scan/tcp&publisher=" + idAlpha + "&q=+203.0.113.7+&after=c1&last=1",
			decisionsQuery{state: StateBlock, firewall: FirewallNotApplied, sort: sortScore, reason: "port_scan/tcp",
				publisher: idAlpha, search: "203.0.113.7", after: "c1", last: true}},
		{"state=blocked&firewall=maybe&sort=reason&last=yes", decisionsQuery{sort: sortDecided}},
		{"q=" + long + "&reason=" + long + "&before=" + long, decisionsQuery{sort: sortDecided}},
	} {
		v, _ := url.ParseQuery(tc.query)
		if got := parseDecisionsQuery(v); got != tc.want {
			t.Errorf("parseDecisionsQuery(%q) = %+v, want %+v", tc.query, got, tc.want)
		}
	}
	q := decisionsQuery{state: StateBlock, sort: sortAddress, search: "198.51.100.0/24", after: "address:,cidr:198.51.100.0/24"}
	if got := q.href("/decisions"); got != "/decisions?after=address%3A%2Ccidr%3A198.51.100.0%2F24&q=198.51.100.0%2F24&sort=address&state=block" {
		t.Errorf("href = %s", got)
	}
	v, _ := url.ParseQuery(strings.TrimPrefix(q.href(""), "?"))
	if got := parseDecisionsQuery(v); got != q {
		t.Errorf("round trip = %+v, want %+v", got, q)
	}
	if got := (decisionsQuery{sort: sortDecided}).href("/decisions"); got != "/decisions" {
		t.Errorf("default href = %s", got)
	}
}

func TestParseRange(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"203.0.113.7", "203.0.113.7/32"},
		{" 2001:DB8::1 ", "2001:db8::1/128"},
		{"198.51.100.7/24", "198.51.100.0/24"},
		{"ipv4:203.0.113.7", "203.0.113.7/32"},
		{"ipv6:2001:db8::1", "2001:db8::1/128"},
		{"cidr:2001:db8::/48", "2001:db8::/48"},
		{"10.0.0.0/8", "10.0.0.0/8"},
	} {
		if got, err := parseRange(tc.in); err != nil || got != pfx(tc.want) {
			t.Errorf("parseRange(%q) = %v, %v, want %s", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"", "example.org", "203.0.113.300", "fe80::1%eth0", "ipv4:", "cidr:x"} {
		if got, err := parseRange(in); err == nil {
			t.Errorf("parseRange(%q) = %v, want an error", in, got)
		}
	}
	if rangeText(pfx("203.0.113.7/32")) != "203.0.113.7" || rangeText(pfx("2001:db8::/48")) != "2001:db8::/48" ||
		decisionHref(pfx("198.51.100.0/24")) != "/decisions/198.51.100.0/24" {
		t.Error("rangeText or decisionHref")
	}
}

// TestDecisionRows: AC1 — state, score against threshold, publishers
// against quorum, reason, when decided and when it expires, and the
// firewall.
func TestDecisionRows(t *testing.T) {
	fw := Firewall{Pass: enforcingPass}
	p := buildDecisions(decisionsInput{now: decisionsNow, query: decisionsQuery{sort: sortDecided}, page: testDecisionPage, firewall: fw})
	if len(p.Rows) != 4 {
		t.Fatalf("rows = %+v", p.Rows)
	}
	for _, tc := range []struct {
		name      string
		got, want decisionRow
	}{
		{"consensus", p.Rows[0], decisionRow{Href: "/decisions/203.0.113.7", Address: "203.0.113.7", State: StateBlock,
			StateLabel: "Block", StateNote: "consensus", Score: "2.4", ScoreNote: "threshold 1.8: reached",
			Publishers: "3", PublishersNote: "quorum 2: reached", Reasons: []string{"password_bruteforce (ssh)", "port_scan (tcp)"},
			ReasonsNote: "3 active verdicts", Decided: stamp(decisionsNow.Add(-2 * time.Minute)),
			Expires: stamp(decisionsNow.Add(2 * time.Hour)), FirewallState: stateReady, FirewallLabel: "Applied",
			FirewallNote: "through the entry for 203.0.113.0/24"}},
		{"force-block", p.Rows[1], decisionRow{Href: "/decisions/203.0.113.0/24", Address: "203.0.113.0/24", State: StateBlock,
			StateLabel: "Block", StateNote: "operator force-block", Score: "0", ScoreNote: "threshold 1.8: not reached",
			Publishers: "0", PublishersNote: "quorum 2: not reached", ReasonsNote: "0 active verdicts",
			Decided: stamp(decisionsNow.Add(-3 * time.Minute)), Expires: stamp(decisionsNow.Add(time.Hour)),
			FirewallState: stateReady, FirewallLabel: "Applied", FirewallNote: "its own entry"}},
		{"protected", p.Rows[2], decisionRow{Href: "/decisions/2001:db8::1", Address: "2001:db8::1", State: StateAllowed,
			StateLabel: "Allowed", StateNote: "protected address", Score: "1", ScoreNote: "threshold 1.8: not reached",
			Publishers: "1", PublishersNote: "quorum 2: not reached", Reasons: []string{"http_probe"}, ReasonsNote: "1 active verdict",
			Decided: stamp(decisionsNow.Add(-4 * time.Minute)), ExpiresNote: "Not a block",
			FirewallState: stateIdle, FirewallLabel: "Not applied", FirewallNote: "not a block"}},
	} {
		if !equalRows(tc.got, tc.want) {
			t.Errorf("%s row = %+v\nwant %+v", tc.name, tc.got, tc.want)
		}
	}
	if p.Rows[3].StateNote != "below consensus" {
		t.Errorf("state note = %q", p.Rows[3].StateNote)
	}
}

func equalRows(a, b decisionRow) bool {
	return slices.Equal(a.Reasons, b.Reasons) && reflect.DeepEqual(a, b)
}

// TestFirewallCell: the list says whether the firewall applies a decision
// and, if not, why.
func TestFirewallCell(t *testing.T) {
	observe := &FirewallPass{Mode: "observe", At: decisionsNow.Add(-time.Minute)}
	block := func(cov Coverage) DecisionItem {
		return DecisionItem{Range: pfx("203.0.113.7/32"), State: StateBlock, DecidedAt: decisionsNow.Add(-time.Hour),
			ExpiresAt: decisionsNow.Add(time.Hour), Firewall: cov}
	}
	none := DecisionItem{Range: pfx("203.0.113.7/32"), State: StateNone, Firewall: Coverage{Applied: true, Entry: pfx("203.0.113.0/24")}}
	late := block(Coverage{})
	late.DecidedAt = decisionsNow
	dying := block(Coverage{})
	dying.ExpiresAt = decisionsNow.Add(500 * time.Millisecond)
	for _, tc := range []struct {
		name               string
		it                 DecisionItem
		pass               *FirewallPass
		state, label, note string
	}{
		{"before the first pass", block(Coverage{}), nil, stateIdle, "Not known yet", "no enforcement pass has run yet"},
		{"observe mode", block(Coverage{}), observe, stateIdle, "Not applied", "observe mode: nothing is applied, by design"},
		{"observe mode, no block", DecisionItem{State: StateNone}, observe, stateIdle, "Not applied", "observe mode"},
		{"own entry", block(Coverage{Applied: true, Entry: pfx("203.0.113.7/32")}), enforcingPass, stateReady, "Applied", "its own entry"},
		{"wider entry", block(Coverage{Applied: true, Entry: pfx("203.0.113.0/24")}), enforcingPass, stateReady, "Applied",
			"through the entry for 203.0.113.0/24"},
		{"a wider entry wins over no block", none, enforcingPass, stateWarning, "Applied", "the entry for 203.0.113.0/24 wins"},
		{"refused", block(Coverage{Skipped: SkipAllowlist, Within: pfx("203.0.113.7/32")}), enforcingPass, stateWarning, "Refused",
			"by the allow-list right before apply"},
		{"capped with a wider one", block(Coverage{Skipped: SkipMaxEntries, Within: pfx("203.0.0.0/16")}), enforcingPass,
			stateWarning, "Left out", "over enforce.max_entries, with 203.0.0.0/16"},
		{"deferred", block(Coverage{Deferred: true}), enforcingPass, stateWarning, "Waiting", "until an entry it overlaps expires"},
		{"decided after the pass", late, enforcingPass, stateWarning, "Not applied yet", "decided after the last pass; the next one applies it"},
		{"about to expire", dying, enforcingPass, stateWarning, "Not applied yet", "it expires within a second"},
		{"otherwise", block(Coverage{}), enforcingPass, stateWarning, "Not applied yet", "the next pass applies it"},
		{"no block", DecisionItem{State: StateAllowed}, enforcingPass, stateIdle, "Not applied", "not a block"},
	} {
		fw := Firewall{Pass: tc.pass}
		if state, label, note := firewallCell(&tc.it, &fw, decisionsNow); state != tc.state || label != tc.label || note != tc.note {
			t.Errorf("%s: %q %q %q, want %q %q %q", tc.name, state, label, note, tc.state, tc.label, tc.note)
		}
	}
}

func TestFirewallNote(t *testing.T) {
	for _, tc := range []struct {
		fw   Firewall
		want string
	}{
		{Firewall{}, "The firewall has not run an enforcement pass yet"},
		{Firewall{Pass: &FirewallPass{Mode: "observe"}}, "Observe mode: the firewall applies nothing, by design."},
		{Firewall{Pass: enforcingPass, Facts: EnforceFacts{Failures: 2, Err: "netlink: operation not permitted"}},
			"The last enforcement pass failed (netlink: operation not permitted). The firewall column shows the last successful pass, at 2026-09-28 11:59:00 UTC."},
		{Firewall{Pass: enforcingPass}, ""},
	} {
		if got := firewallNote(&tc.fw); (tc.want == "" && got != "") || !strings.Contains(got, tc.want) {
			t.Errorf("firewallNote(%+v) = %q, want %q", tc.fw, got, tc.want)
		}
	}
}

// TestDecisionsPager: pages link to the first, previous, next and last
// page by cursor, keeping the filters and the order.
func TestDecisionsPager(t *testing.T) {
	q := decisionsQuery{state: StateBlock, sort: sortScore, after: "score:2,ipv4:203.0.113.7"}
	items := func(n int) []DecisionItem {
		out := make([]DecisionItem, n)
		for i := range out {
			out[i].Cursor = "c" + string(rune('a'+i%26))
		}
		out[n-1].Cursor = "last-cursor"
		return out
	}
	for _, tc := range []struct {
		name string
		page DecisionPage
		want decisionsPager
	}{
		{"first", DecisionPage{Items: items(50), Total: 120}, decisionsPager{Text: "Decisions 1–50 of 120",
			Next: "/decisions?after=last-cursor&sort=score&state=block", Last: "/decisions?last=1&sort=score&state=block"}},
		{"middle", DecisionPage{Items: items(50), Total: 120, Offset: 50}, decisionsPager{Text: "Decisions 51–100 of 120",
			First: "/decisions?sort=score&state=block", Prev: "/decisions?before=ca&sort=score&state=block",
			Next: "/decisions?after=last-cursor&sort=score&state=block", Last: "/decisions?last=1&sort=score&state=block"}},
		{"last", DecisionPage{Items: items(20), Total: 120, Offset: 100}, decisionsPager{Text: "Decisions 101–120 of 120",
			First: "/decisions?sort=score&state=block", Prev: "/decisions?before=ca&sort=score&state=block"}},
		{"only", DecisionPage{Items: items(3), Total: 3}, decisionsPager{Text: "3 decisions"}},
		{"one", DecisionPage{Items: items(1), Total: 1}, decisionsPager{Text: "1 decision"}},
		{"none", DecisionPage{}, decisionsPager{}},
	} {
		if got := decisionsPagerOf(q, &tc.page); got != tc.want {
			t.Errorf("%s: %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}

func TestEmptyDecisions(t *testing.T) {
	for _, tc := range []struct {
		q    decisionsQuery
		all  int
		want string
	}{
		{decisionsQuery{}, 0, "The node holds no decision"},
		{decisionsQuery{reason: "spam/smtp"}, 0, "No decision matches this view."},
		{decisionsQuery{state: StateAllowed}, 4, "No decision matches this view."},
		{decisionsQuery{after: "decided:1,ipv4:203.0.113.7"}, 4, "No decision follows on this page any more"},
	} {
		if got := emptyDecisions(tc.q, tc.all); !strings.HasPrefix(got, tc.want) {
			t.Errorf("emptyDecisions(%+v, %d) = %q", tc.q, tc.all, got)
		}
	}
}

func TestFilterOptions(t *testing.T) {
	reasons := reasonOptions(map[string]int{"port_scan/tcp": 2, "password_bruteforce/ssh": 2, "http_probe": 7}, "spam/smtp")
	var labels []string
	for _, o := range reasons {
		labels = append(labels, o.Label)
	}
	if want := []string{"Every reason", "http_probe (7)", "password_bruteforce (ssh) (2)", "port_scan (tcp) (2)", "spam (smtp) (0)"}; !slices.Equal(labels, want) {
		t.Errorf("reason options = %v, want %v", labels, want)
	}
	if !reasons[4].Selected || reasons[0].Selected {
		t.Errorf("the selected reason is not kept: %+v", reasons)
	}

	pubs := publisherOptions(testPeers, testNode.PeerID, idAlpha)
	labels = nil
	for _, o := range pubs {
		labels = append(labels, o.Label)
	}
	// This node first, then named publishers by name, then the others.
	if want := []string{"Every publisher", "This node: 9 verdicts", "alpha (12D3KooW…rAa1ph): 120 verdicts",
		"charlie (12D3KooW…iePeer): 1 verdict", "12D3KooW…bOther: 5 verdicts", "12D3KooW…rayPee: 2 verdicts"}; !slices.Equal(labels, want) {
		t.Errorf("publisher options = %v\nwant %v", labels, want)
	}
	if !pubs[2].Selected {
		t.Errorf("alpha is not selected: %+v", pubs)
	}
}

// TestDecisionsPage: AC1, AC2 — the list with its filters, search, sort
// links and pager; every filter reaches the decision source, and the view
// is its URL (AC6).
func TestDecisionsPage(t *testing.T) {
	c, b := signedInBrowser(t)
	src := decisionsNode(c)
	src.page.Total, src.page.Offset = 120, 50
	resp, page := b.get("/decisions?q=203.0.113.7&state=block&sort=score&reason=port_scan/tcp&publisher=" + idAlpha +
		"&firewall=applied&after=score:3,ipv4:192.0.2.9")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /decisions = %d", resp.StatusCode)
	}
	want := DecisionQuery{State: StateBlock, Category: "port_scan/tcp", Publisher: idAlpha, Firewall: FirewallApplied,
		Search: pfx("203.0.113.7/32"), Sort: sortScore, After: "score:3,ipv4:192.0.2.9", Limit: decisionsPageSize}
	if got := src.lastQuery(t); got != want {
		t.Errorf("query = %+v\nwant %+v", got, want)
	}
	view := "q=203.0.113.7&amp;reason=port_scan%2Ftcp&amp;sort=score"
	wantAll(t, "the decisions list", page,
		"<title>Decisions · OBIE console</title>",
		"<h1>Decisions</h1>",
		`<div class="refresh" data-refresh="/api/decisions?after=score%3A3%2Cipv4%3A192.0.2.9&amp;firewall=applied&amp;gen=41&amp;publisher=`+
			idAlpha+`&amp;q=203.0.113.7&amp;read=1790596800&amp;reason=port_scan%2Ftcp&amp;seq=7&amp;sort=score&amp;state=block" aria-live="polite">`,
		`<p class="updated">List read at <time datetime="2026-09-28T12:00:00Z">2026-09-28 12:00:00 UTC</time>; no decision changed since.</p>`,
		`<input id="decisions-q" name="q" type="search" value="203.0.113.7"`,
		`<option value="port_scan/tcp" selected>port_scan (tcp) (2)</option>`,
		`<option value="`+idAlpha+`" selected>alpha (12D3KooW…rAa1ph): 120 verdicts</option>`,
		`<option value="applied" selected>Applied by the firewall</option>`,
		`<input type="hidden" name="state" value="block">`,
		`<input type="hidden" name="sort" value="score">`,
		`<a href="/decisions?sort=score&amp;state=block">Clear filters and search</a>`,
		// The searched address, explained in one line (AC4).
		`<p><a class="mono" href="/decisions/203.0.113.7" data-copy>203.0.113.7</a>: <strong>Blocked by consensus until 2026-09-28 14:00:00 UTC</strong>. Not protected: no allow-list entry or override covers it. <a href="/decisions/203.0.113.7">Full explanation</a></p>`,
		// State tabs keep the filters, drop the page.
		`<li><a href="/decisions?firewall=applied&amp;publisher=`+idAlpha+`&amp;`+view+`">All <span class="filter-count">4</span></a></li>`,
		`<li><a href="/decisions?firewall=applied&amp;publisher=`+idAlpha+`&amp;`+view+`&amp;state=block" aria-current="page">Block <span class="filter-count">2</span></a></li>`,
		// Sortable headings; Reason and Firewall do not sort.
		`<th scope="col"><a href="/decisions?firewall=applied&amp;publisher=`+idAlpha+`&amp;q=203.0.113.7&amp;reason=port_scan%2Ftcp&amp;sort=address&amp;state=block">Address</a></th>`,
		`<th scope="col" aria-sort="descending"><a href="/decisions?firewall=applied&amp;publisher=`+idAlpha+`&amp;`+view+`&amp;state=block">Score</a></th>`,
		`<th scope="col">Reason</th>`,
		`<th scope="col">Firewall</th>`,
		// A row.
		`<th scope="row"><a class="mono address" href="/decisions/203.0.113.7" data-copy>203.0.113.7</a></th>`,
		`<span class="decision-state" data-state="block">Block</span>`,
		`<span class="reason">password_bruteforce (ssh)</span>`,
		`<span class="peer-state" data-state="ready">Applied</span>`,
		`<a class="mono address" href="/decisions/203.0.113.0/24" data-copy>203.0.113.0/24</a>`,
		`<a class="mono address" href="/decisions/2001:db8::1" data-copy>2001:db8::1</a>`,
		`<p class="pager-text">Decisions 51–54 of 120</p>`,
		`<a href="/decisions?before=decided%3A1%2Cipv4%3A203.0.113.7&amp;firewall=applied&amp;publisher=`+idAlpha+`&amp;`+view+`&amp;state=block" rel="prev">Previous page</a>`,
		`<a href="/decisions?after=decided%3A5%2Cipv4%3A198.51.100.9&amp;firewall=applied&amp;publisher=`+idAlpha+`&amp;`+view+`&amp;state=block" rel="next">Next page</a>`,
	)
	if nav := navOf(t, page); len(nav) < 3 || nav[2] != (navItem{Path: "/decisions", Title: "Decisions", Current: "page"}) {
		t.Errorf("navigation = %+v, want Decisions after Peers, marked", nav)
	}
}

// TestDecisionsSearch: an address the node knows nothing about is
// explained with its protection (AC4); a network broader than an
// indicator is searched but not explained; text that is no address is
// not searched.
func TestDecisionsSearch(t *testing.T) {
	c, b := signedInBrowser(t)
	src := decisionsNode(c)
	_, page := b.get("/decisions?q=ipv4:192.0.2.1")
	wantAll(t, "a search for a protected address", page,
		`<a class="mono" href="/decisions/192.0.2.1" data-copy>192.0.2.1</a>: <strong>Allowed: never blocked, whatever the verdicts</strong>. Protected: allow-listed: 192.0.2.0/24 (TEST-NET-1, built-in). Nothing blocks it, not even a force-block.`)
	if got := src.lastQuery(t).Search; got != pfx("192.0.2.1/32") {
		t.Errorf("searched %v", got)
	}

	_, page = b.get("/decisions?q=10.0.0.0/8")
	wantAll(t, "a search for a wide network", page,
		`<p><span class="mono">10.0.0.0/8</span> cannot be explained: not an address or network the node decides on: &#34;10.0.0.0/8&#34; is broader than /16</p>`)
	if got := src.lastQuery(t).Search; got != pfx("10.0.0.0/8") {
		t.Errorf("searched %v", got)
	}

	_, page = b.get("/decisions?q=example.org")
	wantAll(t, "a search for no address", page,
		`<p class="callout" data-level="warning">&#34;example.org&#34; is not an IP address or network; the list is not searched</p>`)
	if got := src.lastQuery(t).Search; got.IsValid() {
		t.Errorf("searched %v", got)
	}
}

var refreshAttr = regexp.MustCompile(`data-refresh="([^"]*)"`)

// TestDecisionsStatus: the open list learns cheaply that the decisions
// or the firewall changed since it was read, and links to the same view.
func TestDecisionsStatus(t *testing.T) {
	c, b := signedInBrowser(t)
	src := decisionsNode(c)
	_, page := b.get("/decisions?state=block")
	m := refreshAttr.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no refreshing region:\n%s", page)
	}
	fragment := html.UnescapeString(m[1])
	reads := len(src.queries)
	_, body := b.get(fragment)
	if want := `<p class="updated">List read at <time datetime="2026-09-28T12:00:00Z">2026-09-28 12:00:00 UTC</time>; no decision changed since.</p>`; body != want {
		t.Errorf("fragment = %q, want %q", body, want)
	}
	src.mu.Lock()
	src.generation++
	src.mu.Unlock()
	_, body = b.get(fragment)
	if want := `<p class="callout" data-level="warning">The decisions or the firewall changed since this list was read at <time datetime="2026-09-28T12:00:00Z">2026-09-28 12:00:00 UTC</time>. <a href="/decisions?state=block">Read the list again</a></p>`; body != want {
		t.Errorf("fragment after a change = %q", body)
	}
	src.mu.Lock()
	src.generation--
	src.firewall.Pass = &FirewallPass{Mode: "enforce", Seq: 8}
	src.mu.Unlock()
	if _, body = b.get(fragment); !strings.Contains(body, "changed since") {
		t.Errorf("fragment after a firewall change = %q", body)
	}
	if len(src.queries) != reads {
		t.Error("the fragment read the list")
	}
}

func TestDecisionsWithoutSource(t *testing.T) {
	c, b := signedInBrowser(t)
	c.node.Decisions = nil
	resp, page := b.get("/decisions")
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "The node holds no decision") {
		t.Errorf("GET /decisions without a source = %d:\n%s", resp.StatusCode, page)
	}
	if resp, body := b.get("/api/decisions"); resp.StatusCode != http.StatusOK || !strings.Contains(body, "no decision changed") {
		t.Errorf("GET /api/decisions without a source = %d %q", resp.StatusCode, body)
	}
}

func TestDecisionNotice(t *testing.T) {
	status := func(state lifecycle.State) []lifecycle.Status {
		return []lifecycle.Status{{Name: partDecision, State: state}}
	}
	if decisionNotice(status(lifecycle.StateRunning)) != "" || decisionNotice(nil) != "" ||
		!strings.Contains(decisionNotice(status(lifecycle.StateStarting)), "starting") ||
		!strings.Contains(decisionNotice(status(lifecycle.StateStopped)), "not running") {
		t.Error("decisionNotice")
	}
}

// TestDecisionLinksSurviveSignIn: AC6 — a shared link to a view or an
// explanation opens it after signing in on the same host.
func TestDecisionLinksSurviveSignIn(t *testing.T) {
	c, b := startConsole(t, &syncBuffer{})
	explainNode(c)
	for _, link := range []string{"/decisions?q=198.51.100.0%2F24&sort=address&state=block", "/decisions/198.51.100.0/24",
		"/enforcement?page=2"} {
		b.client.Jar, _ = cookiejar.New(nil)
		resp, _ := b.get(link)
		want := "/login?next=" + url.QueryEscape(link)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Fatalf("GET %s signed out = %d %s, want a redirect to %s", link, resp.StatusCode, resp.Header.Get("Location"), want)
		}
		if resp, _ := b.signIn(c.Token(), link); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != link {
			t.Errorf("sign-in with next %s = %d %s", link, resp.StatusCode, resp.Header.Get("Location"))
		}
		if resp, _ := b.get(link); resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s signed in = %d", link, resp.StatusCode)
		}
	}
}
