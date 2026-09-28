package console

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// Scopes of the verdicts view: whose verdicts it lists (ADR 0023).
const (
	fromMine  = "mine"
	fromPeers = "peers"
)

// maxTotalsRows bounds the publishers the totals list one by one.
const maxTotalsRows = 20

var (
	// verdictScopes are the scope tabs in order.
	verdictScopes = []struct{ from, label string }{{"", "All publishers"}, {fromMine, "This node"}, {fromPeers, "Received"}}
	// verdictStates are the state tabs in order; "" lists the active
	// verdicts.
	verdictStates = []struct{ state, label string }{{"", "Active"}, {VerdictRevoked, "Revoked"}, {VerdictExpired, "Expired"}}
)

// verdictsQuery is what the verdicts view shows: whose verdicts, filters,
// a state and a page. It is the view's URL, so a link shares the view.
type verdictsQuery struct {
	// from is fromMine, fromPeers or "" for every publisher; publisher, if
	// set, selects one publisher instead.
	from, publisher string
	reason          string
	// address is the address or network as typed.
	address string
	// state is VerdictRevoked, VerdictExpired or "" for the active ones.
	state string
	after string
}

// parseVerdictsQuery reads the view's query; unknown values fall back to
// the defaults.
func parseVerdictsQuery(v url.Values) verdictsQuery {
	q := verdictsQuery{publisher: bounded(v.Get("publisher")), reason: bounded(v.Get("reason")),
		address: bounded(strings.TrimSpace(v.Get("address"))), after: bounded(v.Get("after"))}
	if f := v.Get("from"); (f == fromMine || f == fromPeers) && q.publisher == "" {
		q.from = f
	}
	if s := v.Get("state"); s == VerdictRevoked || s == VerdictExpired {
		q.state = s
	}
	return q
}

