package console

import (
	"cmp"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Bounds of the activity timeline (ADR 0025).
const (
	// activityPageSize is how many entries a page of the timeline lists.
	activityPageSize = 100
	// liveLimit is how many entries one answer of the live feed lists one
	// by one; the rest are summarized.
	liveLimit = 50
	// liveMaxRows is how many rows the open timeline keeps; the script
	// takes older ones off.
	liveMaxRows = 500
	// recentEntries is how many entries the overview shows.
	recentEntries = 5
)

// activityKind is a kind of activity the timeline filters by: the audit
// actions it groups.
type activityKind struct {
	value, label string
	actions      []string
}

// activityKinds are the kinds in the order the filter offers them.
var activityKinds = []activityKind{
	{"blocks", "Blocks added, updated or removed", []string{actionBlockAdded, actionBlockUpdated, actionBlockRemoved}},
	{"allowlist", "Spared by the allow-list", []string{actionAllowed}},
	{"overrides", "Overrides set or removed", []string{actionOverrideSet, actionOverrideRemoved}},
	{"reports", "Reports of this node", []string{actionLocalReport}},
	{"revocations", "Revocations of this node", []string{actionRevocation}},
	{"peers", "Peers connecting or disconnecting", []string{actionPeerConnected, actionPeerDisconnected}},
	{"reloads", "Configuration reloads", []string{actionConfigReloaded}},
	{"mode", "Mode changes", []string{actionModeChanged}},
}

// The audit actions the timeline shows (ADR 0015, ADR 0025).
const (
	actionBlockAdded       = "block-added"
	actionBlockUpdated     = "block-updated"
	actionBlockRemoved     = "block-removed"
	actionAllowed          = "allowed-by-allowlist"
	actionOverrideSet      = "override-set"
	actionOverrideRemoved  = "override-removed"
	actionLocalReport      = "local-report"
	actionRevocation       = "revocation"
	actionPeerConnected    = "peer-connected"
	actionPeerDisconnected = "peer-disconnected"
	actionConfigReloaded   = "config-reloaded"
	actionModeChanged      = "mode-changed"
)

// actionNames name an action for one entry and for several, and give the
// kind it belongs to.
var actionNames = map[string]struct{ kind, one, many string }{
	actionBlockAdded:       {"blocks", "Block added", "blocks added"},
	actionBlockUpdated:     {"blocks", "Block updated", "blocks updated"},
	actionBlockRemoved:     {"blocks", "Block removed", "blocks removed"},
	actionAllowed:          {"allowlist", "Spared by the allow-list", "addresses spared by the allow-list"},
	actionOverrideSet:      {"overrides", "Override set", "overrides set"},
	actionOverrideRemoved:  {"overrides", "Override removed", "overrides removed"},
	actionLocalReport:      {"reports", "Reported by this node", "reports of this node"},
	actionRevocation:       {"revocations", "Verdict revoked", "verdicts revoked"},
	actionPeerConnected:    {"peers", "Peer connected", "peers connected"},
	actionPeerDisconnected: {"peers", "Peer disconnected", "peers disconnected"},
	actionConfigReloaded:   {"reloads", "Configuration reloaded", "configuration reloads"},
	actionModeChanged:      {"mode", "Mode changed", "mode changes"},
}

// activityQuery is what the timeline shows. It is the view's URL, so a
// link shares the view.
type activityQuery struct {
	// kind is one of activityKinds or "" for all.
	kind string
	// address is the address or network as typed.
	address string
	// before is where the page starts; "" for the newest entries.
	before string
}

// parseActivityQuery reads the view's query; unknown values fall back to
// the defaults.
func parseActivityQuery(v url.Values) activityQuery {
	q := activityQuery{address: bounded(strings.TrimSpace(v.Get("address"))), before: bounded(v.Get("before"))}
	if k := v.Get("kind"); slices.ContainsFunc(activityKinds, func(ak activityKind) bool { return ak.value == k }) {
		q.kind = k
	}
	return q
}

// href returns path with q as its query, leaving out the defaults.
func (q activityQuery) href(path string) string {
	v := url.Values{}
	for _, kv := range [][2]string{{"kind", q.kind}, {"address", q.address}, {"before", q.before}} {
		if kv[1] != "" {
			v.Set(kv[0], kv[1])
		}
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

// filter returns the entries q selects; the address as parsed is search,
// invalid if q names none or it is no address.
func (q activityQuery) filter(search netip.Prefix) ActivityFilter {
	f := ActivityFilter{Range: search}
	for _, k := range activityKinds {
		if k.value == q.kind {
			f.Actions = k.actions
		}
	}
	return f
}

// activityRow is an entry as the timeline shows it.
type activityRow struct {
	Time timestamp
	// Kind is the entry's kind for the stylesheet; Label says what
	// happened.
	Kind, Label string
	// Subject is the address, peer or setting it is about, Href links to
	// it: the decision, the peer or the configuration. Title, if set, is
	// the peer's full ID.
	Subject, Href, Title string
	// Mono is set if Subject is an address or an ID.
	Mono bool
	// Reason is the audit record's reason.
	Reason string
	// Facts say more in a few words, e.g. when a block ends.
	Facts []string
	// Links lead on to related views.
	Links []link
}

// link is a link with its text.
type link struct {
	Text, Href string
}

// newActivityRow describes the entry e.
func newActivityRow(e *ActivityEntry) activityRow {
	names, known := actionNames[e.Action]
	r := activityRow{Time: stamp(e.Time), Kind: names.kind, Label: names.one, Reason: e.Reason}
	if !known {
		r.Kind, r.Label = "other", e.Action
	}
	if e.Range.IsValid() {
		addr := rangeText(e.Range)
		r.Subject, r.Href, r.Mono = addr, decisionHref(e.Range), true
		r.Links = addressLinks(e, addr)
	}
	switch e.Action {
	case actionAllowed:
		if e.Rule == ruleForceAllow {
			r.Label = "Spared by an always-allow override"
		}
	case actionOverrideSet, actionOverrideRemoved:
		r.Label = overrideLabel(e)
	case actionPeerConnected, actionPeerDisconnected:
		r.Subject, r.Href, r.Title, r.Mono = shortPeerID(e.PeerID), "/peers/"+url.PathEscape(e.PeerID), e.PeerID, true
		if e.PeerName != "" {
			r.Subject, r.Mono = e.PeerName, false
		}
	case actionConfigReloaded:
		r.Subject, r.Href = "Configuration", "/configuration"
	case actionModeChanged:
		r.Label = "Mode changed to " + modeText(e.Mode)
		r.Subject, r.Href = "node.mode", "/configuration#section-node"
	}
	r.Facts = entryFacts(e)
	return r
}

// overrideLabel names an override being set or removed by its rule.
func overrideLabel(e *ActivityEntry) string {
	verb := "set"
	if e.Action == actionOverrideRemoved {
		verb = "removed"
	}
	switch e.Rule {
	case ruleForceAllow:
		return "Always allow " + verb
	case ruleForceBlock:
		return "Always block " + verb
	default:
		return "Override " + verb
	}
}

// modeText names a mode for people.
func modeText(mode string) string {
	if label, ok := modeLabels[mode]; ok {
		return label
	}
	return mode
}

// addressLinks lead from an entry about the address addr to the
// overrides or this node's verdicts on it.
func addressLinks(e *ActivityEntry, addr string) []link {
	switch e.Action {
	case actionOverrideSet, actionOverrideRemoved:
		return []link{{"Overrides on " + addr, overridesQuery{address: addr}.href()}}
	case actionLocalReport:
		return []link{{"This node's verdicts on " + addr, verdictsQuery{from: fromMine, address: addr}.href("/verdicts")}}
	case actionRevocation:
		return []link{{"Its revoked verdicts on " + addr,
			verdictsQuery{from: fromMine, address: addr, state: VerdictRevoked}.href("/verdicts")}}
	}
	return nil
}

// entryFacts says in a few words what else the record holds.
func entryFacts(e *ActivityEntry) []string {
	var facts []string
	if who := originText(e); who != "" {
		facts = append(facts, who)
	}
	switch e.Action {
	case actionBlockAdded, actionBlockUpdated, actionOverrideSet, actionLocalReport:
		if !e.ExpiresAt.IsZero() {
			facts = append(facts, "Until "+stamp(e.ExpiresAt).Text)
		} else if e.Action != actionLocalReport {
			facts = append(facts, "No end")
		}
	}
	if e.Note != "" {
		facts = append(facts, "Note: "+e.Note)
	}
	if e.Cause != "" {
		facts = append(facts, "Cause: "+e.Cause)
	}
	if (e.Action == actionBlockAdded || e.Action == actionBlockUpdated) && e.Mode == "observe" {
		facts = append(facts, "Observe mode: not applied to the firewall")
	}
	return facts
}

// originText says who carried out an operator action and through which
// door; empty for changes no operator made (ADR 0026).
func originText(e *ActivityEntry) string {
	var door string
	switch e.Origin {
	case "console":
		door = "in the console"
	case "admin-api":
		door = "with obiectl or another client of the admin socket"
	default:
		return ""
	}
	who := "an operator"
	switch {
	case e.UserName != "" && e.UserID != "":
		who = e.UserName + " (uid " + e.UserID + ")"
	case e.UserID != "":
		who = "uid " + e.UserID
	}
	return "By " + who + " " + door
}

// activityPage is the data of the activity timeline.
type activityPage struct {
	// Source says where the history comes from; Warning is set if some of
	// it is not available, and SettingHref then links to audit.path.
	Source      string
	Warning     bool
	SettingHref string
	// Kinds are the options of the kind filter; Address is kept by the
	// filter form, AddressErr says why it is none.
	Kinds               []option
	Address, AddressErr string
	// Clear links to the timeline without filters; empty without any.
	Clear   string
	Heading string
	// Err says why the timeline could not be read.
	Err  string
	Rows []activityRow
	// Empty explains why no entry is listed; empty if some are.
	Empty string
	// Older links to the next older page, OlderText names it; Newest links
	// back to the newest entries on an older page.
	Older, OlderText, Newest string
	// Searched says how far back a page looked that found fewer entries
	// than it lists; End says that the list reached the first entry kept.
	Searched, End string
	// Skipped says that lines of the file are no records.
	Skipped string
	// Live is the live feed of the first page: its endpoint with the
	// filters and where it continues; empty on older pages.
	Live string
	// Reload reloads the first page with the filters.
	Reload  string
	MaxRows int
}

// activityInput is what the timeline is built from.
type activityInput struct {
	query activityQuery
	// search is the address or network the list is narrowed to;
	// addressErr says why the address typed is none.
	search     netip.Prefix
	addressErr error
	page       ActivityPage
	// err says why the page could not be read.
	err       error
	startedAt time.Time
}

// buildActivity builds the activity timeline.
func buildActivity(in activityInput) activityPage {
	q, pg := in.query, &in.page
	first := activityQuery{kind: q.kind, address: q.address}
	p := activityPage{Address: q.address, Heading: activityHeading(q.kind, in.search), MaxRows: liveMaxRows,
		Reload: first.href("/activity")}
	if in.addressErr != nil {
		p.AddressErr = in.addressErr.Error()
	}
	for _, k := range append([]activityKind{{value: "", label: "All activity"}}, activityKinds...) {
		p.Kinds = append(p.Kinds, option{Value: k.value, Label: k.label, Selected: k.value == q.kind})
	}
	if q.kind != "" || q.address != "" {
		p.Clear = "/activity"
	}
	if in.err != nil {
		p.Err = in.err.Error()
		return p
	}
	p.Source, p.Warning = activitySource(pg, in.startedAt)
	if p.Warning {
		p.SettingHref = "/configuration#section-audit"
	}
	for i := range pg.Entries {
		p.Rows = append(p.Rows, newActivityRow(&pg.Entries[i]))
	}
	if q.before == "" {
		live := url.Values{"after": {strconv.FormatUint(pg.Live, 10)}}
		if q.kind != "" {
			live.Set("kind", q.kind)
		}
		if in.search.IsValid() {
			live.Set("address", q.address)
		}
		p.Live = "/api/activity?" + live.Encode()
	} else {
		p.Newest = p.Reload
	}
	if pg.Older != "" {
		older := first
		older.before = pg.Older
		p.Older, p.OlderText = older.href("/activity"), "Older entries"
	}
	if pg.Searched && pg.Older != "" {
		p.Searched = "This page searched the audit log as far as it reads at once and found no more entries for this view."
		if !pg.SearchedTo.IsZero() {
			p.Searched = "This page searched the audit log back to " + stamp(pg.SearchedTo).Text + " and found no more entries for this view."
		}
		p.OlderText = "Search further back"
	}
	if pg.Skipped > 0 {
		p.Skipped = plural(pg.Skipped, "line", "lines") + " of the audit log file on this page " +
			isAre(pg.Skipped) + " no OBIE audit record and " + wasWere(pg.Skipped) + " skipped."
	}
	if pg.Older == "" && len(p.Rows) > 0 {
		p.End = activityEnd(pg, q.kind != "" || in.search.IsValid())
	}
	if len(p.Rows) == 0 {
		p.Empty = emptyActivity(q, pg, in.startedAt)
	}
	return p
}

// activitySource says where the timeline's history comes from, and
// whether some of it is not available (ADR 0025).
func activitySource(p *ActivityPage, startedAt time.Time) (text string, warning bool) {
	started := stamp(startedAt).Text
	kept := "at most the last " + count(p.Kept) + " entries"
	switch {
	case p.Path == "":
		return "The audit log is off: audit.path is not set. So this timeline cannot show what happened before obied " +
			"started at " + started + ", keeps " + kept + " in memory and loses them at the next restart, and no SIEM " +
			"receives them. Live updates work. To keep the history, set audit.path and restart obied.", true
	case p.Memory && p.FileErr != "":
		return "The audit log file " + p.Path + " cannot be read here: " + p.FileErr + ". So this timeline cannot show " +
			"what happened before obied started at " + started + "; it shows the entries kept in memory since, " + kept +
			". Live updates work.", true
	case p.Memory:
		return "From the entries kept in memory since obied started at " + started + ", " + kept + ".", false
	case p.FileErr != "":
		return "From the audit log file " + p.Path + ". " + upperFirst(p.FileErr) + ", so this page starts again at " +
			"the newest entries.", false
	default:
		file := "From the audit log file " + p.Path
		if !p.FileSince.IsZero() {
			file += ", which holds the records since " + stamp(p.FileSince).Text
		}
		return file + ": the records a SIEM reads from it, also those from before obied last started. Only the " +
			"current file is read: what logrotate moved away is in the rotated files and in your SIEM.", false
	}
}

// activityEnd says where the list ends; filtered is set if a filter
// narrows it.
func activityEnd(p *ActivityPage, filtered bool) string {
	switch {
	case filtered && !p.Memory:
		return "No older entry of the current audit log file matches this view."
	case filtered:
		return "No older entry kept in memory matches this view."
	case !p.Memory:
		return "This is the first entry of the current audit log file."
	case p.Forgotten:
		return "Older entries since obied started are no longer kept in memory."
	default:
		return "This is the first entry since obied started."
	}
}

// activityHeading says what the list shows.
func activityHeading(kind string, search netip.Prefix) string {
	heading := "All activity"
	for _, k := range activityKinds {
		if k.value == kind {
			heading = k.label
		}
	}
	if search.IsValid() {
		heading += " on or around " + rangeText(search)
	}
	return heading
}

// emptyActivity explains why the timeline under q lists no entry.
func emptyActivity(q activityQuery, p *ActivityPage, startedAt time.Time) string {
	switch {
	case q.kind != "" || q.address != "":
		if p.Searched {
			return "No activity matches this view in the part of the audit log searched so far."
		}
		return "No activity matches this view."
	case q.before != "":
		return "No older activity."
	case p.Memory:
		return "Nothing has happened since obied started at " + stamp(startedAt).Text + "."
	default:
		return "The audit log holds no activity yet."
	}
}

// activityContent reads the page of the timeline the request names.
func (c *Console) activityContent(r *http.Request) any {
	q := parseActivityQuery(r.URL.Query())
	in := activityInput{query: q, startedAt: c.node.StartedAt}
	if q.address != "" {
		p, err := parseRange(q.address)
		if err != nil {
			in.addressErr = fmt.Errorf("%w; the list is not narrowed to it", err)
		} else {
			in.search = p
		}
	}
	if c.node.Activity == nil {
		in.err = errNoActivity
		return buildActivity(in)
	}
	in.page, in.err = c.node.Activity.Timeline(q.filter(in.search), q.before, activityPageSize)
	return buildActivity(in)
}

// errNoActivity says that the node passes the console no activity.
var errNoActivity = errors.New("the node passes the console no audit trail")

// liveFragment is what one answer of the live feed shows.
type liveFragment struct {
	// Next is where the feed continues.
	Next uint64
	Rows []activityRow
	// Burst summarizes the entries not listed one by one; nil if none.
	Burst *burstRow
}

// burstRow says what a burst left out of the live feed.
type burstRow struct {
	Text string
	// Reload reloads the timeline, which lists them all.
	Reload string
}

// liveTemplate renders an answer of the live feed.
var liveTemplate = template.Must(template.New("activity_rows.html").Funcs(templateFuncs).
	ParseFS(templateFiles, "templates/activity_rows.html")).Lookup("live")

// serveActivityLive answers the live feed of the timeline: the entries
// after the one numbered by the query's after, under its filters, as rows
// for the script to put on top (ADR 0025).
func (c *Console) serveActivityLive(w http.ResponseWriter, r *http.Request) {
	q := parseActivityQuery(r.URL.Query())
	after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	if err != nil {
		http.Error(w, "bad request: after must be the number of an entry", http.StatusBadRequest)
		return
	}
	var search netip.Prefix
	if q.address != "" {
		if search, err = parseRange(q.address); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	if c.node.Activity == nil {
		c.render(w, http.StatusOK, liveTemplate, liveFragment{Next: after})
		return
	}
	b := c.node.Activity.Live(q.filter(search), after, liveLimit)
	c.render(w, http.StatusOK, liveTemplate, buildLive(&b, activityQuery{kind: q.kind, address: q.address}.href("/activity")))
}

// buildLive turns a batch of the live feed into rows, and what it left
// out into a summary; reload reloads the timeline.
func buildLive(b *ActivityBatch, reload string) liveFragment {
	f := liveFragment{Next: b.Next}
	for i := range b.Entries {
		f.Rows = append(f.Rows, newActivityRow(&b.Entries[i]))
	}
	if text := burstText(b); text != "" {
		f.Burst = &burstRow{Text: text, Reload: reload}
	}
	return f
}

// burstText says how many entries a batch did not list one by one, of
// which actions, and when they happened; empty if it listed all.
func burstText(b *ActivityBatch) string {
	var parts []string
	if len(b.More) > 0 {
		type tally struct {
			action string
			n      int
		}
		var tallies []tally
		total := 0
		for action, n := range b.More {
			tallies = append(tallies, tally{action, n})
			total += n
		}
		slices.SortFunc(tallies, func(a, b tally) int { return cmp.Or(cmp.Compare(b.n, a.n), strings.Compare(a.action, b.action)) })
		var kinds []string
		for _, t := range tallies {
			many := t.action
			if names, ok := actionNames[t.action]; ok {
				many = names.many
			}
			kinds = append(kinds, count(t.n)+" "+many)
		}
		parts = append(parts, fmt.Sprintf("Busy: to keep this page responsive, %s from %s to %s %s summarized here "+
			"instead of listed one by one: %s.", plural(total, "more entry", "more entries"), clockText(b.From),
			clockText(b.To), isAre(total), strings.Join(kinds, ", ")))
	}
	if b.Lost > 0 {
		parts = append(parts, upperFirst(plural(b.Lost, "entry", "entries"))+" "+wasWere(b.Lost)+
			" no longer kept in memory when this page asked for "+itThem(b.Lost)+".")
	}
	return strings.Join(parts, " ")
}

// clockText returns the time of day of t, in UTC.
func clockText(t time.Time) string {
	return t.UTC().Format("15:04:05")
}

// recentActivity is the overview's section of the last entries.
type recentActivity struct {
	Rows []activityRow
	// Note says where they come from, or why there are none.
	Note string
	// Href links to the timeline.
	Href string
}

// recentActivity reads the last entries for the overview; nil without a
// source.
func (c *Console) recentActivity() *recentActivity {
	if c.node.Activity == nil {
		return nil
	}
	p, err := c.node.Activity.Timeline(ActivityFilter{}, "", recentEntries)
	ra := &recentActivity{Href: "/activity"}
	switch {
	case err != nil:
		ra.Note = "The activity could not be read: " + err.Error() + "."
	case len(p.Entries) == 0 && p.Memory:
		ra.Note = "Nothing has happened since obied started."
	case len(p.Entries) == 0:
		ra.Note = "The audit log holds no activity yet."
	case p.Path == "":
		ra.Note = "The audit log is off: only what happened since obied started is shown."
	}
	for i := range p.Entries {
		ra.Rows = append(ra.Rows, newActivityRow(&p.Entries[i]))
	}
	return ra
}

// isAre, wasWere and itThem agree with a count.
func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func wasWere(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

func itThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// upperFirst capitalizes the first letter of s.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
