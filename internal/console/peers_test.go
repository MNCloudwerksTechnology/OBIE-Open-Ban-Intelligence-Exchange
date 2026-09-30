package console

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// testConsoleConfig serves a console on a free loopback port.
var testConsoleConfig = config.Console{Enabled: true, Listen: "127.0.0.1:0"}

// startedBrowser starts c and returns a browser signed in to it.
func startedBrowser(t *testing.T, c *Console) *browser {
	t.Helper()
	if err := c.Start(context.Background()); err != nil || c.Addr() == nil {
		t.Fatalf("console not serving: %v %s", err, c.Detail())
	}
	b := newBrowser(t, "http://"+c.Addr().String())
	if resp, _ := b.signIn(c.Token(), "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", resp.StatusCode)
	}
	return b
}

// Peer IDs of the test peers.
const (
	idAlpha   = "12D3KooWAa1phaPeerAa1phaPeerAa1phaPeerAa1phaPeerAa1ph"
	idBravo   = "12D3KooWBravoPeerBravoPeerBravoPeerBravoPeerBravoPee"
	idCharlie = "12D3KooWCharxiePeerCharxiePeerCharxiePeerCharxiePeer"
	idStray   = "12D3KooWStrayPeerStrayPeerStrayPeerStrayPeerStrayPee"
	idOther   = "12D3KooWOtherPubOtherPubOtherPubOtherPubOtherPubOther"
)

var (
	peersNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	// testPeers are four peers: a connected bootstrap peer that is also a
	// trusted publisher, an unreachable bootstrap peer without trust, a
	// trusted publisher that never connected, and a connected peer nobody
	// configured.
	testPeers = PeerSet{
		Peers: []Peer{
			{ID: idStray, Connected: true, Addrs: []string{"/ip4/198.51.100.9/tcp/40112"},
				ConnectedSince: peersNow.Add(-5 * time.Minute), Events: EventCounts{Accepted: 3},
				GossipScore: &GossipScore{Score: -120.004, Below: []string{"gossip", "publish"}, InvalidMessageDeliveries: 3.4641,
					ReadAt: peersNow.Add(-5 * time.Second)}},
			{ID: idAlpha, Name: "alpha", Bootstrap: true, Publisher: true, Connected: true,
				Addrs: []string{"/ip4/192.0.2.1/tcp/4001", "/ip4/192.0.2.1/udp/4001/quic-v1"}, ConnectedSince: peersNow.Add(-2 * time.Hour),
				Latency: 12345 * time.Microsecond, Weight: 0.7,
				Events: EventCounts{Accepted: 57, Duplicates: 4, Rejected: map[string]int{"invalid_signature": 2, "expired": 1}},
				GossipScore: &GossipScore{Score: 1.25, TimeInMesh: 90*time.Second + 400*time.Millisecond, FirstMessageDeliveries: 2.5,
					IPColocationFactor: 0, BehaviourPenalty: 0, AppSpecificScore: 0, ReadAt: peersNow.Add(-5 * time.Second)}},
			{ID: idBravo, Name: "Bravo", Bootstrap: true, Addrs: []string{"/dns4/bravo.example.org/tcp/4001"},
				LastSeen: peersNow.Add(-30 * time.Minute), DialError: "failed to dial: connection refused",
				DialFailedAt: peersNow.Add(-time.Minute),
				GossipScore:  &GossipScore{Score: -250, Below: []string{"gossip", "publish", "graylist"}, ReadAt: peersNow.Add(-5 * time.Second)}},
			{ID: idCharlie, Name: "charlie", Publisher: true, Weight: 0.5},
		},
		Verdicts: map[string]VerdictCount{
			idAlpha:         {Held: 120, Counting: 118},
			idCharlie:       {Held: 1, Counting: 1},
			idStray:         {Held: 2},
			idOther:         {Held: 5},
			testNode.PeerID: {Held: 9, Counting: 9},
		},
		EventWindow: time.Hour,
		LocalWeight: 1,
	}
)

// peersOf builds the peers view of set under the query q.
func peersOf(set PeerSet, q string) peersPage {
	query, _ := url.ParseQuery(q)
	return buildPeers(peersInput{now: peersNow, self: testNode.PeerID, set: set, query: parsePeersQuery(query)})
}

