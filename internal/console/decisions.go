package console

import (
	"cmp"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// decisionsPageSize is how many decisions a page of the list shows.
const decisionsPageSize = 50

// maxQueryValue bounds a value of the list's query; a longer one is
// ignored.
const maxQueryValue = 256

// Orders of the decisions list, as the decision engine names them.
const (
	sortDecided    = "decided"
	sortAddress    = "address"
	sortState      = "state"
	sortScore      = "score"
	sortPublishers = "publishers"
	sortExpires    = "expires"
)

// Node modes, as the configuration names them.
const modeObserve = "observe"

// decisionColumnOption is a column of the decisions list: the order it
// sorts by, empty if it does not sort, its heading and its direction for
// aria-sort.
type decisionColumnOption struct{ sort, label, direction string }

var (
	// decisionColumns are the list's columns in order.
	decisionColumns = []decisionColumnOption{
		{sortAddress, "Address", "ascending"},
		{sortState, "State", "other"},
		{sortScore, "Score", "descending"},
		{sortPublishers, "Publishers", "descending"},
		{"", "Reason", ""},
		{sortDecided, "Decided", "descending"},
		{sortExpires, "Expires", "ascending"},
		{"", "Firewall", ""},
	}
	// decisionStates are the state filters in the order the list offers
	// them.
	decisionStates = []struct{ state, label string }{
		{"", "All"}, {StateBlock, "Block"}, {StateAllowed, "Allowed"}, {StateNone, "None"},
	}
	// firewallFilters are the choices of the firewall filter.
	firewallFilters = []struct{ value, label string }{
		{"", "Applied or not"}, {FirewallApplied, "Applied by the firewall"}, {FirewallNotApplied, "Not applied"},
	}
)

// decisionsQuery is what the decisions list shows: filters, a search, an
// order and a page. It is the view's URL, so a link shares the view.
type decisionsQuery struct {
	state, reason, publisher, firewall string
	// search is the search as typed.
	search string
	sort   string
	// after, before and last select the page; none selects the first.
	after, before string
	last          bool
}

// parseDecisionsQuery reads the list's query; unknown values fall back to
// the defaults.
func parseDecisionsQuery(v url.Values) decisionsQuery {
	q := decisionsQuery{sort: sortDecided}
	if s := v.Get("state"); s == StateBlock || s == StateNone || s == StateAllowed {
		q.state = s
	}
	if s := v.Get("firewall"); s == FirewallApplied || s == FirewallNotApplied {
		q.firewall = s
	}
	if s := v.Get("sort"); s != "" && slices.ContainsFunc(decisionColumns, func(c decisionColumnOption) bool { return c.sort == s }) {
		q.sort = s
	}
	q.reason, q.publisher = bounded(v.Get("reason")), bounded(v.Get("publisher"))
	q.search = bounded(strings.TrimSpace(v.Get("q")))
	q.after, q.before, q.last = bounded(v.Get("after")), bounded(v.Get("before")), v.Get("last") == "1"
	return q
}

// bounded returns s, or "" if it is longer than maxQueryValue.
func bounded(s string) string {
	if len(s) > maxQueryValue {
		return ""
	}
	return s
}

// values returns q as a URL query, leaving out the defaults.
func (q decisionsQuery) values() url.Values {
	v := url.Values{}
	for _, kv := range [][2]string{{"q", q.search}, {"state", q.state}, {"reason", q.reason}, {"publisher", q.publisher},
		{"firewall", q.firewall}, {"after", q.after}, {"before", q.before}} {
		if kv[1] != "" {
			v.Set(kv[0], kv[1])
		}
	}
	if q.sort != sortDecided {
		v.Set("sort", q.sort)
	}
	if q.last {
		v.Set("last", "1")
	}
	return v
}

// href returns path with q as its query.
func (q decisionsQuery) href(path string) string {
	if v := q.values(); len(v) > 0 {
		return path + "?" + v.Encode()
	}
	return path
}

// first returns q on its first page.
func (q decisionsQuery) first() decisionsQuery {
	q.after, q.before, q.last = "", "", false
	return q
}

// filtered reports whether q filters or searches the decisions, the state
// aside.
func (q decisionsQuery) filtered() bool {
	return q.reason != "" || q.publisher != "" || q.firewall != "" || q.search != ""
}

// decisionsPage is the data of the decisions list.
type decisionsPage struct {
	// Status is the data of the refreshing region.
	Status decisionsStatus
	// Notice explains that the decision engine does not run; empty while
	// it does.
	Notice string
	// FirewallNote says how far the firewall column can be trusted, e.g.
	// in observe mode; empty if fully.
	FirewallNote string
	// Search is the search as typed; SearchErr says why it is no address
	// or network. Searched explains the searched address or network.
	Search    string
	SearchErr string
	Searched  *searchLine
	// State and Sort are kept by the filter form.
	State, Sort string
	Reasons     []option
	Publishers  []option
	Firewalls   []option
	// Clear links to the list without filters and search; empty without
	// any.
	Clear   string
	States  []peerFilter
	Columns []peerColumn
	Rows    []decisionRow
	// Empty explains why no decision is listed; empty if some are.
	Empty string
	Pager decisionsPager
}

// option is a choice of a filter.
type option struct {
	Value, Label string
	Selected     bool
}

// decisionsPager moves through the pages of the list.
type decisionsPager struct {
	// Text says which decisions the page shows.
	Text string
	// First, Prev, Next and Last link to those pages; empty if the page is
	// that page already, or there is none.
	First, Prev, Next, Last string
}

// decisionsStatus is the refreshing region of the list: when it was read
// and whether the decisions changed since. The list itself is read when
// the page opens (ADR 0022).
type decisionsStatus struct {
	// Fragment is the path and query of the region.
	Fragment string
	ReadAt   timestamp
	// Changed is set once the decisions or the firewall changed since the
	// list was read; Reload reads the same view again.
	Changed bool
	Reload  string
}

// searchLine explains a searched address or network in one line.
type searchLine struct {
	Range, Href string
	// Headline says whether it is blocked; Protection whether a rule of
	// the operator's covers it.
	Headline, Protection string
	// Err says why it could not be explained.
	Err string
}

// decisionRow is a decision as the list shows it.
type decisionRow struct {
	// Href links to its explanation; Address is the address or network.
	Href, Address string
	// State is the decision state for the stylesheet; StateLabel names it
	// and StateNote says what decided it.
	State, StateLabel, StateNote string
	// Score against the threshold and the publishers against the quorum.
	Score, ScoreNote           string
	Publishers, PublishersNote string
	// Reasons name the categories of its verdicts; ReasonsNote counts them.
	Reasons     []string
	ReasonsNote string
	Decided     timestamp
	Expires     timestamp
	// ExpiresNote says why there is no expiry.
	ExpiresNote string
	// Firewall says whether the firewall applies it: FirewallLabel, with
	// FirewallNote saying how or why not, FirewallState for the
	// stylesheet.
	FirewallState, FirewallLabel, FirewallNote string
}

// decisionsInput is what the decisions list is built from.
type decisionsInput struct {
	now   time.Time
	query decisionsQuery
	// searchErr says why the search is no address or network.
	searchErr  error
	page       DecisionPage
	categories map[string]int
	peers      PeerSet
	self       string
	firewall   Firewall
	// searched explains the searched range, if it is an indicator; nil
	// otherwise. searchedErr says why it could not be explained.
	searched    *Explanation
	searchedErr error
	notice      string
	// seq is the firewall's pass sequence the page was read at.
	seq uint64
}

// buildDecisions builds the list from a page of decisions.
func buildDecisions(in decisionsInput) decisionsPage {
	q := in.query
	p := decisionsPage{
		Notice:       in.notice,
		FirewallNote: firewallNote(&in.firewall),
		Search:       q.search,
		State:        q.state,
		Sort:         q.sort,
		Reasons:      reasonOptions(in.categories, q.reason, true),
		Publishers:   publisherOptions(in.peers, in.self, q.publisher),
	}
	status := url.Values{"gen": {strconv.FormatUint(in.page.Generation, 10)}, "seq": {strconv.FormatUint(in.seq, 10)},
		"read": {strconv.FormatInt(in.now.Unix(), 10)}}
	p.Status = decisionsStatus{Fragment: mergeQuery(q.href("/api/decisions"), status), ReadAt: stamp(in.now),
		Reload: q.href("/decisions")}
	if in.searchErr != nil {
		p.SearchErr = in.searchErr.Error()
	}
	if in.searched != nil || in.searchedErr != nil {
		p.Searched = newSearchLine(in.searched, in.searchedErr, q.search)
	}
	for _, f := range firewallFilters {
		p.Firewalls = append(p.Firewalls, option{Value: f.value, Label: f.label, Selected: f.value == q.firewall})
	}
	if q.filtered() {
		p.Clear = decisionsQuery{state: q.state, sort: q.sort}.href("/decisions")
	}
	all := 0
	for _, n := range in.page.States {
		all += n
	}
	for _, s := range decisionStates {
		n := all
		if s.state != "" {
			n = in.page.States[s.state]
		}
		f := q.first()
		f.state = s.state
		p.States = append(p.States, peerFilter{Label: s.label, Count: n, Current: s.state == q.state, Href: f.href("/decisions")})
	}
	for _, c := range decisionColumns {
		col := peerColumn{Label: c.label}
		if c.sort != "" {
			f := q.first()
			f.sort = c.sort
			col.Href = f.href("/decisions")
			if c.sort == q.sort {
				col.Sort = c.direction
			}
		}
		p.Columns = append(p.Columns, col)
	}
	for i := range in.page.Items {
		p.Rows = append(p.Rows, newDecisionRow(&in.page.Items[i], &in.firewall, in.now))
	}
	p.Pager = decisionsPagerOf(q, &in.page)
	if len(p.Rows) == 0 {
		p.Empty = emptyDecisions(q, all)
	}
	return p
}

// mergeQuery adds extra to the query of href.
func mergeQuery(href string, extra url.Values) string {
	path, query, _ := strings.Cut(href, "?")
	v, _ := url.ParseQuery(query) // our own encoding
	for k, vs := range extra {
		v[k] = vs
	}
	return path + "?" + v.Encode()
}

// decisionsPagerOf links the pages around page.
func decisionsPagerOf(q decisionsQuery, page *DecisionPage) decisionsPager {
	var pg decisionsPager
	n := len(page.Items)
	switch {
	case n == 0:
		return pg
	case page.Total > n:
		pg.Text = fmt.Sprintf("Decisions %s–%s of %s", count(page.Offset+1), count(page.Offset+n), count(page.Total))
	default:
		pg.Text = plural(page.Total, "decision", "decisions")
	}
	if page.Offset > 0 {
		pg.First = q.first().href("/decisions")
		prev := q.first()
		prev.before = page.Items[0].Cursor
		pg.Prev = prev.href("/decisions")
	}
	if page.Offset+n < page.Total {
		next := q.first()
		next.after = page.Items[n-1].Cursor
		pg.Next = next.href("/decisions")
		last := q.first()
		last.last = true
		pg.Last = last.href("/decisions")
	}
	return pg
}

// emptyDecisions explains why the list shows no decision; all counts the
// decisions its filters select in any state.
func emptyDecisions(q decisionsQuery, all int) string {
	switch {
	case q.filtered() || (q.state != "" && all > 0):
		return "No decision matches this view."
	case q.after != "" || q.before != "":
		return "No decision follows on this page any more: the decisions changed since the link was made."
	default:
		return "The node holds no decision: no active verdict and no force-block. Decisions appear once this node " +
			"or its peers report addresses (obiectl report, or the Fail2Ban action on every ban)."
	}
}

// reasonOptions offers the categories of the held verdicts, the most
// frequent first, with how many decisions hold one if counted; the
// selected one stays offered when no decision holds it any more.
func reasonOptions(categories map[string]int, selected string, counted bool) []option {
	type category struct {
		name string
		n    int
	}
	var cs []category
	for name, n := range categories {
		cs = append(cs, category{name, n})
	}
	slices.SortFunc(cs, func(a, b category) int { return cmp.Or(cmp.Compare(b.n, a.n), strings.Compare(a.name, b.name)) })
	out := []option{{Value: "", Label: "Every reason", Selected: selected == ""}}
	found := selected == ""
	label := func(name string, n int) string {
		if !counted {
			return categoryText(name)
		}
		return fmt.Sprintf("%s (%s)", categoryText(name), count(n))
	}
	for _, c := range cs {
		out = append(out, option{Value: c.name, Label: label(c.name, c.n), Selected: c.name == selected})
		found = found || c.name == selected
	}
	if !found {
		out = append(out, option{Value: selected, Label: label(selected, 0), Selected: true})
	}
	return out
}

// publisherOptions offers the publishers the node holds verdicts of,
// named ones first; the selected one stays offered.
func publisherOptions(set PeerSet, self, selected string) []option {
	names := make(map[string]string, len(set.Peers))
	for _, p := range set.Peers {
		names[p.ID] = p.Name
	}
	var ids []string
	for id, c := range set.Verdicts {
		if c.Held > 0 {
			ids = append(ids, id)
		}
	}
	if selected != "" && !slices.Contains(ids, selected) {
		ids = append(ids, selected)
	}
	label := func(id string) string {
		switch {
		case id == self:
			return "This node"
		case names[id] != "":
			return names[id] + " (" + shortPeerID(id) + ")"
		default:
			return shortPeerID(id)
		}
	}
	rank := func(id string) int {
		switch {
		case id == self:
			return 0
		case names[id] != "":
			return 1
		default:
			return 2
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), strings.Compare(strings.ToLower(names[a]), strings.ToLower(names[b])),
			strings.Compare(a, b))
	})
	out := []option{{Value: "", Label: "Every publisher", Selected: selected == ""}}
	for _, id := range ids {
		out = append(out, option{Value: id, Label: fmt.Sprintf("%s: %s", label(id), plural(set.Verdicts[id].Held, "verdict", "verdicts")),
			Selected: id == selected})
	}
	return out
}

