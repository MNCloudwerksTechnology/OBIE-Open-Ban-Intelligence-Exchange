package console

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// fakeVerdicts is a VerdictSource that returns fixed data and records the
// queries it answers.
type fakeVerdicts struct {
	mu         sync.Mutex
	queries    []VerdictQuery
	list       VerdictList
	err        error
	totals     VerdictTotals
	totalsErr  error
	categories map[string]int
}

func (f *fakeVerdicts) Verdicts(q VerdictQuery) (VerdictList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	return f.list, f.err
}

func (f *fakeVerdicts) Totals() (VerdictTotals, error) { return f.totals, f.totalsErr }

func (f *fakeVerdicts) Categories() map[string]int { return f.categories }

// lastQuery returns the query the source answered last.
func (f *fakeVerdicts) lastQuery(t *testing.T) VerdictQuery {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queries) == 0 {
		t.Fatal("no verdicts were read")
	}
	return f.queries[len(f.queries)-1]
}

const (
	testLogHash  = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	eventAlpha   = "01926a4c-0000-7000-8000-00000000a001"
	eventSelf    = "01926a4c-0000-7000-8000-00000000f001"
	eventRevoked = "01926a4c-0000-7000-8000-00000000c001"
	eventRevoker = "01926a4c-0000-7000-8000-00000000c002"
)

var (
	verdictsNow = decisionsNow
	// testVerdictItems are a counting verdict of alpha, this node's own on
	// the same address, a watch verdict of an unnamed peer without weight,
	// a revoked verdict of an unknown publisher and an expired one of
	// charlie.
	testVerdictItems = []VerdictItem{
		{Range: pfx("203.0.113.7/32"), EventID: eventAlpha, Publisher: idAlpha, Weight: 0.7, Action: "ban", Confidence: 0.9,
			Reason: "password_bruteforce", Protocol: "ssh", Events: 1234, LogHash: testLogHash,
			IssuedAt: verdictsNow.Add(-time.Hour), ExpiresAt: verdictsNow.Add(23 * time.Hour), State: VerdictActive, Counts: true,
			Decision: StateBlock, Cursor: "ipv4:203.0.113.7," + idAlpha},
		{Range: pfx("203.0.113.7/32"), EventID: eventSelf, Publisher: testNode.PeerID, Local: true, Weight: 1, Action: "ban",
			Confidence: 0.8, Reason: "password_bruteforce", Protocol: "ssh", Events: 3, IssuedAt: verdictsNow.Add(-time.Minute),
			ExpiresAt: verdictsNow.Add(7 * 24 * time.Hour), State: VerdictActive, Counts: true, Decision: StateBlock,
			Cursor: "ipv4:203.0.113.7," + testNode.PeerID},
		{Range: pfx("198.51.100.0/24"), EventID: "01926a4c-0000-7000-8000-00000000b001", Publisher: idStray, Action: "watch",
			Confidence: 0.4, Reason: "port_scan", Protocol: "tcp", Events: 1, IssuedAt: verdictsNow.Add(-2 * time.Hour),
			ExpiresAt: verdictsNow.Add(time.Hour), State: VerdictActive, Decision: StateNone},
		{Range: pfx("2001:db8::1/128"), EventID: eventRevoked, Publisher: idOther, Action: "ban", Confidence: 1,
			Reason: "http_probe", Events: 7, IssuedAt: verdictsNow.Add(-3 * time.Hour), ExpiresAt: verdictsNow.Add(time.Hour),
			State: VerdictRevoked, Revocation: &VerdictRevocation{ID: eventRevoker, Reason: "false_positive", At: verdictsNow.Add(-time.Hour)}},
		{Range: pfx("198.51.100.9/32"), EventID: "01926a4c-0000-7000-8000-00000000d001", Publisher: idCharlie, Weight: 0.5,
			Action: "ban", Confidence: 0.6, Reason: "spam", Protocol: "smtp", Events: 2, IssuedAt: verdictsNow.Add(-26 * time.Hour),
			ExpiresAt: verdictsNow.Add(-2 * time.Hour), State: VerdictExpired},
	}
	testVerdictTotals = VerdictTotals{
		ByPublisher: map[string]VerdictCounts{
			testNode.PeerID: {Active: 9, Counting: 9, Revoked: 2, Expired: 1},
			idAlpha:         {Active: 120, Counting: 118, Expired: 4},
			idCharlie:       {Active: 1, Counting: 1, Expired: 1},
			idStray:         {Active: 2},
			idOther:         {Active: 5, Revoked: 1},
		},
		Retention: 24 * time.Hour,
	}
)