func rowIDs(p peersPage) []string {
	var ids []string
	for _, r := range p.Rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// TestBuildPeersListsEveryKnownPeer: AC1–AC3 — every configured and
// connected peer with how it is configured, its connection, trust,
// verdicts and events.
func TestBuildPeersListsEveryKnownPeer(t *testing.T) {
	p := peersOf(testPeers, "")
	if got, want := rowIDs(p), []string{idAlpha, idBravo, idCharlie, idStray}; !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want named peers by name, then unnamed ones: %v", got, want)
	}
	if p.Fragment != "/api/peers" || p.ReadAt.Text != "2026-09-28 12:00:00 UTC" || p.Pager.Text != "4 peers" ||
		p.Pager.Prev != "" || p.Pager.Next != "" || p.Empty != "" {
		t.Errorf("page = %+v", p)
	}
	alpha, bravo, charlie, stray := p.Rows[0], p.Rows[1], p.Rows[2], p.Rows[3]

	for _, c := range []struct {
		name string
		got  any
		want any
	}{
		{"alpha href", alpha.Href, "/peers/" + idAlpha},
		{"alpha title", alpha.Title + "|" + alpha.ShortID, "alpha|12D3KooW…rAa1ph"},
		{"alpha roles", strings.Join(alpha.Roles, ", ") + "|" + fmt.Sprint(alpha.Configured), "Bootstrap peer, Trusted publisher|true"},
		{"alpha connection", alpha.State + "|" + alpha.StateLabel + "|" + alpha.SinceLabel + "|" + alpha.Since.Text,
			"ready|Connected|since|2026-09-28 10:00:00 UTC"},
		{"alpha latency", alpha.Latency, "12.3ms"},
		{"alpha addresses", strings.Join(alpha.Addrs, " ") + "|" + alpha.AddrsFrom,
			"/ip4/192.0.2.1/tcp/4001 /ip4/192.0.2.1/udp/4001/quic-v1|of the open connections"},
		{"alpha trust", alpha.Weight + "|" + fmt.Sprint(alpha.Default, alpha.NoInfluence), "0.7|false false"},
		{"alpha verdicts", alpha.Held + "|" + alpha.HeldNote, "120|118 count in decisions"},
		{"alpha events", fmt.Sprint(alpha.Accepted, alpha.Rejected, alpha.Duplicates, alpha.Reasons),
			"57 3 4 [{invalid signature 2} {expired or dated in the future 1}]"},
		{"alpha window", alpha.Window, "last hour"},
		{"alpha score", alpha.Score.Value + "|" + alpha.Score.Badge + "|" + alpha.Score.ReadAt.Text, "1.25||2026-09-28 11:59:55 UTC"},
		{"alpha score components", alpha.Score.Components, []scoreComponent{{"Time in this node's mesh", "1m30s"},
			{"First deliveries of valid events", "2.5"}, {"Invalid messages", "0"}, {"Behaviour penalty", "0"},
			{"IP colocation factor", "0"}, {"Application score", "0"}}},

		// The unreachable bootstrap peer: disconnected, last seen, the failed dial.
		{"bravo roles", strings.Join(bravo.Roles, ", "), "Bootstrap peer"},
		{"bravo connection", bravo.State + "|" + bravo.StateLabel + "|" + bravo.SinceLabel + "|" + bravo.Since.Text,
			"warning|Disconnected|last seen|2026-09-28 11:30:00 UTC"},
		{"bravo dial", bravo.DialError + "|" + bravo.DialFailedAt.Text, "failed to dial: connection refused|2026-09-28 11:59:00 UTC"},
		{"bravo addresses", strings.Join(bravo.Addrs, " ") + "|" + bravo.AddrsFrom, "/dns4/bravo.example.org/tcp/4001|from mesh.bootstrap"},
		{"bravo trust", bravo.Weight + "|" + fmt.Sprint(bravo.Default, bravo.NoInfluence), "0|true true"},
		{"bravo verdicts", bravo.Held + "|" + bravo.HeldNote, "None|"},
		{"bravo events", fmt.Sprint(bravo.Accepted, bravo.Rejected, bravo.Reasons), "0 0 []"},
		{"bravo score", bravo.Score.Value + "|" + bravo.Score.Badge + "|" + bravo.Score.Components[0].Value,
			"-250|Graylisted: its messages are ignored|not in this node's mesh"},

		// A trusted publisher that never connected and is not dialed.
		{"charlie connection", charlie.State + "|" + charlie.SinceLabel + "|" + charlie.Since.Text,
			"idle|not connected since obied started|"},
		{"charlie addresses", charlie.AddrsFrom, "none known: the peer is not in mesh.bootstrap, so this node does not dial it"},
		{"charlie verdicts", charlie.Held + "|" + charlie.HeldNote, "1|counts in decisions"},
		{"charlie trust", charlie.Weight + "|" + fmt.Sprint(charlie.Default, charlie.NoInfluence), "0.5|false false"},
		{"charlie score", fmt.Sprint(charlie.Score.Value == "", charlie.Score.Badge == "", charlie.Score.Components == nil),
			"true true true"},

		// A connected peer nobody configured: no influence on decisions.
		{"stray title", stray.Title + "|" + fmt.Sprint(stray.Named), "Unnamed peer|false"},
		{"stray roles", strings.Join(stray.Roles, ", ") + "|" + fmt.Sprint(stray.Configured), "Not configured|false"},
		{"stray trust", stray.Weight + "|" + fmt.Sprint(stray.Default, stray.NoInfluence), "0|true true"},
		{"stray verdicts", stray.Held + "|" + stray.HeldNote, "2|none counts in decisions"},
		{"stray score", stray.Score.Value + "|" + stray.Score.Badge + "|" + stray.Score.Components[2].Value,
			"-120|Below the publish threshold: gets none of this node's events|3.46"},
	} {
		if fmt.Sprint(c.got) != fmt.Sprint(c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	// The node's own verdicts are not a peer's; the other publisher's are.
	if want := "This node also holds 5 active verdicts from 1 other publisher that are neither configured nor connected. " +
		"They carry the default weight 0: no influence on decisions."; p.Others != want {
		t.Errorf("Others = %q, want %q", p.Others, want)
	}
}

// TestBuildPeersFilters: AC5 — connected, disconnected and untrusted peers,
// with how many each filter lists.
func TestBuildPeersFilters(t *testing.T) {
	for _, tc := range []struct {
		show string
		want []string
	}{
		{"", []string{idAlpha, idBravo, idCharlie, idStray}},
		{"all", []string{idAlpha, idBravo, idCharlie, idStray}},
		{"connected", []string{idAlpha, idStray}},
		{"disconnected", []string{idBravo, idCharlie}},
		{"untrusted", []string{idBravo, idStray}},
		{"bogus", []string{idAlpha, idBravo, idCharlie, idStray}},
	} {
		p := peersOf(testPeers, "show="+tc.show)
		if got := rowIDs(p); !slices.Equal(got, tc.want) {
			t.Errorf("show=%s: rows = %v, want %v", tc.show, got, tc.want)
		}
		var filters []string
		for _, f := range p.Filters {
			filters = append(filters, fmt.Sprintf("%s %d %s %v", f.Label, f.Count, f.Href, f.Current))
		}
		show := tc.show
		if show == "" || show == "bogus" {
			show = "all"
		}
		want := []string{
			fmt.Sprintf("All 4 /peers %v", show == "all"),
			fmt.Sprintf("Connected 2 /peers?show=connected %v", show == "connected"),
			fmt.Sprintf("Disconnected 2 /peers?show=disconnected %v", show == "disconnected"),
			fmt.Sprintf("Untrusted 2 /peers?show=untrusted %v", show == "untrusted"),
		}
		if !slices.Equal(filters, want) {
			t.Errorf("show=%s: filters = %q, want %q", tc.show, filters, want)
		}
	}

	allConnected := PeerSet{Peers: []Peer{{ID: idAlpha, Connected: true, Weight: 1}}}
	for show, want := range map[string]string{
		"connected":    "",
		"disconnected": "Every peer this node knows is connected.",
		"untrusted":    "Every peer this node knows carries weight in its decisions.",
	} {
		if p := peersOf(allConnected, "show="+show); p.Empty != want || (want != "" && (len(p.Rows) != 0 || p.Pager.Text != "")) {
			t.Errorf("show=%s: Empty = %q, rows %d, pager %q; want %q", show, p.Empty, len(p.Rows), p.Pager.Text, want)
		}
	}
	if p := peersOf(PeerSet{Peers: []Peer{{ID: idBravo}}}, "show=connected"); p.Empty != "No peer is connected." {
		t.Errorf("no connected peer: Empty = %q", p.Empty)
	}
	if p := peersOf(PeerSet{}, ""); !strings.HasPrefix(p.Empty, "This node knows no peer: none is configured in mesh.bootstrap") {
		t.Errorf("no peers: Empty = %q", p.Empty)
	}
}

// TestBuildPeersSorts: AC5 — every column sorts in its natural direction,
// ties by name and peer ID; the headings keep the filter.
func TestBuildPeersSorts(t *testing.T) {
	set := testPeers
	set.Peers = slices.Clone(testPeers.Peers)
	set.Peers[0].Events.Rejected = map[string]int{"too_large": 7} // the stray peer rejects most
	for sort, want := range map[string][]string{
		"peer":       {idAlpha, idBravo, idCharlie, idStray},
		"connection": {idAlpha, idStray, idBravo, idCharlie},
		"trust":      {idAlpha, idCharlie, idBravo, idStray},
		"verdicts":   {idAlpha, idStray, idCharlie, idBravo},
		"rejected":   {idStray, idAlpha, idBravo, idCharlie},
		"score":      {idBravo, idStray, idAlpha, idCharlie},
	} {
		p := peersOf(set, "sort="+sort)
		if got := rowIDs(p); !slices.Equal(got, want) {
			t.Errorf("sort=%s: rows = %v, want %v", sort, got, want)
		}
	}

	p := peersOf(set, "show=connected&sort=trust&page=1")
	var cols []string
	for _, c := range p.Columns {
		cols = append(cols, c.Label+" "+c.Href+" "+c.Sort)
	}
	if want := []string{
		"Peer /peers?show=connected ",
		"Connection /peers?show=connected&sort=connection ",
		"Trust weight /peers?show=connected&sort=trust descending",
		"Verdicts held /peers?show=connected&sort=verdicts ",
		"Events, last hour /peers?show=connected&sort=rejected ",
		"Gossip score /peers?show=connected&sort=score ",
	}; !slices.Equal(cols, want) {
		t.Errorf("columns = %q, want %q", cols, want)
	}
	if p.Fragment != "/api/peers?show=connected&sort=trust" || p.Filters[2].Href != "/peers?show=disconnected&sort=trust" {
		t.Errorf("fragment %q, filter link %q: want the filter and order kept", p.Fragment, p.Filters[2].Href)
	}
	if p.Columns[0].Sort != "" || peersOf(set, "").Columns[0].Sort != "ascending" || peersOf(set, "sort=connection").Columns[1].Sort != "other" {
		t.Error("aria-sort marks the wrong column")
	}
}

// TestBuildPeersPages: hundreds of peers stay readable, 50 per page.
func TestBuildPeersPages(t *testing.T) {
	var set PeerSet
	for i := range 120 {
		set.Peers = append(set.Peers, Peer{ID: fmt.Sprintf("12D3KooWPeer%03d", i), Connected: true})
	}
	p := peersOf(set, "show=connected&page=2")
	if len(p.Rows) != 50 || p.Rows[0].ID != "12D3KooWPeer050" || p.Rows[49].ID != "12D3KooWPeer099" {
		t.Errorf("page 2 = %d rows from %s", len(p.Rows), p.Rows[0].ID)
	}
	if p.Pager != (pager{Text: "Peers 51–100 of 120, page 2 of 3", Prev: "/peers?show=connected",
		Next: "/peers?page=3&show=connected"}) {
		t.Errorf("pager = %+v", p.Pager)
	}
	if p.Fragment != "/api/peers?page=2&show=connected" {
		t.Errorf("fragment = %q", p.Fragment)
	}
	last := peersOf(set, "page=99")
	if len(last.Rows) != 20 || last.Pager.Next != "" || last.Pager.Prev != "/peers?page=2" || last.Fragment != "/api/peers?page=3" {
		t.Errorf("page 99 = %d rows, pager %+v, fragment %q; want the last page", len(last.Rows), last.Pager, last.Fragment)
	}
	for _, q := range []string{"page=0", "page=-3", "page=x"} {
		if p := peersOf(set, q); p.Rows[0].ID != "12D3KooWPeer000" || p.Pager.Prev != "" {
			t.Errorf("%s: want the first page", q)
		}
	}
}

func TestOthersNoteWithDefaultWeight(t *testing.T) {
	set := PeerSet{DefaultWeight: 0.2, Verdicts: map[string]VerdictCount{idOther: {Held: 4, Counting: 3}, idBravo: {Held: 1, Counting: 1}}}
	want := "This node also holds 5 active verdicts from 2 other publishers that are neither configured nor connected. " +
		"They carry the default weight 0.2; 4 of their verdicts count in decisions."
	if got := othersNote(set, testNode.PeerID); got != want {
		t.Errorf("othersNote = %q, want %q", got, want)
	}
	if got := othersNote(PeerSet{Verdicts: map[string]VerdictCount{testNode.PeerID: {Held: 1}}}, testNode.PeerID); got != "" {
		t.Errorf("othersNote with only this node's verdicts = %q", got)
	}
}

func TestMeshNotice(t *testing.T) {
	for state, want := range map[lifecycle.State]string{
		lifecycle.StateRunning:  "",
		lifecycle.StateStarting: "The mesh is starting: no peer is connected yet. The configured peers are listed as configured.",
		lifecycle.StateStopped:  "The mesh is not running: no peer is connected. The configured peers are listed as configured.",
	} {
		if got := meshNotice([]lifecycle.Status{{Name: partStore}, {Name: partMesh, State: state}}); got != want {
			t.Errorf("%s: meshNotice = %q, want %q", state, got, want)
		}
	}
	if got := meshNotice(nil); got != "" {
		t.Errorf("without a mesh: %q", got)
	}
}

func TestFindPeer(t *testing.T) {
	set := testPeers
	set.DefaultWeight = 0.1
	for _, tc := range []struct {
		id     string
		known  bool
		weight float64
		held   int
	}{
		{idAlpha, true, 0.7, 120},
		{idCharlie, true, 0.5, 1},
		{idOther, true, 0.1, 5}, // neither configured nor connected, but its verdicts are held
		{"12D3KooWUnknown", false, 0.1, 0},
		{testNode.PeerID, false, 0.1, 9}, // this node is not a peer
	} {
		e, known := findPeer(set, tc.id, testNode.PeerID)
		if known != tc.known || e.ID != tc.id || e.Weight != tc.weight || e.verdicts.Held != tc.held {
			t.Errorf("findPeer(%s) = %+v, %v", tc.id, e, known)
		}
	}
}

// TestPeerVerdicts: AC4 — a peer's verdicts, whether each counts, links
// into the verdict view (or the commands without it), and paging.
func TestPeerVerdicts(t *testing.T) {
	c := newConsole(t, testConsoleConfig, &syncBuffer{})
	var asked []string
	c.node.PeerVerdicts = func(id, after string, limit int) (VerdictPage, error) {
		asked = append(asked, fmt.Sprintf("%s after %q limit %d", id, after, limit))
		return VerdictPage{Verdicts: []Verdict{
			{Key: "cidr:198.51.100.0/24", Address: "198.51.100.0/24", Action: "ban", Confidence: 0.9, Reason: "port_scan", ExpiresAt: peersNow.Add(23 * time.Hour)},
			{Key: "ipv4:203.0.113.7", Address: "203.0.113.7", Action: "watch", Confidence: 0.5, Reason: "password_bruteforce",
				Protocol: "ssh", ExpiresAt: peersNow.Add(time.Hour)},
		}, Next: "ipv4:203.0.113.7"}, nil
	}
	e := peerEntry{Peer: Peer{ID: idAlpha, Weight: 0.7}, verdicts: VerdictCount{Held: 2, Counting: 1}}
	l := c.peerVerdicts(&e, "")
	if len(asked) != 1 || asked[0] != idAlpha+` after "" limit 50` {
		t.Errorf("asked %v", asked)
	}
	if len(l.Rows) != 2 {
		t.Fatalf("rows = %+v", l.Rows)
	}
	cidr, v4 := l.Rows[0], l.Rows[1]
	if cidr.Address != "198.51.100.0/24" || cidr.Action != "ban" || cidr.Confidence != "0.9" || cidr.Reason != "port_scan" ||
		cidr.Expires.Text != "2026-09-29 11:00:00 UTC" || !cidr.Counting || cidr.Counts != "Yes" {
		t.Errorf("ban row = %+v", cidr)
	}
	if v4.Reason != "password_bruteforce (ssh)" || v4.Counting || v4.Counts != "No: a watch verdict" {
		t.Errorf("watch row = %+v", v4)
	}
	// The verdicts link into the verdict view (ADR 0023).
	if cidr.Href != "/verdicts?address=198.51.100.0%2F24" || l.All != "/verdicts?publisher="+idAlpha || l.AllCommand != "" ||
		l.ShowCommand != "" {
		t.Errorf("links with a verdict view: row %q, all %q / %q, show %q", cidr.Href, l.All, l.AllCommand, l.ShowCommand)
	}
	if l.Next != "/peers/"+idAlpha+"?after=ipv4%3A203.0.113.7" || l.First != "" {
		t.Errorf("paging: next %q, first %q", l.Next, l.First)
	}

	// Without the verdict view, the page names the commands.
	c.pages = slices.DeleteFunc(c.pages, func(v view) bool { return v.Path == "/verdicts" })
	untrusted := peerEntry{Peer: Peer{ID: idBravo}, verdicts: VerdictCount{Held: 2}}
	l = c.peerVerdicts(&untrusted, "ipv4:1.2.3.4")
	if l.Rows[0].Href != "" || l.All != "" || l.AllCommand != "obiectl indicators --publisher "+idBravo ||
		l.ShowCommand != "obiectl show <address>" || l.First != "/peers/"+idBravo {
		t.Errorf("links without a verdict view: row %q, all %q / %q, show %q, first %q", l.Rows[0].Href, l.All, l.AllCommand, l.ShowCommand, l.First)
	}
	if l.Rows[0].Counting || l.Rows[0].Counts != "No: weight 0" {
		t.Errorf("ban of an untrusted peer: %+v", l.Rows[0])
	}

	// A peer the node holds no verdict of is not looked up: that would
	// walk every verdict key.
	asked = nil
	none := peerEntry{Peer: Peer{ID: idCharlie, Weight: 1}}
	if l := c.peerVerdicts(&none, ""); len(asked) != 0 || len(l.Rows) != 0 || l.Err != "" {
		t.Errorf("a peer without verdicts: asked %v, list %+v", asked, l)
	}

	c.node.PeerVerdicts = func(string, string, int) (VerdictPage, error) { return VerdictPage{}, errors.New("store is not open") }
	if l := c.peerVerdicts(&e, ""); l.Err != "store is not open" || len(l.Rows) != 0 {
		t.Errorf("failed read: %+v", l)
	}
	c.node.PeerVerdicts = nil
	if l := c.peerVerdicts(&e, ""); l.Err != "" || len(l.Rows) != 0 {
		t.Errorf("without PeerVerdicts: %+v", l)
	}
}

// TestItemPages: a view's items are served below its path, marked as the
// view in the navigation, with a refreshing region of their own; an
// unknown item is not found, but its region still renders.
func TestItemPages(t *testing.T) {
	c := newConsole(t, testConsoleConfig, &syncBuffer{})
	itemTemplate := template.Must(template.Must(template.New("item").ParseFS(templateFiles, "templates/layout.html")).
		Parse(`{{define "content"}}<h1>{{.}}</h1><div data-refresh>{{template "region" .}}</div>{{end}}{{define "region"}}<p>item {{.}}</p>{{end}}`)).Lookup("layout")
	c.pages = append(c.pages, view{Path: "/things", Title: "Things", Fragment: "/api/things",
		template: itemTemplate, content: func(*http.Request) any { return "all" },
		item: &item{template: itemTemplate, missing: "No such thing.", content: func(r *http.Request, region bool) (string, any, bool) {
			id := r.PathValue("id")
			return "Thing " + id, id + fmt.Sprintf(" region=%v", region), id != "gone"
		}}})
	c.handler = c.routes()
	b := startedBrowser(t, c)

	resp, page := b.get("/things/one")
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "<h1>one region=false</h1>") ||
		!strings.Contains(page, "<title>Thing one · OBIE console</title>") {
		t.Fatalf("GET /things/one = %d:\n%s", resp.StatusCode, page)
	}
	if nav := navOf(t, page); nav[len(nav)-1] != (navItem{Path: "/things", Title: "Things", Current: "true"}) {
		t.Errorf("navigation = %+v, want the view marked as containing the page", nav)
	}
	if resp, body := b.get("/api/things/one"); resp.StatusCode != http.StatusOK || body != "<p>item one region=true</p>" {
		t.Errorf("GET /api/things/one = %d %q", resp.StatusCode, body)
	}
	resp, page = b.get("/things/gone")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(page, "<code>/things/gone</code>. No such thing.</p>") {
		t.Errorf("GET /things/gone = %d:\n%s", resp.StatusCode, page)
	}
	for _, item := range navOf(t, page) {
		if item.Current != "" {
			t.Errorf("the not-found page marks %s as current", item.Path)
		}
	}
	if resp, body := b.get("/api/things/gone"); resp.StatusCode != http.StatusOK || body != "<p>item gone region=true</p>" {
		t.Errorf("GET /api/things/gone = %d %q", resp.StatusCode, body)
	}
	if resp, _ := b.get("/things/one/more"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /things/one/more = %d, want 404", resp.StatusCode)
	}
}