// newDecisionRow describes the decision it for the list.
func newDecisionRow(it *DecisionItem, fw *Firewall, now time.Time) decisionRow {
	r := decisionRow{
		Href:           decisionHref(it.Range),
		Address:        rangeText(it.Range),
		State:          it.State,
		StateLabel:     stateLabel(it.State),
		StateNote:      stateNote(it),
		Score:          score(it.Score),
		ScoreNote:      "threshold " + score(it.Threshold) + reached(it.Score >= it.Threshold-scoreTolerance),
		Publishers:     count(it.Contributors),
		PublishersNote: "quorum " + count(it.Quorum) + reached(it.Contributors >= it.Quorum),
		ReasonsNote:    plural(it.Verdicts, "active verdict", "active verdicts"),
		Decided:        stamp(it.DecidedAt),
		Expires:        stamp(it.ExpiresAt),
	}
	for _, c := range it.Categories {
		r.Reasons = append(r.Reasons, categoryText(c))
	}
	if it.ExpiresAt.IsZero() {
		r.ExpiresNote = "Not a block"
	}
	r.FirewallState, r.FirewallLabel, r.FirewallNote = firewallCell(it, fw, now)
	return r
}

// scoreTolerance absorbs float rounding like the decision engine does, so
// that 0.6 + 0.6 + 0.6 reaches 1.8.
const scoreTolerance = 1e-9