// verdictsNode makes c show the test verdicts, with a running node.
func verdictsNode(c *Console) *fakeVerdicts {
	src := &fakeVerdicts{
		list: VerdictList{Items: testVerdictItems, Total: 5,
			States: map[string]int{VerdictActive: 3, VerdictRevoked: 1, VerdictExpired: 1}},
		totals:     testVerdictTotals,
		categories: map[string]int{"password_bruteforce/ssh": 1, "port_scan/tcp": 1, "http_probe": 1},
	}
	c.now = func() time.Time { return verdictsNow }
	c.node.Status = func() []lifecycle.Status { return runningStatuses() }
	c.node.Peers = func() PeerSet { return testPeers }
	c.node.Verdicts = src
	return src
}

// verdictsOf builds the verdicts view of the test verdicts under the
// query q.
func verdictsOf(q string, list VerdictList) verdictsPage {
	v, _ := url.ParseQuery(q)
	in := verdictsInput{now: verdictsNow, query: parseVerdictsQuery(v), self: testNode.PeerID, peers: testPeers,
		list: list, totals: testVerdictTotals, categories: map[string]int{"password_bruteforce/ssh": 3, "http_probe": 1}}
	if p, err := parseRange(in.query.address); err == nil {
		in.searched = p
	}
	return buildVerdicts(in)
}

func TestParseVerdictsQuery(t *testing.T) {
	long := strings.Repeat("x", maxQueryValue+1)
	for _, tc := range []struct {
		query string
		want  verdictsQuery
	}{
		{"", verdictsQuery{}},
		{"from=mine&state=revoked&reason=port_scan/tcp&address=+203.0.113.7+&after=c1",
			verdictsQuery{from: fromMine, state: VerdictRevoked, reason: "port_scan/tcp", address: "203.0.113.7", after: "c1"}},
		{"from=peers&state=expired", verdictsQuery{from: fromPeers, state: VerdictExpired}},
		// One publisher replaces the scope.
		{"from=peers&publisher=" + idAlpha, verdictsQuery{publisher: idAlpha}},
		{"from=all&state=active&state=gone", verdictsQuery{}},
		{"publisher=" + long + "&address=" + long + "&after=" + long, verdictsQuery{}},
	} {
		v, _ := url.ParseQuery(tc.query)
		if got := parseVerdictsQuery(v); got != tc.want {
			t.Errorf("parseVerdictsQuery(%q) = %+v, want %+v", tc.query, got, tc.want)
		}
	}
	q := verdictsQuery{from: fromPeers, reason: "port_scan/tcp", address: "198.51.100.0/24", state: VerdictExpired, after: "cidr:x,y"}
	if got := verdictsHref(q); got != "/verdicts?address=198.51.100.0%2F24&after=cidr%3Ax%2Cy&from=peers&reason=port_scan%2Ftcp&state=expired" {
		t.Errorf("href = %s", got)
	}
	v, _ := url.ParseQuery(strings.TrimPrefix(q.href(""), "?"))
	if got := parseVerdictsQuery(v); got != q {
		t.Errorf("round trip = %+v, want %+v", got, q)
	}
	if got := verdictsHref(verdictsQuery{}); got != "/verdicts" {
		t.Errorf("default href = %s", got)
	}
}

