package console

import (
	"cmp"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// peersPageSize is how many peers a page of the peers view lists.
const peersPageSize = 50

// Filters of the peers view (ADR 0021).
const (
	showAll          = "all"
	showConnected    = "connected"
	showDisconnected = "disconnected"
	showUntrusted    = "untrusted"
)

// Orders of the peers view, each in its natural direction.
const (
	sortPeer        = "peer"
	sortConnection  = "connection"
	sortTrust       = "trust"
	sortVerdicts    = "verdicts"
	sortRejected    = "rejected"
	sortGossipScore = "score"
)

// States of a peer's connection, for the stylesheet.
const stateIdle = "idle"

// peerFilterOption is a filter the peers view offers.
type peerFilterOption struct{ show, label string }

// peerColumnOption is a column of the peers view: the order it sorts by,
// its heading and its direction for aria-sort.
type peerColumnOption struct{ sort, label, direction string }

var (
	// peerFilters are the filters in the order the view offers them.
	peerFilters = []peerFilterOption{
		{showAll, "All"}, {showConnected, "Connected"}, {showDisconnected, "Disconnected"}, {showUntrusted, "Untrusted"},
	}
	// peerColumns are the list's columns in order.
	peerColumns = []peerColumnOption{
		{sortPeer, "Peer", "ascending"},
		{sortConnection, "Connection", "other"},
		{sortTrust, "Trust weight", "descending"},
		{sortVerdicts, "Verdicts held", "descending"},
		{sortRejected, "Events, last hour", "descending"},
		{sortGossipScore, "Gossip score", "ascending"},
	}
	// rejectionReasons name the reasons for rejecting an event, by the
	// outcome of the gossip validation.
	rejectionReasons = map[string]string{
		"invalid_signature": "invalid signature",
		"invalid_schema":    "not a valid obie/0.1 event",
		"too_large":         "too large",
		"expired":           "expired or dated in the future",
		"rate_limited":      "over the rate limit",
	}
)

// peersQuery is what the peers view shows: a filter, an order and a page.
type peersQuery struct {
	show, sort string
	page       int
}

// parsePeersQuery reads the view's query; unknown values fall back to the
// defaults.
func parsePeersQuery(q url.Values) peersQuery {
	p := peersQuery{show: showAll, sort: sortPeer, page: 1}
	if s := q.Get("show"); slices.ContainsFunc(peerFilters, func(f peerFilterOption) bool { return f.show == s }) {
		p.show = s
	}
	if s := q.Get("sort"); slices.ContainsFunc(peerColumns, func(c peerColumnOption) bool { return c.sort == s }) {
		p.sort = s
	}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 {
		p.page = n
	}
	return p
}

// href returns path with p as its query, leaving out the defaults.
func (p peersQuery) href(path string) string {
	v := url.Values{}
	if p.show != showAll {
		v.Set("show", p.show)
	}
	if p.sort != sortPeer {
		v.Set("sort", p.sort)
	}
	if p.page > 1 {
		v.Set("page", strconv.Itoa(p.page))
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

// peersPage is the data of the peers view.
type peersPage struct {
	// Fragment is the path and query of the refreshing region.
	Fragment string
	ReadAt   timestamp
	// Notice explains that the mesh does not run; empty while it does.
	Notice  string
	Filters []peerFilter
	Columns []peerColumn
	Rows    []peerView
	// Empty explains why no peer is listed; empty if some are.
	Empty string
	Pager pager
	// Others sums up the verdicts held from publishers that are not
	// listed under any filter; empty if there are none.
	Others string
}

// peerFilter is a link to the list under one filter.
type peerFilter struct {
	Label, Href string
	Count       int
	Current     bool
}

// peerColumn is a column heading that sorts the list.
type peerColumn struct {
	Label, Href string
	// Sort is the column's aria-sort value while the list is sorted by it;
	// empty otherwise.
	Sort string
}

// pager moves through the pages of a list.
type pager struct {
	// Text says which items the page shows.
	Text string
	// Prev and Next link to the neighboring pages; empty if there is none.
	Prev, Next string
}

// peerEntry is a peer with the verdicts the node holds from it.
type peerEntry struct {
	Peer
	verdicts VerdictCount
}

func (e *peerEntry) rejected() int {
	n := 0
	for _, c := range e.Events.Rejected {
		n += c
	}
	return n
}

// untrusted reports whether the peer carries no weight in decisions.
func (e *peerEntry) untrusted() bool { return !(e.Weight > 0) }

// shows reports whether the filter show lists the peer.
func (e *peerEntry) shows(show string) bool {
	switch show {
	case showConnected:
		return e.Connected
	case showDisconnected:
		return !e.Connected
	case showUntrusted:
		return e.untrusted()
	default:
		return true
	}
}

// peersInput is what the peers view is built from.
type peersInput struct {
	now time.Time
	// self is this node's peer ID, whose verdicts are not a peer's.
	self  string
	set   PeerSet
	query peersQuery
	// notice explains that the mesh does not run; empty while it does.
	notice string
}

// buildPeers filters, sorts and pages the node's peers.
func buildPeers(in peersInput) peersPage {
	entries := make([]peerEntry, len(in.set.Peers))
	for i, p := range in.set.Peers {
		entries[i] = peerEntry{Peer: p, verdicts: in.set.Verdicts[p.ID]}
	}
	q := in.query
	p := peersPage{ReadAt: stamp(in.now), Notice: in.notice, Others: othersNote(in.set, in.self)}
	for _, f := range peerFilters {
		n := 0
		for i := range entries {
			if entries[i].shows(f.show) {
				n++
			}
		}
		p.Filters = append(p.Filters, peerFilter{Label: f.label, Count: n, Current: f.show == q.show,
			Href: peersQuery{show: f.show, sort: q.sort, page: 1}.href("/peers")})
	}
	for _, c := range peerColumns {
		col := peerColumn{Label: c.label, Href: peersQuery{show: q.show, sort: c.sort, page: 1}.href("/peers")}
		if c.sort == q.sort {
			col.Sort = c.direction
		}
		p.Columns = append(p.Columns, col)
	}

	shown := slices.DeleteFunc(entries, func(e peerEntry) bool { return !e.shows(q.show) })
	sortPeers(shown, q.sort)
	pages := max((len(shown)+peersPageSize-1)/peersPageSize, 1)
	q.page = min(q.page, pages)
	p.Fragment = q.href("/api/peers")
	first := (q.page - 1) * peersPageSize
	for i := range shown[first:min(first+peersPageSize, len(shown))] {
		p.Rows = append(p.Rows, newPeerView(&shown[first+i], in.set.EventWindow))
	}
	switch {
	case len(shown) == 0:
		p.Empty = emptyPeers(q.show, len(entries))
	case pages > 1:
		p.Pager.Text = fmt.Sprintf("Peers %s–%s of %s, page %d of %d", count(first+1), count(first+len(p.Rows)),
			count(len(shown)), q.page, pages)
	default:
		p.Pager.Text = plural(len(shown), "peer", "peers")
	}
	if q.page > 1 {
		p.Pager.Prev = peersQuery{show: q.show, sort: q.sort, page: q.page - 1}.href("/peers")
	}
	if q.page < pages {
		p.Pager.Next = peersQuery{show: q.show, sort: q.sort, page: q.page + 1}.href("/peers")
	}
	return p
}

// sortPeers orders entries by the order by, then by name and peer ID.
func sortPeers(entries []peerEntry, by string) {
	slices.SortFunc(entries, func(a, b peerEntry) int {
		var c int
		switch by {
		case sortConnection:
			c = cmp.Compare(connectionRank(&a), connectionRank(&b))
		case sortTrust:
			c = cmp.Compare(b.Weight, a.Weight)
		case sortVerdicts:
			c = cmp.Compare(b.verdicts.Held, a.verdicts.Held)
		case sortRejected:
			c = cmp.Compare(b.rejected(), a.rejected())
		case sortGossipScore:
			c = compareScores(a.GossipScore, b.GossipScore)
		}
		if c != 0 {
			return c
		}
		return comparePeers(&a, &b)
	})
}

// compareScores orders the lowest GossipSub score first and peers
// without one last.
func compareScores(a, b *GossipScore) int {
	switch {
	case a == nil || b == nil:
		return cmp.Compare(boolRank(a == nil), boolRank(b == nil))
	default:
		return cmp.Compare(a.Score, b.Score)
	}
}

// boolRank orders false before true.
func boolRank(v bool) int {
	if v {
		return 1
	}
	return 0
}

// connectionRank orders the connected peers first, then those seen since
// obied started, then the others.
func connectionRank(e *peerEntry) int {
	switch {
	case e.Connected:
		return 0
	case !e.LastSeen.IsZero():
		return 1
	default:
		return 2
	}
}

// comparePeers orders named peers by name, before unnamed ones, then by
// peer ID.
func comparePeers(a, b *peerEntry) int {
	if (a.Name == "") != (b.Name == "") {
		if a.Name != "" {
			return -1
		}
		return 1
	}
	if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}

// emptyPeers explains why the filter show lists none of the known peers.
func emptyPeers(show string, known int) string {
	switch {
	case known == 0:
		return "This node knows no peer: none is configured in mesh.bootstrap or trust.publishers, and none is " +
			"connected. Add the peers of your mesh to mesh.bootstrap and restart obied; the federation guide explains how."
	case show == showConnected:
		return "No peer is connected."
	case show == showDisconnected:
		return "Every peer this node knows is connected."
	default:
		return "Every peer this node knows carries weight in its decisions."
	}
}

// othersNote sums up the verdicts the node holds from publishers that are
// neither configured nor connected, and the weight they carry.
func othersNote(set PeerSet, self string) string {
	listed := make(map[string]bool, len(set.Peers))
	for _, p := range set.Peers {
		listed[p.ID] = true
	}
	var publishers, held, counting int
	for id, c := range set.Verdicts {
		if listed[id] || id == self || c.Held == 0 {
			continue
		}
		publishers++
		held += c.Held
		counting += c.Counting
	}
	if publishers == 0 {
		return ""
	}
	note := fmt.Sprintf("This node also holds %s from %s that are neither configured nor connected. ",
		plural(held, "active verdict", "active verdicts"), plural(publishers, "other publisher", "other publishers"))
	if !(set.DefaultWeight > 0) {
		return note + "They carry the default weight " + weight(set.DefaultWeight) + ": no influence on decisions."
	}
	return note + fmt.Sprintf("They carry the default weight %s; %s in decisions.", weight(set.DefaultWeight),
		plural(counting, "of their verdicts counts", "of their verdicts count"))
}

// peerView is a peer as the peers view and the peer's page show it.
type peerView struct {
	// Href links to the peer's page.
	Href        string
	ID, ShortID string
	// Title is the peer's name, or "Unnamed peer" without one.
	Title string
	Named bool
	// Roles say how the peer is configured; Configured is set if it is.
	Roles      []string
	Configured bool
	// State is stateReady for a connected peer, stateWarning for a
	// bootstrap peer that is not connected and stateIdle for another one;
	// StateLabel names it.
	Connected         bool
	State, StateLabel string
	// SinceLabel says what Since is: when the peer connected or was last
	// seen. Without a time, SinceLabel says it was not seen.
	SinceLabel string
	Since      timestamp
	Latency    string
	// DialError is why the last dial of a bootstrap peer failed, at
	// DialFailedAt.
	DialError    string
	DialFailedAt timestamp
	Addrs        []string
	// AddrsFrom says where the addresses come from.
	AddrsFrom string
	// Weight is the peer's trust weight; Default is set if it is
	// trust.default_weight, and NoInfluence if it is 0.
	Weight               string
	Default, NoInfluence bool
	// Held counts the verdicts held from the peer; HeldNote says how many
	// count in decisions.
	Held, HeldNote string
	// Accepted, Rejected and Duplicates count the events the peer sent in
	// the window; Reasons explain the rejections.
	Accepted, Rejected, Duplicates int
	Reasons                        []rejection
	// Window names how far back the events count, e.g. "last hour".
	Window string
	// Score describes the peer's GossipSub score; its Value is empty if
	// the peer has none.
	Score scoreView
}

// scoreView is a peer's GossipSub score as the views show it (ADR 0032).
type scoreView struct {
	Value string
	// Badge names the lowest threshold the score is below; empty if none.
	Badge string
	// ReadAt is when the score was read.
	ReadAt timestamp
	// Components explain the score on the peer's page.
	Components []scoreComponent
}

// scoreComponent is one component of a GossipSub score.
type scoreComponent struct{ Label, Value string }

// thresholdBadges say what a score below each threshold means, lowest
// threshold last.
var thresholdBadges = []struct{ threshold, badge string }{
	{"gossip", "Below the gossip threshold: no gossip with it"},
	{"publish", "Below the publish threshold: gets none of this node's events"},
	{"graylist", "Graylisted: its messages are ignored"},
}

// newScoreView describes the GossipSub score s; the zero view if s is nil.
func newScoreView(s *GossipScore) scoreView {
	if s == nil {
		return scoreView{}
	}
	v := scoreView{Value: scoreNumber(s.Score), ReadAt: stamp(s.ReadAt)}
	for _, b := range thresholdBadges {
		if slices.Contains(s.Below, b.threshold) {
			v.Badge = b.badge
		}
	}
	inMesh := "not in this node's mesh"
	if s.TimeInMesh > 0 {
		inMesh = s.TimeInMesh.Round(time.Second).String()
	}
	v.Components = []scoreComponent{
		{"Time in this node's mesh", inMesh},
		{"First deliveries of valid events", scoreNumber(s.FirstMessageDeliveries)},
		{"Invalid messages", scoreNumber(s.InvalidMessageDeliveries)},
		{"Behaviour penalty", scoreNumber(s.BehaviourPenalty)},
		{"IP colocation factor", scoreNumber(s.IPColocationFactor)},
		{"Application score", scoreNumber(s.AppSpecificScore)},
	}
	return v
}

// scoreNumber formats a score or a component rounded to two decimals.
func scoreNumber(f float64) string {
	return strconv.FormatFloat(math.Round(f*100)/100+0, 'f', -1, 64)
}

// rejection is why a number of events were rejected.
type rejection struct {
	Reason string
	Count  int
}

// newPeerView describes the peer e, whose events count over window.
func newPeerView(e *peerEntry, window time.Duration) peerView {
	v := peerView{
		Href:      "/peers/" + url.PathEscape(e.ID),
		ID:        e.ID,
		ShortID:   shortPeerID(e.ID),
		Title:     e.Name,
		Named:     e.Name != "",
		Addrs:     e.Addrs,
		Connected: e.Connected,
		Weight:    weight(e.Weight),
		Accepted:  e.Events.Accepted, Duplicates: e.Events.Duplicates, Rejected: e.rejected(),
		Reasons: rejections(e.Events.Rejected),
		Window:  windowName(window),
	}
	if !v.Named {
		v.Title = "Unnamed peer"
	}
	v.Roles, v.Configured = roles(&e.Peer), e.Bootstrap || e.Publisher
	v.State, v.StateLabel, v.SinceLabel, v.Since = connection(&e.Peer)
	if e.Latency > 0 {
		v.Latency = e.Latency.Round(100 * time.Microsecond).String()
	}
	if !e.Connected {
		v.DialError, v.DialFailedAt = e.DialError, stamp(e.DialFailedAt)
	}
	v.AddrsFrom = addrsFrom(&e.Peer)
	v.Default, v.NoInfluence = !e.Publisher, e.untrusted()
	v.Held, v.HeldNote = heldText(e.verdicts)
	v.Score = newScoreView(e.GossipScore)
	return v
}

// roles say how peer p is configured.
func roles(p *Peer) []string {
	var out []string
	if p.Bootstrap {
		out = append(out, "Bootstrap peer")
	}
	if p.Publisher {
		out = append(out, "Trusted publisher")
	}
	if len(out) == 0 {
		out = append(out, "Not configured")
	}
	return out
}

// connection describes the connection to peer p: its state, its label,
// and since when.
func connection(p *Peer) (state, label, sinceLabel string, since timestamp) {
	switch {
	case p.Connected:
		return stateReady, "Connected", "since", stamp(p.ConnectedSince)
	case p.Bootstrap:
		state = stateWarning
	default:
		state = stateIdle
	}
	if p.LastSeen.IsZero() {
		return state, "Disconnected", "not connected since obied started", timestamp{}
	}
	return state, "Disconnected", "last seen", stamp(p.LastSeen)
}

// addrsFrom says where the addresses of peer p come from.
func addrsFrom(p *Peer) string {
	switch {
	case p.Connected:
		return "of the open connections"
	case len(p.Addrs) > 0:
		return "from mesh.bootstrap"
	case p.Publisher:
		return "none known: the peer is not in mesh.bootstrap, so this node does not dial it"
	default:
		return "none known"
	}
}

// heldText formats the verdicts held from a peer and how many count.
func heldText(c VerdictCount) (held, note string) {
	switch {
	case c.Held == 0:
		return "None", ""
	case c.Counting == 0:
		return count(c.Held), "none counts in decisions"
	case c.Counting == c.Held:
		if c.Held == 1 {
			return "1", "counts in decisions"
		}
		return count(c.Held), "all count in decisions"
	default:
		return count(c.Held), plural(c.Counting, "counts in decisions", "count in decisions")
	}
}

// rejections lists the reasons for rejected events, the most frequent
// first.
func rejections(byReason map[string]int) []rejection {
	var out []rejection
	for reason, n := range byReason {
		if n == 0 {
			continue
		}
		label, ok := rejectionReasons[reason]
		if !ok {
			label = strings.ReplaceAll(reason, "_", " ")
		}
		out = append(out, rejection{Reason: label, Count: n})
	}
	slices.SortFunc(out, func(a, b rejection) int {
		if c := cmp.Compare(b.Count, a.Count); c != 0 {
			return c
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	return out
}

// windowName names how far back events count.
func windowName(d time.Duration) string {
	if d == time.Hour || d == 0 {
		return "last hour"
	}
	return "last " + humanDuration(d)
}

// weight formats a trust weight.
func weight(w float64) string {
	return strconv.FormatFloat(w, 'g', -1, 64)
}

// meshNotice explains that the mesh does not run, from its lifecycle
// status; empty while it runs or is not part of the node.
func meshNotice(statuses []lifecycle.Status) string {
	for _, s := range statuses {
		if s.Name != partMesh {
			continue
		}
		switch s.State {
		case lifecycle.StateRunning:
			return ""
		case lifecycle.StatePending, lifecycle.StateStarting:
			return "The mesh is starting: no peer is connected yet. The configured peers are listed as configured."
		default:
			return "The mesh is not running: no peer is connected. The configured peers are listed as configured."
		}
	}
	return ""
}

// peersContent reads the node's peers and returns the data of the peers
// view under the request's filter, order and page.
func (c *Console) peersContent(r *http.Request) any {
	return buildPeers(peersInput{now: c.now(), self: c.node.PeerID, set: c.peerSet(),
		query: parsePeersQuery(r.URL.Query()), notice: meshNotice(c.node.Status())})
}

// peerSet reads the node's peers; none if the node passes no function.
func (c *Console) peerSet() PeerSet {
	if c.node.Peers == nil {
		return PeerSet{}
	}
	return c.node.Peers()
}