// reached says whether a threshold or quorum is reached.
func reached(ok bool) string {
	if ok {
		return ": reached"
	}
	return ": not reached"
}

// score formats a score, weight or confidence like obiectl, rounded to
// four decimals.
func score(f float64) string {
	return strconv.FormatFloat(math.Round(f*1e4)/1e4, 'f', -1, 64)
}

// stateLabel names a decision state.
func stateLabel(state string) string {
	switch state {
	case StateBlock:
		return "Block"
	case StateAllowed:
		return "Allowed"
	case StateNone:
		return "None"
	default:
		return state
	}
}

// stateNote says what decided the state of it.
func stateNote(it *DecisionItem) string {
	switch {
	case it.Rule == ruleForceBlock:
		return "operator force-block"
	case it.Rule == ruleForceAllow:
		return "operator force-allow"
	case it.Rule == ruleAllowlist && it.Protected:
		return "protected address"
	case it.Rule == ruleAllowlist:
		return "allow-list"
	case it.Autoblock:
		return "local autoblock"
	case it.State == StateBlock:
		return "consensus"
	default:
		return "below consensus"
	}
}

// Rules of the operator, as the decision engine names them.
const (
	ruleForceAllow = "force_allow"
	ruleAllowlist  = "allowlist"
	ruleForceBlock = "force_block"
)