// TestVerdictRows: AC1, AC4 and the edge cases — every field of a verdict,
// its state, whether it counts, the publisher's weight, and the links to
// the publisher and the decision.
func TestVerdictRows(t *testing.T) {
	p := verdictsOf("", VerdictList{Items: testVerdictItems, Total: 5})
	if len(p.Rows) != 5 {
		t.Fatalf("rows = %+v", p.Rows)
	}
	for _, tc := range []struct {
		name      string
		got, want verdictItemRow
	}{
		{"a counting verdict of a named peer", p.Rows[0], verdictItemRow{Address: "203.0.113.7", Href: "/verdicts?address=203.0.113.7",
			DecisionHref: "/decisions/203.0.113.7", DecisionLabel: "Block", DecisionState: StateBlock,
			Publisher: "alpha", PublisherHref: "/peers/" + idAlpha, Weight: "0.7", Action: "ban", Confidence: "0.9",
			Reason: "password_bruteforce (ssh)", Events: "1,234 events", LogHash: testLogHash, EventID: eventAlpha,
			Issued: stamp(verdictsNow.Add(-time.Hour)), Expires: stamp(verdictsNow.Add(23 * time.Hour)),
			State: VerdictActive, StateLabel: "Active", Counts: "Yes"}},
		{"this node's own verdict", p.Rows[1], verdictItemRow{Address: "203.0.113.7", Href: "/verdicts?address=203.0.113.7",
			DecisionHref: "/decisions/203.0.113.7", DecisionLabel: "Block", DecisionState: StateBlock,
			Publisher: "This node", Weight: "1", Action: "ban", Confidence: "0.8",
			Reason: "password_bruteforce (ssh)", Events: "3 events", EventID: eventSelf, Issued: stamp(verdictsNow.Add(-time.Minute)),
			Expires: stamp(verdictsNow.Add(7 * 24 * time.Hour)), State: VerdictActive, StateLabel: "Active", Counts: "Yes"}},
		{"a watch verdict of a peer without weight", p.Rows[2], verdictItemRow{Address: "198.51.100.0/24",
			Href: "/verdicts?address=198.51.100.0%2F24", DecisionHref: "/decisions/198.51.100.0/24", DecisionLabel: "None",
			DecisionState: StateNone, Publisher: shortPeerID(idStray), PublisherHref: "/peers/" + idStray,
			Weight: "0", NoWeight: true, Action: "watch", Confidence: "0.4", Reason: "port_scan (tcp)", Events: "1 event",
			EventID: "01926a4c-0000-7000-8000-00000000b001", Issued: stamp(verdictsNow.Add(-2 * time.Hour)),
			Expires: stamp(verdictsNow.Add(time.Hour)), State: VerdictActive, StateLabel: "Active", Counts: "No: a watch verdict"}},
		{"a revoked verdict", p.Rows[3], verdictItemRow{Address: "2001:db8::1", Href: "/verdicts?address=2001%3Adb8%3A%3A1",
			DecisionHref: "/decisions/2001:db8::1", DecisionLabel: "No decision", DecisionState: stateIdle,
			Publisher: shortPeerID(idOther), PublisherHref: "/peers/" + idOther, Weight: "0", NoWeight: true,
			Action: "ban", Confidence: "1", Reason: "http_probe", Events: "7 events", EventID: eventRevoked,
			Issued: stamp(verdictsNow.Add(-3 * time.Hour)), Expires: stamp(verdictsNow.Add(time.Hour)), State: VerdictRevoked,
			StateLabel: "Revoked", Ended: stamp(verdictsNow.Add(-time.Hour)), RevokeReason: "false_positive", RevokedByID: eventRevoker}},
		{"an expired verdict", p.Rows[4], verdictItemRow{Address: "198.51.100.9", Href: "/verdicts?address=198.51.100.9",
			DecisionHref: "/decisions/198.51.100.9", DecisionLabel: "No decision", DecisionState: stateIdle,
			Publisher: "charlie", PublisherHref: "/peers/" + idCharlie, Weight: "0.5", Action: "ban",
			Confidence: "0.6", Reason: "spam (smtp)", Events: "2 events", EventID: "01926a4c-0000-7000-8000-00000000d001",
			Issued: stamp(verdictsNow.Add(-26 * time.Hour)), Expires: stamp(verdictsNow.Add(-2 * time.Hour)), State: VerdictExpired,
			StateLabel: "Expired", Ended: stamp(verdictsNow.Add(-2 * time.Hour))}},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s row = %+v\nwant %+v", tc.name, tc.got, tc.want)
		}
	}

	// A verdict of a publisher with weight 0 that counts nowhere says why.
	ban := testVerdictItems[0]
	ban.Publisher, ban.Weight, ban.Counts = idBravo, 0, false
	if r := newVerdictItemRow(&ban, testNode.PeerID, peerNames(testPeers)); r.Counts != "No: weight 0" || !r.NoWeight ||
		r.Publisher != "Bravo" {
		t.Errorf("row of a publisher without weight = %+v", r)
	}
	// A revoked verdict whose revocation is not known.
	unknown := testVerdictItems[3]
	unknown.Revocation = nil
	if r := newVerdictItemRow(&unknown, testNode.PeerID, nil); r.StateLabel != "Revoked" || r.RevokeReason != "" || r.Ended.Text != "" {
		t.Errorf("row of a revocation not known = %+v", r)
	}
}