// href returns path with q as its query, leaving out the defaults.
func (q verdictsQuery) href(path string) string {
	v := url.Values{}
	for _, kv := range [][2]string{{"from", q.from}, {"publisher", q.publisher}, {"reason", q.reason}, {"address", q.address},
		{"state", q.state}, {"after", q.after}} {
		if kv[1] != "" {
			v.Set(kv[0], kv[1])
		}
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

// first returns q on its first page.
func (q verdictsQuery) first() verdictsQuery {
	q.after = ""
	return q
}

// filtered reports whether q filters the verdicts, its scope and state
// aside.
func (q verdictsQuery) filtered() bool {
	return q.reason != "" || q.address != "" || q.publisher != ""
}

// verdictsHref links to the verdicts view under q.
func verdictsHref(q verdictsQuery) string { return q.href("/verdicts") }

// verdictsPage is the data of the verdicts view. It is read when the page
// opens (ADR 0023).
type verdictsPage struct {
	ReadAt timestamp
	// Notice explains that the part of the node the list comes from does
	// not run; Err says why the verdicts could not be read.
	Notice, Err string
	// Totals count the verdicts by publisher; nil if they could not be
	// read, and TotalsErr says why.
	Totals    *verdictTotalsView
	TotalsErr string
	// Scopes and States are the tabs; Scopes mark none current while one
	// publisher is listed, whom Whose names.
	Scopes, States []peerFilter
	Whose          *whoseLine
	// Address is the address as typed; AddressErr says why it is none.
	// Searched introduces the verdicts on it.
	Address, AddressErr string
	Searched            *verdictsAddress
	// From, Publisher and State are kept by the filter form.
	From, Publisher, State string
	Reasons, Publishers    []option
	// Clear links to the list without filters; empty without any.
	Clear string
	// Heading says what the list shows, and StateNote what its state
	// means for a list of verdicts that ended.
	Heading, StateNote string
	Rows               []verdictItemRow
	// Empty explains why no verdict is listed; empty if some are.
	Empty string
	Pager verdictsPager
	// Retention says how long the node keeps a verdict that ended, e.g.
	// "24 hours".
	Retention string
}

// whoseLine names the one publisher whose verdicts are listed.
type whoseLine struct {
	Title, PeerID string
	// Href links to its peer page; empty for this node.
	Href string
	// Weight is its trust weight; NoWeight is set for 0.
	Weight   string
	NoWeight bool
}

// verdictsAddress introduces the verdicts on one address, as obiectl show
// lists them.
type verdictsAddress struct {
	Address, DecisionHref, Command string
}

// verdictsPager moves through the pages of the list.
type verdictsPager struct {
	// Text says which verdicts the page shows.
	Text string
	// First and Next link to those pages; empty if there is none.
	First, Next string
}

// verdictItemRow is a verdict as the list shows it.
type verdictItemRow struct {
	// Address is the address or network; Href links to every verdict on it
	// and DecisionHref to its decision, which DecisionLabel names and
	// DecisionState marks for the stylesheet.
	Address, Href                              string
	DecisionHref, DecisionLabel, DecisionState string
	// Publisher names the publisher; PublisherHref links to its peer page,
	// empty for this node's own verdicts.
	Publisher, PublisherHref string
	// Weight is the publisher's trust weight; NoWeight is set for 0.
	Weight             string
	NoWeight           bool
	Action, Confidence string
	Reason             string
	// Events says how many events are behind the verdict; LogHash is the
	// hash of the log lines, empty if none were given.
	Events, LogHash string
	EventID         string
	Issued, Expires timestamp
	// State is the verdict's state for the stylesheet and StateLabel names
	// it. A revoked verdict has when (Ended), why and by which revocation;
	// an expired one when.
	State, StateLabel         string
	Ended                     timestamp
	RevokeReason, RevokedByID string
	// Counts says whether an active verdict counts in its decision, and if
	// not, why.
	Counts string
}

// verdictTotalsView counts the verdicts of this node and of every other
// publisher.
type verdictTotalsView struct {
	Mine totalsRow
	// Received lists the other publishers, the most active verdicts first;
	// More counts those left out. ReceivedAll sums up every other
	// publisher, Publishers of them.
	Received    []totalsRow
	More        int
	ReceivedAll totalsRow
	Publishers  int
}

// totalsRow counts one publisher's verdicts, or those of many.
type totalsRow struct {
	// Title names the publisher and Href links to its peer page; empty for
	// this node and for a sum.
	Title, PeerID, ShortID, Href string
	Named                        bool
	// Weight is the publisher's trust weight; NoWeight is set for 0.
	Weight   string
	NoWeight bool
	// Active, Revoked and Expired link to those verdicts.
	Active, Revoked, Expired countLink
	// CountingNote says how many of the active verdicts count.
	CountingNote string
}

// countLink is a number linking to what it counts; Href is empty for 0.
type countLink struct {
	Count int
	Href  string
}

// verdictsInput is what the verdicts view is built from.
type verdictsInput struct {
	now   time.Time
	query verdictsQuery
	self  string
	peers PeerSet
	// list is the page of verdicts, listErr why it could not be read.
	list    VerdictList
	listErr error
	// searched is the address or network the list is on; addressErr says
	// why the address typed is none.
	searched   netip.Prefix
	addressErr error
	totals     VerdictTotals
	totalsErr  error
	categories map[string]int
	notice     string
}

// buildVerdicts builds the verdicts view from a page of verdicts and the
// totals.
func buildVerdicts(in verdictsInput) verdictsPage {
	q := in.query
	names := peerNames(in.peers)
	p := verdictsPage{
		ReadAt:     stamp(in.now),
		Notice:     in.notice,
		Address:    q.address,
		From:       q.from,
		Publisher:  q.publisher,
		State:      q.state,
		Reasons:    reasonOptions(in.categories, q.reason, false),
		Publishers: publisherOptions(withTotals(in.peers, in.totals), in.self, q.publisher),
		Retention:  retentionText(in.totals.Retention),
		Heading:    verdictsHeading(q, in.self, names, in.searched),
	}
	p.StateNote = stateNoteOf(q.state, p.Retention)
	if q.state != "" && in.totals.EndedFull[q.state] {
		p.StateNote += fmt.Sprintf(" It keeps at most %s of other publishers' %s verdicts, and keeps that many now: newer ones "+
			"are not kept until older ones are forgotten.", count(in.totals.EndedMax), q.state)
	}
	if in.listErr != nil {
		p.Err = in.listErr.Error()
	}
	if in.addressErr != nil {
		p.AddressErr = in.addressErr.Error()
	}
	if in.searched.IsValid() {
		addr := rangeText(in.searched)
		p.Searched = &verdictsAddress{Address: addr, DecisionHref: decisionHref(in.searched), Command: "sudo obiectl show " + addr}
	}
	if in.totalsErr != nil {
		p.TotalsErr = in.totalsErr.Error()
	} else {
		p.Totals = newTotalsView(in.totals, in.peers, in.self)
	}
	if q.publisher != "" {
		p.Whose = newWhoseLine(q.publisher, in.self, &in.peers)
	}
	for _, s := range verdictScopes {
		f := q.first()
		f.from, f.publisher = s.from, ""
		p.Scopes = append(p.Scopes, peerFilter{Label: s.label, Current: q.publisher == "" && s.from == q.from, Href: verdictsHref(f)})
	}
	for _, s := range verdictStates {
		f := q.first()
		f.state = s.state
		name := s.state
		if name == "" {
			name = VerdictActive
		}
		p.States = append(p.States, peerFilter{Label: s.label, Count: in.list.States[name], Current: s.state == q.state,
			Href: verdictsHref(f)})
	}
	if q.filtered() {
		p.Clear = verdictsHref(verdictsQuery{from: q.from, state: q.state})
	}
	for i := range in.list.Items {
		p.Rows = append(p.Rows, newVerdictItemRow(&in.list.Items[i], in.self, names))
	}
	p.Pager = verdictsPagerOf(q, &in.list)
	if len(p.Rows) == 0 && in.listErr == nil {
		p.Empty = emptyVerdicts(q, p.Retention)
	}
	return p
}

// withTotals returns set counting every verdict of each publisher in t, so
// that the publisher filter offers also those whose verdicts all ended;
// set itself if t counts none.
func withTotals(set PeerSet, t VerdictTotals) PeerSet {
	if len(t.ByPublisher) == 0 {
		return set
	}
	set.Verdicts = make(map[string]VerdictCount, len(t.ByPublisher))
	for id, c := range t.ByPublisher {
		set.Verdicts[id] = VerdictCount{Held: c.Active + c.Revoked + c.Expired}
	}
	return set
}

// peerNames returns the names of the peers in trust.publishers, by peer ID.
func peerNames(set PeerSet) map[string]string {
	names := make(map[string]string, len(set.Peers))
	for _, p := range set.Peers {
		if p.Name != "" {
			names[p.ID] = p.Name
		}
	}
	return names
}

// weightOf returns the trust weight the decisions give the publisher id:
// trust.local_weight for this node, its weight in trust.publishers, else
// trust.default_weight.
func weightOf(set *PeerSet, id, self string) float64 {
	if id == self {
		return set.LocalWeight
	}
	for _, p := range set.Peers {
		if p.ID == id {
			return p.Weight
		}
	}
	return set.DefaultWeight
}

// publisherTitle names the publisher id: this node, its name in
// trust.publishers, or its shortened peer ID.
func publisherTitle(id, self string, names map[string]string) string {
	switch {
	case id == self:
		return "This node"
	case names[id] != "":
		return names[id]
	default:
		return shortPeerID(id)
	}
}

// peerHref links to the page of the peer id; this node has none.
func peerHref(id, self string) string {
	if id == self {
		return ""
	}
	return "/peers/" + url.PathEscape(id)
}

// verdictsHeading says what the list under q shows, on the address or
// network searched if it is one.
func verdictsHeading(q verdictsQuery, self string, names map[string]string, searched netip.Prefix) string {
	heading := "Active verdicts"
	switch q.state {
	case VerdictRevoked:
		heading = "Revoked verdicts"
	case VerdictExpired:
		heading = "Expired verdicts"
	}
	switch {
	case q.publisher == self || (q.publisher == "" && q.from == fromMine):
		heading += " of this node"
	case q.publisher != "":
		heading += " of " + publisherTitle(q.publisher, self, names)
	case q.from == fromPeers:
		heading += " received from other publishers"
	default:
		heading += " of every publisher"
	}
	if searched.IsValid() {
		heading += " on " + rangeText(searched)
	}
	return heading
}

// newWhoseLine names the publisher id whose verdicts are listed.
func newWhoseLine(id, self string, set *PeerSet) *whoseLine {
	w := weightOf(set, id, self)
	return &whoseLine{Title: publisherTitle(id, self, peerNames(*set)), PeerID: id, Href: peerHref(id, self),
		Weight: weight(w), NoWeight: id != self && !(w > 0)}
}

// newVerdictItemRow describes the verdict it for the list.
func newVerdictItemRow(it *VerdictItem, self string, names map[string]string) verdictItemRow {
	addr := rangeText(it.Range)
	r := verdictItemRow{
		Address:       addr,
		Href:          verdictsHref(verdictsQuery{address: addr}),
		DecisionHref:  decisionHref(it.Range),
		DecisionLabel: "No decision",
		DecisionState: stateIdle,
		Publisher:     publisherTitle(it.Publisher, self, names),
		PublisherHref: peerHref(it.Publisher, self),
		Weight:        weight(it.Weight),
		Action:        it.Action,
		Confidence:    score(it.Confidence),
		Reason:        reasonText(it.Reason, it.Protocol),
		Events:        plural(int(it.Events), "event", "events"),
		LogHash:       it.LogHash,
		EventID:       it.EventID,
		Issued:        stamp(it.IssuedAt),
		Expires:       stamp(it.ExpiresAt),
		State:         it.State,
	}
	r.NoWeight = !it.Local && it.Publisher != self && !(it.Weight > 0)
	if it.Decision != "" {
		r.DecisionLabel, r.DecisionState = stateLabel(it.Decision), it.Decision
	}
	switch it.State {
	case VerdictRevoked:
		r.StateLabel = "Revoked"
		if rev := it.Revocation; rev != nil {
			r.Ended, r.RevokeReason, r.RevokedByID = stamp(rev.At), rev.Reason, rev.ID
		}
	case VerdictExpired:
		r.StateLabel, r.Ended = "Expired", stamp(it.ExpiresAt)
	default:
		r.StateLabel = "Active"
		switch {
		case it.Counts:
			r.Counts = "Yes"
		case it.Action != "ban":
			r.Counts = "No: a " + it.Action + " verdict"
		default:
			r.Counts = "No: weight 0"
		}
	}
	return r
}

// newTotalsView counts this node's verdicts and those of every other
// publisher, the most active first.
func newTotalsView(t VerdictTotals, set PeerSet, self string) *verdictTotalsView {
	names := peerNames(set)
	v := &verdictTotalsView{Mine: totalsRowOf(t.ByPublisher[self], verdictsQuery{from: fromMine})}
	v.Mine.Title, v.Mine.Weight = "This node", weight(set.LocalWeight)
	var others []string
	var all VerdictCounts
	for id, c := range t.ByPublisher {
		if id == self || c == (VerdictCounts{}) {
			continue
		}
		others = append(others, id)
		all.Active, all.Counting = all.Active+c.Active, all.Counting+c.Counting
		all.Revoked, all.Expired = all.Revoked+c.Revoked, all.Expired+c.Expired
	}
	slices.SortFunc(others, func(a, b string) int {
		ca, cb := t.ByPublisher[a], t.ByPublisher[b]
		return cmp.Or(cmp.Compare(cb.Active, ca.Active), cmp.Compare(cb.Revoked+cb.Expired, ca.Revoked+ca.Expired),
			strings.Compare(strings.ToLower(names[a]), strings.ToLower(names[b])), strings.Compare(a, b))
	})
	v.Publishers = len(others)
	v.ReceivedAll = totalsRowOf(all, verdictsQuery{from: fromPeers})
	v.ReceivedAll.Title = "Every other publisher"
	if len(others) > maxTotalsRows {
		v.More, others = len(others)-maxTotalsRows, others[:maxTotalsRows]
	}
	for _, id := range others {
		row := totalsRowOf(t.ByPublisher[id], verdictsQuery{publisher: id})
		w := weightOf(&set, id, self)
		row.Title, row.PeerID, row.ShortID, row.Href = publisherTitle(id, self, names), id, shortPeerID(id), peerHref(id, self)
		row.Named, row.Weight, row.NoWeight = names[id] != "", weight(w), !(w > 0)
		v.Received = append(v.Received, row)
	}
	return v
}

// totalsRowOf counts c, each number linking to the list under q in its
// state.
func totalsRowOf(c VerdictCounts, q verdictsQuery) totalsRow {
	link := func(n int, state string) countLink {
		l := countLink{Count: n}
		if n > 0 {
			q.state = state
			l.Href = verdictsHref(q)
		}
		return l
	}
	_, note := heldText(VerdictCount{Held: c.Active, Counting: c.Counting})
	return totalsRow{Active: link(c.Active, ""), Revoked: link(c.Revoked, VerdictRevoked), Expired: link(c.Expired, VerdictExpired),
		CountingNote: note}
}

// verdictsPagerOf links the first and the following page of list.
func verdictsPagerOf(q verdictsQuery, list *VerdictList) verdictsPager {
	var pg verdictsPager
	n := len(list.Items)
	switch {
	case n == 0:
		return pg
	case list.Total > n:
		pg.Text = fmt.Sprintf("Verdicts %s–%s of %s", count(list.Offset+1), count(list.Offset+n), count(list.Total))
	default:
		pg.Text = plural(list.Total, "verdict", "verdicts")
	}
	if list.Offset > 0 {
		pg.First = verdictsHref(q.first())
	}
	if list.Next != "" {
		next := q.first()
		next.after = list.Next
		pg.Next = verdictsHref(next)
	}
	return pg
}

// emptyVerdicts explains why the list under q shows no verdict; retention
// says how long ended verdicts are kept.
func emptyVerdicts(q verdictsQuery, retention string) string {
	kept := ""
	if retention != "" {
		kept = " The node keeps a verdict that ended for " + retention + " after its expiry."
	}
	switch {
	case q.after != "":
		return "No verdict follows on this page any more: the verdicts changed since the link was made."
	case q.filtered():
		return "No verdict matches this view."
	case q.state == VerdictRevoked:
		return "No revoked verdict is kept." + kept
	case q.state == VerdictExpired:
		return "No expired verdict is kept." + kept
	case q.from == fromMine:
		return "This node has no active verdict: it reports an address with obiectl report, or the Fail2Ban action on every ban."
	case q.from == fromPeers:
		return "The node holds no active verdict of another publisher. Verdicts arrive once peers report addresses."
	default:
		return "The node holds no active verdict. Verdicts appear once this node or its peers report addresses " +
			"(obiectl report, or the Fail2Ban action on every ban)."
	}
}

// stateNoteOf says what the state tab state means, for the verdicts that
// ended, which the node keeps for retention after their expiry.
func stateNoteOf(state, retention string) string {
	kept := ""
	if retention != "" {
		kept = " The node keeps them for " + retention + " after their expiry, then forgets them."
	}
	switch state {
	case VerdictRevoked:
		return "Revoked verdicts: their publisher withdrew them, so they count in no decision any more." + kept
	case VerdictExpired:
		return "Expired verdicts: they reached their expiry unrevoked, so they count in no decision any more." + kept
	default:
		return ""
	}
}

// retentionText says how long ended verdicts or overrides are kept, e.g.
// "24 hours" or "7 days".
func retentionText(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d <= 0:
		return ""
	case d > day && d%day == 0:
		return plural(int(d/day), "day", "days")
	case d%time.Hour == 0:
		return plural(int(d/time.Hour), "hour", "hours")
	default:
		return humanDuration(d)
	}
}

// verdictsNotice explains that the part of the node the list in state
// comes from does not run: the decision engine for the active verdicts,
// the store for those that ended; empty while it runs.
func verdictsNotice(statuses []lifecycle.Status, state string) string {
	part, what := partDecision, "The decision engine"
	if state != "" {
		part, what = partStore, "The store"
	}
	for _, s := range statuses {
		if s.Name != part {
			continue
		}
		switch s.State {
		case lifecycle.StateRunning:
			return ""
		case lifecycle.StatePending, lifecycle.StateStarting:
			return what + " is starting: the verdicts appear once it runs."
		default:
			return what + " is not running: the verdicts cannot be read until it runs again."
		}
	}
	return ""
}

// verdictsContent reads a page of the verdicts and the totals under the
// request's scope, filters, state and page.
func (c *Console) verdictsContent(r *http.Request) any {
	q := parseVerdictsQuery(r.URL.Query())
	in := verdictsInput{now: c.now(), query: q, self: c.node.PeerID, peers: c.peerSet(),
		notice: verdictsNotice(c.node.Status(), q.state)}
	src := c.node.Verdicts
	if src == nil {
		in.listErr, in.totalsErr = errNoVerdicts, errNoVerdicts
		return buildVerdicts(in)
	}
	vq := VerdictQuery{State: VerdictActive, Category: q.reason, After: q.after, Limit: verdictsPageSize}
	if q.state != "" {
		vq.State = q.state
	}
	switch {
	case q.publisher != "":
		vq.Publisher = q.publisher
	case q.from == fromMine:
		vq.Publisher = c.node.PeerID
	case q.from == fromPeers:
		vq.Except = c.node.PeerID
	}
	if q.address != "" {
		p, err := parseRange(q.address)
		if err != nil {
			in.addressErr = fmt.Errorf("%w; the list is not narrowed to it", err)
		} else {
			vq.Range = p
		}
	}
	in.list, in.listErr = src.Verdicts(vq)
	switch {
	case errors.Is(in.listErr, ErrNoIndicator):
		in.addressErr, in.listErr = fmt.Errorf("%s is %w: no verdict can be on it", rangeText(vq.Range), ErrNoIndicator), nil
	case vq.Range.IsValid():
		in.searched = vq.Range
	}
	in.totals, in.totalsErr = src.Totals()
	in.categories = src.Categories()
	return buildVerdicts(in)
}

// errNoVerdicts says that the node passes the console no verdicts.
var errNoVerdicts = errors.New("the node passes the console no verdicts")