// firewallCell says whether the firewall applies the decision it:
// the state for the stylesheet, a label and a note.
func firewallCell(it *DecisionItem, fw *Firewall, now time.Time) (state, label, note string) {
	cov, block := &it.Firewall, it.State == StateBlock
	switch {
	case fw.Pass == nil:
		return stateIdle, "Not known yet", "no enforcement pass has run yet"
	case fw.Pass.Mode == modeObserve && block:
		return stateIdle, "Not applied", "observe mode: nothing is applied, by design"
	case fw.Pass.Mode == modeObserve:
		return stateIdle, "Not applied", "observe mode"
	case cov.Applied && cov.Entry == it.Range:
		return stateReady, "Applied", "its own entry"
	case cov.Applied && block:
		return stateReady, "Applied", "through the entry for " + rangeText(cov.Entry)
	case cov.Applied:
		return stateWarning, "Applied", "the entry for " + rangeText(cov.Entry) + " wins"
	case !block:
		return stateIdle, "Not applied", "not a block"
	case cov.Gone:
		return stateWarning, "Not in the firewall", "applied by the last pass, gone since; the next pass adds it again"
	case cov.Skipped != "":
		label, note = skipText(cov.Skipped)
		if cov.Within != it.Range {
			note += ", with " + rangeText(cov.Within)
		}
		return stateWarning, label, note
	case cov.Deferred:
		return stateWarning, "Waiting", "until an entry it overlaps expires"
	default:
		return stateWarning, "Not applied yet", notYetApplied(it, fw.Pass, now)
	}
}