// TestVerdictsTabs: AC1, AC2 — the scopes and the states, each state with
// how many verdicts match, the filters kept by every tab.
func TestVerdictsTabs(t *testing.T) {
	list := VerdictList{Items: testVerdictItems[:1], Total: 1, States: map[string]int{VerdictActive: 1, VerdictRevoked: 2}}
	p := verdictsOf("from=peers&reason=port_scan/tcp&after=c1", list)
	var scopes, states []string
	for _, s := range p.Scopes {
		scopes = append(scopes, fmt.Sprintf("%s %v %s", s.Label, s.Current, s.Href))
	}
	for _, s := range p.States {
		states = append(states, fmt.Sprintf("%s %d %v %s", s.Label, s.Count, s.Current, s.Href))
	}
	if want := []string{
		"All publishers false /verdicts?reason=port_scan%2Ftcp",
		"This node false /verdicts?from=mine&reason=port_scan%2Ftcp",
		"Received true /verdicts?from=peers&reason=port_scan%2Ftcp",
	}; !slices.Equal(scopes, want) {
		t.Errorf("scopes = %v\nwant %v", scopes, want)
	}
	if want := []string{
		"Active 1 true /verdicts?from=peers&reason=port_scan%2Ftcp",
		"Revoked 2 false /verdicts?from=peers&reason=port_scan%2Ftcp&state=revoked",
		"Expired 0 false /verdicts?from=peers&reason=port_scan%2Ftcp&state=expired",
	}; !slices.Equal(states, want) {
		t.Errorf("states = %v\nwant %v", states, want)
	}
	if p.Clear != "/verdicts?from=peers" || p.Heading != "Active verdicts received from other publishers" {
		t.Errorf("clear %q, heading %q", p.Clear, p.Heading)
	}
	if p.Reasons[0].Label != "Every reason" || p.Reasons[1].Label != "password_bruteforce (ssh)" ||
		p.Reasons[len(p.Reasons)-1].Label != "port_scan (tcp)" || !p.Reasons[len(p.Reasons)-1].Selected {
		t.Errorf("reasons = %+v, want no counts and the selected one kept", p.Reasons)
	}

	// One publisher: no scope is current, and the page names it.
	p = verdictsOf("publisher="+idStray+"&state=expired", list)
	for _, s := range p.Scopes {
		if s.Current {
			t.Errorf("scope %s is current while one publisher is listed", s.Label)
		}
	}
	want := &whoseLine{Title: shortPeerID(idStray), PeerID: idStray, Href: "/peers/" + idStray, Weight: "0", NoWeight: true}
	if !reflect.DeepEqual(p.Whose, want) || p.Heading != "Expired verdicts of "+shortPeerID(idStray) {
		t.Errorf("whose = %+v, heading %q", p.Whose, p.Heading)
	}
	if p.Scopes[1].Href != "/verdicts?from=mine&state=expired" {
		t.Errorf("a scope keeps the publisher: %s", p.Scopes[1].Href)
	}
	p = verdictsOf("publisher="+testNode.PeerID, list)
	if p.Whose.Title != "This node" || p.Whose.Href != "" || p.Whose.NoWeight || p.Whose.Weight != "1" ||
		p.Heading != "Active verdicts of this node" {
		t.Errorf("this node as the publisher: %+v, %q; want its weight trust.local_weight", p.Whose, p.Heading)
	}
	for q, heading := range map[string]string{
		"":                               "Active verdicts of every publisher",
		"from=mine&state=revoked":        "Revoked verdicts of this node",
		"publisher=" + idAlpha:           "Active verdicts of alpha",
		"address=203.0.113.7&from=peers": "Active verdicts received from other publishers on 203.0.113.7",
		"address=198.51.100.7/24":        "Active verdicts of every publisher on 198.51.100.0/24",
		// The list is not narrowed to what is no address.
		"address=example.org": "Active verdicts of every publisher",
	} {
		if got := verdictsOf(q, list).Heading; got != heading {
			t.Errorf("heading of %q = %q, want %q", q, got, heading)
		}
	}
}

// TestVerdictTotals: AC5 — this node's verdicts and the verdicts received
// per publisher, each number linking to the list.
func TestVerdictTotals(t *testing.T) {
	v := newTotalsView(testVerdictTotals, testPeers, testNode.PeerID)
	mine := totalsRow{Title: "This node", Weight: "1", Active: countLink{9, "/verdicts?from=mine"},
		Revoked: countLink{2, "/verdicts?from=mine&state=revoked"}, Expired: countLink{1, "/verdicts?from=mine&state=expired"},
		CountingNote: "all count in decisions"}
	if !reflect.DeepEqual(v.Mine, mine) {
		t.Errorf("mine = %+v\nwant %+v", v.Mine, mine)
	}
	var got []string
	for _, r := range v.Received {
		got = append(got, fmt.Sprintf("%s w%s nw%v %d/%d/%d %s %s", r.Title, r.Weight, r.NoWeight, r.Active.Count, r.Revoked.Count,
			r.Expired.Count, r.Href, r.Active.Href))
	}
	want := []string{
		"alpha w0.7 nwfalse 120/0/4 /peers/" + idAlpha + " /verdicts?publisher=" + idAlpha,
		shortPeerID(idOther) + " w0 nwtrue 5/1/0 /peers/" + idOther + " /verdicts?publisher=" + idOther,
		shortPeerID(idStray) + " w0 nwtrue 2/0/0 /peers/" + idStray + " /verdicts?publisher=" + idStray,
		"charlie w0.5 nwfalse 1/0/1 /peers/" + idCharlie + " /verdicts?publisher=" + idCharlie,
	}
	if !slices.Equal(got, want) {
		t.Errorf("received = %v\nwant %v", got, want)
	}
	if v.Received[1].Revoked.Href != "/verdicts?publisher="+idOther+"&state=revoked" || v.Received[1].Expired.Href != "" {
		t.Errorf("links of the revoked and expired counts = %+v", v.Received[1])
	}
	all := totalsRow{Title: "Every other publisher", Active: countLink{128, "/verdicts?from=peers"},
		Revoked: countLink{1, "/verdicts?from=peers&state=revoked"}, Expired: countLink{5, "/verdicts?from=peers&state=expired"},
		CountingNote: "119 count in decisions"}
	if !reflect.DeepEqual(v.ReceivedAll, all) || v.Publishers != 4 || v.More != 0 {
		t.Errorf("received in all = %+v (%d publishers, %d more)\nwant %+v", v.ReceivedAll, v.Publishers, v.More, all)
	}

	many := VerdictTotals{ByPublisher: map[string]VerdictCounts{}}
	for i := range maxTotalsRows + 3 {
		many.ByPublisher[fmt.Sprintf("12D3KooWMany%040d", i)] = VerdictCounts{Active: i}
	}
	many.ByPublisher["12D3KooWNothingAnyMore"] = VerdictCounts{}
	v = newTotalsView(many, PeerSet{}, testNode.PeerID)
	if len(v.Received) != maxTotalsRows || v.More != 2 || v.Publishers != maxTotalsRows+2 ||
		v.Received[0].Active.Count != maxTotalsRows+2 || v.Mine.Active.Href != "" {
		t.Errorf("many publishers: %d listed, %d more, %d in all, first %+v", len(v.Received), v.More, v.Publishers, v.Received[0])
	}
}