// skipText names why a block is skipped.
func skipText(reason string) (label, note string) {
	switch reason {
	case SkipAllowlist:
		return "Refused", "by the allow-list right before apply"
	case SkipMaxEntries:
		return "Left out", "over enforce.max_entries"
	default:
		return "Skipped", strings.ReplaceAll(reason, "_", " ")
	}
}

// notYetApplied says why the last pass did not apply the block it.
func notYetApplied(it *DecisionItem, pass *FirewallPass, now time.Time) string {
	switch {
	case it.DecidedAt.After(pass.At):
		return "decided after the last pass; the next one applies it"
	case it.ExpiresAt.Sub(now) < time.Second:
		return "it expires within a second"
	default:
		return "the next pass applies it"
	}
}

// firewallNote says what the firewall column shows, when not simply the
// last pass in enforce mode.
func firewallNote(fw *Firewall) string {
	switch {
	case fw.Pass == nil:
		return "The firewall has not run an enforcement pass yet, so the list cannot say what it applies."
	case fw.Pass.Mode == modeObserve:
		return "Observe mode: the firewall applies nothing, by design. Decisions are made and logged; set node.mode " +
			"to enforce to apply the blocks."
	case fw.Facts.Failures > 0:
		return fmt.Sprintf("The last enforcement pass failed (%s). The firewall column shows the last successful "+
			"pass, at %s.", fw.Facts.Err, stamp(fw.Pass.At).Text)
	default:
		return ""
	}
}

// newSearchLine explains a searched range in one line.
func newSearchLine(ex *Explanation, err error, search string) *searchLine {
	if ex == nil {
		return &searchLine{Range: search, Err: err.Error()}
	}
	return &searchLine{Range: rangeText(ex.Range), Href: decisionHref(ex.Range), Headline: headline(ex),
		Protection: protection(&ex.Ruling)}
}

// headline says in one line whether the explained range is blocked.
func headline(ex *Explanation) string {
	switch {
	case ex.State == StateBlock && ex.Ruling.Rule == ruleForceBlock:
		return "Blocked by the operator's force-block until " + stamp(ex.ExpiresAt).Text
	case ex.State == StateBlock && ex.Autoblock:
		return "Blocked by this node's own verdict (local autoblock) until " + stamp(ex.ExpiresAt).Text
	case ex.State == StateBlock:
		return "Blocked by consensus until " + stamp(ex.ExpiresAt).Text
	case ex.State == StateAllowed:
		return "Allowed: never blocked, whatever the verdicts"
	case len(ex.Verdicts) == 0:
		return "No verdicts, not blocked"
	default:
		return "Not blocked: below consensus"
	}
}

// protection says whether a rule of the operator's covers a range.
func protection(r *Ruling) string {
	switch {
	case r.Rule == ruleAllowlist && r.Protected:
		return "Protected: " + r.Reason + ". Nothing blocks it, not even a force-block."
	case r.Rule == ruleAllowlist:
		return "On the allow-list: " + r.Reason + ". Only the operator's force-block overrules it."
	case r.Rule == ruleForceAllow:
		return "Force-allowed: " + r.Reason + "."
	case r.Rule == ruleForceBlock:
		return "Force-blocked: " + r.Reason + ". It overrules the allow-list, but not a protected address."
	default:
		return "Not protected: no allow-list entry or override covers it."
	}
}

// categoryText formats a category, "password_bruteforce/ssh", as
// "password_bruteforce (ssh)".
func categoryText(category string) string {
	reason, protocol, _ := strings.Cut(category, "/")
	return reasonText(reason, protocol)
}

// reasonText formats a verdict's reason and protocol like the peer page:
// "password_bruteforce (ssh)".
func reasonText(reason, protocol string) string {
	if protocol == "" {
		return reason
	}
	return reason + " (" + protocol + ")"
}

// rangeText formats a range: an address alone, a network with its length.
func rangeText(p netip.Prefix) string {
	if p.IsValid() && p.Bits() == p.Addr().BitLen() {
		return p.Addr().String()
	}
	return p.String()
}

// decisionHref links to the explanation of a range.
func decisionHref(p netip.Prefix) string {
	return "/decisions/" + rangeText(p)
}

// parseRange reads an address or network as an operator types it: an
// IPv4 or IPv6 address, a CIDR range, or an indicator key such as
// "ipv4:203.0.113.7".
func parseRange(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if kind, value, ok := strings.Cut(s, ":"); ok && (kind == "ipv4" || kind == "ipv6" || kind == "cidr") {
		s = value
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil || addr.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("%q is not an IP address or network", s)
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// decisionNotice explains that the decision engine does not run, from its
// lifecycle status; empty while it runs.
func decisionNotice(statuses []lifecycle.Status) string {
	for _, s := range statuses {
		if s.Name != partDecision {
			continue
		}
		switch s.State {
		case lifecycle.StateRunning:
			return ""
		case lifecycle.StatePending, lifecycle.StateStarting:
			return "The decision engine is starting: the decisions appear once it has read the store."
		default:
			return "The decision engine is not running: the decisions shown are those it held when it stopped, if any."
		}
	}
	return ""
}

// decisionsContent reads a page of the decisions under the request's
// filters, search, order and page.
func (c *Console) decisionsContent(r *http.Request) any {
	q := parseDecisionsQuery(r.URL.Query())
	in := decisionsInput{now: c.now(), query: q, self: c.node.PeerID, peers: c.peerSet(),
		notice: decisionNotice(c.node.Status())}
	src := c.node.Decisions
	if src == nil {
		return buildDecisions(in)
	}
	in.firewall = src.Firewall()
	if in.firewall.Pass != nil {
		in.seq = in.firewall.Pass.Seq
	}
	dq := DecisionQuery{State: q.state, Category: q.reason, Publisher: q.publisher, Firewall: q.firewall, Sort: q.sort,
		After: q.after, Before: q.before, Last: q.last, Limit: decisionsPageSize}
	if q.search != "" {
		p, err := parseRange(q.search)
		if err != nil {
			in.searchErr = fmt.Errorf("%w; the list is not searched", err)
		} else {
			dq.Search = p
			ex, err := src.Explain(p)
			if err == nil {
				in.searched = &ex
			} else {
				in.searchedErr = err
			}
		}
	}
	in.page = src.Decisions(dq)
	in.categories = src.Categories()
	return buildDecisions(in)
}

// decisionsStatusContent tells an open list whether the decisions or the
// firewall changed since it was read: cheap, without reading the list.
func (c *Console) decisionsStatusContent(r *http.Request) any {
	v := r.URL.Query()
	q := parseDecisionsQuery(v)
	gen, _ := strconv.ParseUint(v.Get("gen"), 10, 64)
	seq, _ := strconv.ParseUint(v.Get("seq"), 10, 64)
	read, _ := strconv.ParseInt(v.Get("read"), 10, 64)
	s := decisionsStatus{Reload: q.href("/decisions")}
	if read > 0 {
		s.ReadAt = stamp(time.Unix(read, 0))
	}
	if src := c.node.Decisions; src != nil {
		fw := src.Firewall()
		s.Changed = src.Generation() != gen || (fw.Pass != nil && fw.Pass.Seq != seq)
	}
	return s
}