func TestVerdictsPager(t *testing.T) {
	q := verdictsQuery{from: fromMine, state: VerdictExpired, after: "ipv4:203.0.113.7,x"}
	items := make([]VerdictItem, 50)
	for _, tc := range []struct {
		name string
		list VerdictList
		want verdictsPager
	}{
		{"first", VerdictList{Items: items, Total: 120, Next: "n1"}, verdictsPager{Text: "Verdicts 1–50 of 120",
			Next: "/verdicts?after=n1&from=mine&state=expired"}},
		{"middle", VerdictList{Items: items, Total: 120, Offset: 50, Next: "n2"}, verdictsPager{Text: "Verdicts 51–100 of 120",
			First: "/verdicts?from=mine&state=expired", Next: "/verdicts?after=n2&from=mine&state=expired"}},
		{"last", VerdictList{Items: items[:20], Total: 120, Offset: 100}, verdictsPager{Text: "Verdicts 101–120 of 120",
			First: "/verdicts?from=mine&state=expired"}},
		{"one page", VerdictList{Items: items[:1], Total: 1}, verdictsPager{Text: "1 verdict"}},
		{"none", VerdictList{}, verdictsPager{}},
	} {
		if got := verdictsPagerOf(q, &tc.list); got != tc.want {
			t.Errorf("%s: pager = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestEmptyVerdicts(t *testing.T) {
	for q, want := range map[string]string{
		"":                          "The node holds no active verdict.",
		"from=mine":                 "This node has no active verdict: it reports an address with obiectl report",
		"from=peers":                "The node holds no active verdict of another publisher.",
		"state=revoked":             "No revoked verdict is kept. The node keeps a verdict that ended for 24 hours after its expiry.",
		"state=expired&from=mine":   "No expired verdict is kept. The node keeps a verdict that ended for 24 hours after its expiry.",
		"reason=spam/smtp":          "No verdict matches this view.",
		"address=192.0.2.1":         "No verdict matches this view.",
		"after=ipv4:203.0.113.7,xy": "No verdict follows on this page any more",
	} {
		p := verdictsOf(q, VerdictList{})
		if !strings.HasPrefix(p.Empty, want) {
			t.Errorf("empty list of %q = %q, want %q", q, p.Empty, want)
		}
	}
	p := verdictsOf("", VerdictList{})
	if p.Retention != "24 hours" {
		t.Errorf("retention = %q", p.Retention)
	}
	for d, want := range map[time.Duration]string{0: "", time.Hour: "1 hour", 90 * time.Minute: "1 h 30 min"} {
		if got := retentionText(d); got != want {
			t.Errorf("retentionText(%v) = %q, want %q", d, got, want)
		}
	}
}

// TestVerdictsContentQueries: the scope, the filters, the state and the
// page reach the source; a bad address is explained and not searched.
func TestVerdictsContentQueries(t *testing.T) {
	c := newConsole(t, testConsoleConfig, &syncBuffer{})
	src := verdictsNode(c)
	read := func(query string) verdictsPage {
		t.Helper()
		return c.verdictsContent(httptest.NewRequest("GET", "/verdicts?"+query, nil)).(verdictsPage)
	}
	for _, tc := range []struct {
		query string
		want  VerdictQuery
	}{
		{"", VerdictQuery{State: VerdictActive, Limit: verdictsPageSize}},
		{"from=mine&state=revoked", VerdictQuery{State: VerdictRevoked, Publisher: testNode.PeerID, Limit: verdictsPageSize}},
		{"from=peers&reason=port_scan/tcp&after=c9", VerdictQuery{State: VerdictActive, Except: testNode.PeerID,
			Category: "port_scan/tcp", After: "c9", Limit: verdictsPageSize}},
		{"publisher=" + idAlpha + "&state=expired&address=198.51.100.7/24", VerdictQuery{State: VerdictExpired, Publisher: idAlpha,
			Range: pfx("198.51.100.0/24"), Limit: verdictsPageSize}},
	} {
		p := read(tc.query)
		if got := src.lastQuery(t); got != tc.want {
			t.Errorf("%q: query = %+v, want %+v", tc.query, got, tc.want)
		}
		if p.Err != "" || p.Totals == nil || len(p.Rows) != 5 {
			t.Errorf("%q: page = %+v", tc.query, p)
		}
	}
	p := read("address=203.0.113.7")
	if p.Searched == nil || *p.Searched != (verdictsAddress{Address: "203.0.113.7", DecisionHref: "/decisions/203.0.113.7",
		Command: "sudo obiectl show 203.0.113.7"}) {
		t.Errorf("searched = %+v", p.Searched)
	}
	p = read("address=example.org")
	if src.lastQuery(t).Range.IsValid() || p.AddressErr != `"example.org" is not an IP address or network; the list is not narrowed to it` ||
		p.Searched != nil {
		t.Errorf("bad address: %q, searched %+v, query %+v", p.AddressErr, p.Searched, src.lastQuery(t))
	}

	src.err = fmt.Errorf("%w: prefix too broad", ErrNoIndicator)
	p = read("address=10.0.0.0/8")
	if p.AddressErr != "10.0.0.0/8 is not an address or network the node decides on: no verdict can be on it" ||
		p.Err != "" || p.Searched != nil {
		t.Errorf("no indicator: address error %q, error %q, searched %+v", p.AddressErr, p.Err, p.Searched)
	}
	src.err, src.totalsErr = errors.New("store is not open"), errors.New("store is not open")
	p = read("state=expired")
	if p.Err != "store is not open" || p.TotalsErr != "store is not open" || p.Totals != nil || p.Empty != "" {
		t.Errorf("failed reads: %+v", p)
	}

	c.node.Verdicts = nil
	p = read("")
	if p.Err != "the node passes the console no verdicts" || p.Totals != nil {
		t.Errorf("without a source: %+v", p)
	}
}

func TestVerdictsNotice(t *testing.T) {
	statuses := func(name string, state lifecycle.State) []lifecycle.Status {
		return []lifecycle.Status{{Name: name, State: state}}
	}
	for _, tc := range []struct {
		statuses []lifecycle.Status
		state    string
		want     string
	}{
		{runningStatuses(), "", ""},
		{runningStatuses(), VerdictExpired, ""},
		{statuses(partDecision, lifecycle.StateStarting), "", "The decision engine is starting"},
		{statuses(partDecision, lifecycle.StateStopped), "", "The decision engine is not running"},
		{statuses(partDecision, lifecycle.StateStopped), VerdictRevoked, ""},
		{statuses(partStore, lifecycle.StatePending), VerdictRevoked, "The store is starting"},
		{statuses(partStore, lifecycle.StateStopping), VerdictExpired, "The store is not running"},
	} {
		if got := verdictsNotice(tc.statuses, tc.state); !strings.HasPrefix(got, tc.want) || (tc.want == "" && got != "") {
			t.Errorf("verdictsNotice(%v, %q) = %q, want %q", tc.statuses, tc.state, got, tc.want)
		}
	}
}

// TestVerdictsOfferEveryPublisher: the publisher filter offers also the
// publishers whose verdicts all ended, counting every verdict of each.
func TestVerdictsOfferEveryPublisher(t *testing.T) {
	totals := testVerdictTotals
	totals.ByPublisher = map[string]VerdictCounts{idAlpha: {Active: 2, Expired: 1}, idBravo: {Revoked: 1}}
	in := verdictsInput{now: verdictsNow, self: testNode.PeerID, peers: testPeers, totals: totals}
	var labels []string
	for _, o := range buildVerdicts(in).Publishers {
		labels = append(labels, o.Label)
	}
	if want := []string{"Every publisher", "alpha (12D3KooW…rAa1ph): 3 verdicts", "Bravo (12D3KooW…avoPee): 1 verdict"}; !slices.Equal(labels, want) {
		t.Errorf("publisher options = %v, want %v", labels, want)
	}
	in.totals = VerdictTotals{}
	if got := buildVerdicts(in).Publishers; len(got) != len(publisherOptions(testPeers, testNode.PeerID, "")) {
		t.Errorf("without totals the options = %+v, want those of the held verdicts", got)
	}
}

// TestVerdictsSayWhenTheCapIsReached: F1 of the review — the tab of the
// verdicts that ended says when the node keeps no more of them.
func TestVerdictsSayWhenTheCapIsReached(t *testing.T) {
	totals := testVerdictTotals
	totals.EndedMax, totals.EndedFull = 100000, map[string]bool{VerdictExpired: true}
	for q, want := range map[string]string{
		"state=expired": "It keeps at most 100,000 of other publishers' expired verdicts, and keeps that many now",
		"state=revoked": "",
		"":              "",
	} {
		v, _ := url.ParseQuery(q)
		p := buildVerdicts(verdictsInput{now: verdictsNow, query: parseVerdictsQuery(v), self: testNode.PeerID, peers: testPeers,
			totals: totals})
		if got := strings.Contains(p.StateNote, "It keeps at most"); got != (want != "") || !strings.Contains(p.StateNote, want) {
			t.Errorf("state note of %q = %q, want %q", q, p.StateNote, want)
		}
	}
}
