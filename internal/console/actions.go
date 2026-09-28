package console

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// maxActionBody bounds the form of an action.
const maxActionBody = 16 << 10

// Outcomes of actions are kept for the page the browser returns to.
const (
	maxOutcomes     = 64
	outcomeLifetime = 15 * time.Minute
)

// defaultRevokeReason is the reason obiectl revoke gives by default.
const defaultRevokeReason = "false_positive"

// Steps of an action's page.
const (
	stepForm    = "form"
	stepConfirm = "confirm"
	stepOff     = "off"
)

// actionKind describes the page of an action.
type actionKind struct {
	// title names the action.
	title string
	// details is set if the operator enters details before the review;
	// the other actions are reviewed as soon as the address is known.
	details bool
	// lead explains the action on its form.
	lead string
}

// actionKinds are the console's actions (ADR 0026).
var actionKinds = map[string]actionKind{
	ActionAllow: {title: "Always allow", details: true,
		lead: "Never block an address or network on this node, whatever the mesh reports, like obiectl allow. It beats every other rule, including the allow-list and always-block overrides on overlapping ranges."},
	ActionBlock: {title: "Always block", details: true,
		lead: "Block an address or network on this node whatever its score, like obiectl block. It beats your allow-list entries, but never the built-in ranges, this node's own addresses or its bootstrap peers."},
	ActionUnoverride: {title: "Remove the override",
		lead: "Remove the always-allow or always-block override on an address or network, like obiectl unoverride. The verdicts and the allow-list decide again."},
	ActionReport: {title: "Report", details: true,
		lead: "Publish a signed verdict on an address this node saw attacking it, like obiectl report. It counts on this node and goes to the mesh, where every peer weighs it with the trust it places in this node."},
	ActionRevoke: {title: "Revoke your verdict",
		lead: "Withdraw this node's active verdict on an address or network, like obiectl revoke: a signed revocation goes to the mesh and every peer stops counting the verdict."},
}

// actionForm is an action's form as the operator entered it.
type actionForm struct {
	Address, TTL, Note       string
	Protocol, Reason, Events string
	Confidence, Verdict      string
}

// actionFields are the form's fields in the order of the page.
var actionFields = []string{"address", "ttl", "note", "protocol", "reason", "events", "confidence", "verdict"}

// readActionForm reads an action's form from v, with the defaults
// obiectl gives the details.
func readActionForm(v url.Values, kind string) actionForm {
	f := actionForm{Address: bounded(strings.TrimSpace(v.Get("address"))), TTL: bounded(strings.TrimSpace(v.Get("ttl"))),
		Note: strings.TrimSpace(v.Get("note")), Protocol: bounded(strings.TrimSpace(v.Get("protocol"))),
		Reason: bounded(strings.TrimSpace(v.Get("reason"))), Events: bounded(strings.TrimSpace(v.Get("events"))),
		Confidence: bounded(strings.TrimSpace(v.Get("confidence"))), Verdict: bounded(v.Get("verdict"))}
	switch kind {
	case ActionReport:
		if !v.Has("events") {
			f.Events = "1"
		}
		if f.Verdict == "" {
			f.Verdict = "ban"
		}
	case ActionRevoke:
		if !v.Has("reason") {
			f.Reason = defaultRevokeReason
		}
	}
	return f
}

// get returns the value of the field name.
func (f *actionForm) get(name string) string {
	switch name {
	case "address":
		return f.Address
	case "ttl":
		return f.TTL
	case "note":
		return f.Note
	case "protocol":
		return f.Protocol
	case "reason":
		return f.Reason
	case "events":
		return f.Events
	case "confidence":
		return f.Confidence
	case "verdict":
		return f.Verdict
	}
	return ""
}

// used reports whether the action kind has the field name.
func used(kind, name string) bool {
	switch name {
	case "address":
		return true
	case "ttl":
		return kind == ActionAllow || kind == ActionBlock || kind == ActionReport
	case "note":
		return kind == ActionAllow || kind == ActionBlock
	case "reason":
		return kind == ActionReport || kind == ActionRevoke
	case "protocol", "events", "confidence", "verdict":
		return kind == ActionReport
	}
	return false
}

// values returns the fields of the action kind that hold something.
func (f *actionForm) values(kind string) url.Values {
	v := url.Values{}
	for _, name := range actionFields {
		if value := f.get(name); used(kind, name) && value != "" {
			v.Set(name, value)
		}
	}
	return v
}

// request reads the action kind from the form. Its errors are the
// operator's: a duration, a number that is none. Everything else the node
// checks with the rules of the admin API.
func (f *actionForm) request(kind string) (ActionRequest, error) {
	req := ActionRequest{Kind: kind, Address: f.Address}
	switch kind {
	case ActionAllow, ActionBlock:
		req.Note = f.Note
		if f.TTL != "" {
			d, err := config.ParseDuration(f.TTL)
			if err != nil || d <= 0 {
				return req, fmt.Errorf("invalid end %q: want a positive duration such as 90m, 36h or 7d, or nothing for never", f.TTL)
			}
			req.TTL = d.Std().Round(time.Second)
		}
	case ActionReport:
		req.Report = ReportDetails{Protocol: f.Protocol, Reason: f.Reason, Action: f.Verdict}
		events, err := strconv.ParseInt(f.Events, 10, 64)
		if err != nil {
			return req, fmt.Errorf("events: %q is not a whole number", f.Events)
		}
		req.Report.Events = events
		if f.Confidence != "" {
			c, err := strconv.ParseFloat(f.Confidence, 64)
			if err != nil {
				return req, fmt.Errorf("confidence: %q is not a number from 0 to 1", f.Confidence)
			}
			req.Report.Confidence = &c
		}
		if f.TTL != "" {
			d, err := config.ParseDuration(f.TTL)
			if err != nil {
				return req, fmt.Errorf("lifetime: %w", err)
			}
			req.TTL = d.Std()
		}
	case ActionRevoke:
		req.Reason = f.Reason
	}
	return req, nil
}

// actionPage is the data of an action's page: its form, or what it would
// do and the button that carries it out.
type actionPage struct {
	// Kind is the action; Title names it and Heading heads the page.
	Kind, Title, Heading, Lead string
	// Step is stepForm, stepConfirm or stepOff.
	Step string
	// Path is where the form goes.
	Path string
	Form actionForm
	// Return is where the browser returns after the action, empty for the
	// address's decision; Cancel is where Cancel leads.
	Return, Cancel string
	// Err says why the action is not carried out; Off why there are no
	// actions.
	Err string
	// Changed is set when the state changed since the confirmation was
	// shown, so nothing was carried out.
	Changed bool
	// Address is the address or network the confirmation is about;
	// DecisionHref links to its decision.
	Address, DecisionHref string
	// Consequences say what the action will do; Details list its details.
	Consequences []consequence
	Details      []detail
	// Hidden are the fields the confirmation posts, with the state token.
	Hidden []hiddenField
	// Confirm labels the button; Change leads back to the form.
	Confirm, Change string
	// Held is set if the events would wait for a peer.
	Held bool
}

// consequence is one thing an action will do; Level is "warning" for one
// the operator should weigh.
type consequence struct{ Text, Level string }

type detail struct{ Label, Value string }

type hiddenField struct{ Name, Value string }

// Has reports whether the page's form has the field name.
func (p *actionPage) Has(name string) bool { return used(p.Kind, name) }

// newActionPage returns the page of the action kind for form, returning to
// ret.
func newActionPage(kind string, form actionForm, ret string) actionPage {
	k := actionKinds[kind]
	p := actionPage{Kind: kind, Title: k.title, Heading: k.title, Lead: k.lead, Step: stepForm, Path: "/actions/" + kind,
		Form: form}
	if ret != "" {
		p.Return = safeNext(ret)
	}
	p.Cancel = p.Return
	if rng, err := parseRange(form.Address); err == nil {
		p.Address, p.DecisionHref = rangeText(rng), decisionHref(rng)
		p.Heading += " " + p.Address
		if p.Cancel == "" {
			p.Cancel = p.DecisionHref
		}
	}
	if p.Cancel == "" {
		p.Cancel = "/decisions"
	}
	return p
}

// actionsOff says why the console carries out no action; empty if it
// does.
func (c *Console) actionsOff() string {
	switch {
	case c.node.Actions == nil:
		return "The node passes the console no actions. Use obiectl on the node, e.g. sudo obiectl allow <address>."
	case !c.actionsOn():
		return "Actions are switched off on this node (console.actions: false): the console is read-only. " +
			"Use obiectl on the node, e.g. sudo obiectl allow <address>, or set console.actions: true and reload."
	default:
		return ""
	}
}

// actionsOffNote is what a view says where it would offer the actions
// that are switched off.
const actionsOffNote = "The console is read-only here: act on the node with sudo obiectl allow, block, unoverride, report or revoke."

// actionLinks lead from a view to the action pages, which return to it.
// The zero value leads nowhere and says nothing.
type actionLinks struct {
	// back is the view to return to; on is set if there are actions, and
	// off says why not.
	back string
	on   bool
	off  string
}

// actionLinks returns the links from the view r shows to the actions.
func (c *Console) actionLinks(r *http.Request) actionLinks {
	if c.actionsOff() != "" {
		return actionLinks{off: actionsOffNote}
	}
	return actionLinks{back: returnHere(r), on: true}
}

// href links to the action kind on address, or on one to enter for "";
// empty if there are no actions.
func (l actionLinks) href(kind, address string) string {
	if !l.on {
		return ""
	}
	v := url.Values{"return": {l.back}}
	if address != "" {
		v.Set("address", address)
	}
	return "/actions/" + kind + "?" + v.Encode()
}

// offNote says why the view offers no actions; empty if it does.
func (l actionLinks) offNote() string { return l.off }

// returnHere returns the path and query of r, without the outcome of an
// earlier action, for an action to return to.
func returnHere(r *http.Request) string {
	q := r.URL.Query()
	q.Del("done")
	if len(q) == 0 {
		return r.URL.Path
	}
	return r.URL.Path + "?" + q.Encode()
}

// actionBar is the actions on one address or network on its decision.
type actionBar struct {
	// Links are the actions that apply; Off says why there are none.
	Links []link
	Off   string
}

// actionBarOf returns the actions on p: always allow and block, and
// report; remove if an override is set on p, revoke if own, this node
// holds an active verdict on it.
func (c *Console) actionBarOf(r *http.Request, p netip.Prefix, own bool) *actionBar {
	l := c.actionLinks(r)
	if !l.on {
		return &actionBar{Off: l.off}
	}
	addr := rangeText(p)
	bar := &actionBar{Links: []link{{Text: "Always allow…", Href: l.href(ActionAllow, addr)},
		{Text: "Always block…", Href: l.href(ActionBlock, addr)}}}
	if c.overrideOn(p) {
		bar.Links = append(bar.Links, link{Text: "Remove the override…", Href: l.href(ActionUnoverride, addr)})
	}
	bar.Links = append(bar.Links, link{Text: "Report…", Href: l.href(ActionReport, addr)})
	if own {
		bar.Links = append(bar.Links, link{Text: "Revoke my verdict…", Href: l.href(ActionRevoke, addr)})
	}
	return bar
}

// overrideOn reports whether an override is set on exactly p; true if the
// console cannot tell, since the action's page says so.
func (c *Console) overrideOn(p netip.Prefix) bool {
	if c.node.Rules == nil {
		return true
	}
	list, err := c.node.Rules.Overrides(false)
	if err != nil {
		return true
	}
	for _, o := range list {
		if o.Range == p {
			return true
		}
	}
	return false
}

// crossSiteNavigation reports whether r is a navigation from another site
// or another port of this host: action pages open only from the console's
// own pages, the address bar or a bookmark, so a link elsewhere cannot
// present a prefilled confirmation (ADR 0026).
func crossSiteNavigation(r *http.Request) bool {
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "cross-site" || site == "same-site"
}

// serveAction shows an action's form or, once its details are entered,
// what it would do and a button that carries it out. It changes nothing.
func (c *Console) serveAction(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	k, ok := actionKinds[kind]
	if !ok {
		c.notFound(w, r)
		return
	}
	if crossSiteNavigation(r) {
		refuse(w, http.StatusForbidden, "refused: an action page opens only from the console's own pages. "+
			"Open the console directly in the address bar and choose the action there.")
		return
	}
	q := r.URL.Query()
	form := readActionForm(q, kind)
	p := newActionPage(kind, form, q.Get("return"))
	if off := c.actionsOff(); off != "" {
		c.renderAction(w, r, http.StatusForbidden, p.off(off))
		return
	}
	if form.Address == "" || q.Get("edit") != "" || (k.details && q.Get("review") == "") {
		c.renderAction(w, r, http.StatusOK, &p)
		return
	}
	req, err := form.request(kind)
	if err != nil {
		p.Err = err.Error()
		c.renderAction(w, r, http.StatusBadRequest, &p)
		return
	}
	review, err := c.node.Actions.Review(req)
	if err != nil {
		c.renderActionError(w, r, &p, err)
		return
	}
	p.confirm(&review, req, c.now())
	c.renderAction(w, r, http.StatusOK, &p)
}

// carryOutAction carries out the action a confirmation posts, if the
// session is still valid and the address's state is the one the
// confirmation showed, and returns the browser to the page it came from.
func (c *Console) carryOutAction(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := actionKinds[kind]; !ok {
		c.notFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxActionBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	form := readActionForm(r.PostForm, kind)
	ret := r.PostForm.Get("return")
	if !c.signedIn(r) {
		// The session ended (expired, signed out, rotated, restarted):
		// nothing is carried out, and the confirmation opens again after
		// signing in, with the state of then.
		c.log.Info("console action not carried out: the session ended", append([]any{"action", kind}, userAttrs(r)...)...)
		v := form.values(kind)
		if ret != "" {
			v.Set("return", ret)
		}
		v.Set("review", "1")
		next := "/actions/" + kind + "?" + v.Encode()
		http.Redirect(w, r, "/login?reason=action&next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	p := newActionPage(kind, form, ret)
	if off := c.actionsOff(); off != "" {
		c.renderAction(w, r, http.StatusForbidden, p.off(off))
		return
	}
	req, err := form.request(kind)
	if err != nil {
		p.Err = err.Error()
		c.renderAction(w, r, http.StatusBadRequest, &p)
		return
	}
	src := c.node.Actions
	// One action at a time: the state checked is the state acted on, for
	// every tab of every browser.
	c.actionMu.Lock()
	defer c.actionMu.Unlock()
	review, err := src.Review(req)
	if err != nil {
		c.renderActionError(w, r, &p, err)
		return
	}
	if stateToken(&review) != r.PostForm.Get("state") {
		p.Changed = true
		p.confirm(&review, req, c.now())
		c.renderAction(w, r, http.StatusConflict, &p)
		return
	}
	out, err := src.Do(r.Context(), req, actorOf(r))
	if err != nil {
		c.renderActionError(w, r, &p, err)
		return
	}
	c.log.Info("console action carried out", append([]any{"action", kind, "indicator", rangeText(out.Range)},
		userAttrs(r)...)...)
	id := c.outcomes.put(outcomeNotice(kind, &out), c.now())
	back := p.Return
	if back == "" {
		back = decisionHref(out.Range)
	}
	http.Redirect(w, r, mergeQuery(back, url.Values{"done": {id}}), http.StatusSeeOther) // #nosec G710 -- safeNext allows only paths on the console.
}

// off turns p into the page that says why there are no actions.
func (p *actionPage) off(why string) *actionPage {
	p.Step, p.Err = stepOff, why
	return p
}

// renderAction renders the action page p.
func (c *Console) renderAction(w http.ResponseWriter, r *http.Request, code int, p *actionPage) {
	c.render(w, code, actionTemplate, c.page(r, p.Heading, "", p))
}

// renderActionError shows the form with why the node does not carry the
// action out: the operator's to fix for an *ActionError, the node's else.
func (c *Console) renderActionError(w http.ResponseWriter, r *http.Request, p *actionPage, err error) {
	p.Step = stepForm
	var ae *ActionError
	if errors.As(err, &ae) {
		p.Err = ae.Message
		c.renderAction(w, r, ae.Status, p)
		return
	}
	c.log.Error("console action failed", "action", p.Kind, "error", err)
	p.Err = "The node could not check or carry out the action; the obied log says why."
	c.renderAction(w, r, http.StatusInternalServerError, p)
}

// actorOf returns the local user of r's connection.
func actorOf(r *http.Request) Actor {
	u, ok := r.Context().Value(connKey{}).(*connUser)
	if !ok || u.err != nil {
		return Actor{}
	}
	return Actor{UID: u.cred.UID, Known: true}
}

// stateToken is a fingerprint of what an action acts on — the override on
// the address and this node's active verdict — so that a confirmation
// shown before another change is not carried out over it (ADR 0026).
func stateToken(r *ActionReview) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "obie-console-action\x00%s\x00", r.Range)
	if o := r.Override; o != nil {
		_, _ = fmt.Fprintf(h, "override\x00%s\x00%d\x00%d\x00%s\x00", o.Action, o.CreatedAt.UnixNano(), o.ExpiresAt.UnixNano(), o.Note)
	}
	if v := r.Verdict; v != nil {
		_, _ = fmt.Fprintf(h, "verdict\x00%s\x00", v.EventID)
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:18])
}

// confirm turns p into the confirmation of req, which review describes.
func (p *actionPage) confirm(review *ActionReview, req ActionRequest, now time.Time) {
	p.Step, p.Err = stepConfirm, ""
	p.Address, p.DecisionHref = rangeText(review.Range), decisionHref(review.Range)
	p.Heading = p.Title + " " + p.Address
	if p.Return == "" {
		p.Cancel = p.DecisionHref
	}
	w := consequenceWriter{review: review, req: req, now: now, addr: p.Address}
	switch req.Kind {
	case ActionAllow:
		p.Consequences, p.Details, p.Confirm = w.allow(), w.overrideDetails(), "Always allow "+p.Address
	case ActionBlock:
		p.Consequences, p.Details, p.Confirm = w.block(), w.overrideDetails(), "Always block "+p.Address
	case ActionUnoverride:
		p.Consequences, p.Details, p.Confirm = w.unoverride(), w.removedDetails(), "Remove the override on "+p.Address
	case ActionReport:
		p.Consequences, p.Details, p.Confirm = w.report(), w.reportDetails(), "Report "+p.Address
		p.Held = review.Peers == 0 && review.Planned != nil && !review.Planned.Coalesced
	case ActionRevoke:
		p.Consequences, p.Details, p.Confirm = w.revoke(), w.revokeDetails(), "Revoke my verdict on "+p.Address
		p.Held = review.Peers == 0
	}
	values := p.Form.values(p.Kind)
	for _, name := range actionFields {
		if v := values.Get(name); v != "" {
			p.Hidden = append(p.Hidden, hiddenField{Name: name, Value: v})
		}
	}
	if p.Return != "" {
		p.Hidden = append(p.Hidden, hiddenField{Name: "return", Value: p.Return})
		values.Set("return", p.Return)
	}
	p.Hidden = append(p.Hidden, hiddenField{Name: "state", Value: stateToken(review)})
	if p.Kind != ActionUnoverride {
		values.Set("edit", "1")
		p.Change = p.Path + "?" + values.Encode()
	}
}

// consequenceWriter says in plain words what an action will do.
type consequenceWriter struct {
	review *ActionReview
	req    ActionRequest
	now    time.Time
	addr   string
}

func (w *consequenceWriter) network() bool { return isNetwork(w.review.Range) }

// until says how long an override of the request lasts.
func (w *consequenceWriter) until() string {
	if w.req.TTL <= 0 {
		return "until you remove the override"
	}
	return "until " + stamp(w.now.Add(w.req.TTL)).Text + " (for " + spanText(w.req.TTL) + ")"
}

// blockedNote says what a new block does to the firewall in the mode.
func (w *consequenceWriter) blockedNote() string {
	if w.review.Mode == modeObserve {
		return "In observe mode the node only logs blocks: nothing reaches the firewall until node.mode is enforce."
	}
	return "The firewall applies it within seconds."
}

// unblockedNote says what a lifted block does to the firewall in the
// mode.
func (w *consequenceWriter) unblockedNote() string {
	if w.review.Mode == modeObserve {
		return "In observe mode the firewall holds no block anyway."
	}
	return "Its firewall entry is removed within seconds."
}

// replaced says which override the new one replaces, if any.
func (w *consequenceWriter) replaced() []consequence {
	o := w.review.Override
	if o == nil {
		return nil
	}
	return []consequence{{Text: "It replaces the " + overrideText(o) + "."}}
}

// nothingPublished says that an override stays on this node.
var nothingPublished = consequence{Text: "Nothing is published: the mesh's peers are not told and keep deciding for themselves."}

func (w *consequenceWriter) allow() []consequence {
	main := w.addr + " will never be blocked by this node, whatever the mesh reports, " + w.until() + "."
	if w.network() {
		main = "No address in " + w.addr + " will be blocked by this node, whatever the mesh reports, " + w.until() + "."
	}
	out := []consequence{{Text: main}}
	switch now := w.review.Now; now.State {
	case StateBlock:
		out = append(out, consequence{Text: "It is blocked now; it will be unblocked on this node only. " + w.unblockedNote()})
	case StateAllowed:
		out = append(out, consequence{Text: "It is allowed already (" + now.Reason + "); the override keeps it so if that changes."})
	default:
		out = append(out, consequence{Text: "It is not blocked now; from now on no verdict can block it here."})
	}
	return append(append(out, w.replaced()...), nothingPublished)
}

func (w *consequenceWriter) block() []consequence {
	main := w.addr + " will be blocked by this node whatever its score, " + w.until() + "."
	if w.network() {
		main = "Every address in " + w.addr + " will be blocked by this node whatever its score, " + w.until() + "."
	}
	out := []consequence{{Text: main}}
	switch now, after := w.review.Now, w.review.After; {
	case after.State != StateBlock:
		out = append(out, consequence{Level: "warning", Text: "The always-block will not take effect: " + after.Reason +
			". It is set all the same, as obiectl block does, and takes effect once that no longer applies."})
	case now.State == StateBlock:
		out = append(out, consequence{Text: "It is blocked already; the override keeps it blocked when its verdicts expire or are revoked."})
	default:
		out = append(out, consequence{Text: "It is not blocked now; it will be blocked on this node only. " + w.blockedNote()})
	}
	return append(append(out, w.replaced()...), nothingPublished)
}

func (w *consequenceWriter) unoverride() []consequence {
	out := []consequence{{Text: "The " + overrideText(w.review.Override) + " will be removed on this node only."}}
	now, after := w.review.Now, w.review.After
	switch {
	case now.State == StateBlock && after.State != StateBlock:
		out = append(out, consequence{Text: w.addr + " will be unblocked on this node only: " + after.Reason + ". " + w.unblockedNote()})
	case now.State != StateBlock && after.State == StateBlock:
		out = append(out, consequence{Level: "warning", Text: w.addr + " will be blocked on this node: " + after.Reason + ". " + w.blockedNote()})
	default:
		out = append(out, consequence{Text: "Its decision stays " + strings.ToLower(stateLabel(after.State)) + ": " + after.Reason + "."})
	}
	return append(out, nothingPublished)
}

// unreachableText says what happens to an event while no peer is reachable.
const unreachableText = "No peer is reachable now. This node stores it and counts it at once, and sends it as soon as a peer is reachable (unless obied restarts before)."

func (w *consequenceWriter) report() []consequence {
	pl, rep := w.review.Planned, w.req.Report
	if pl == nil {
		return nil
	}
	var out []consequence
	if pl.Coalesced {
		out = append(out, consequence{Text: fmt.Sprintf("You reported %s less than a minute ago, so nothing is published now: %s added to the next refresh of your verdict issued %s.",
			w.addr, plural(int(rep.Events), "event is", "events are"), stamp(w.review.Verdict.IssuedAt).Text)})
	} else {
		verdict := fmt.Sprintf("a signed %s verdict on %s (%s %s, %s, confidence %s, for %s)", pl.Action, w.addr,
			rep.Protocol, rep.Reason, plural(int(rep.Events), "event", "events"), score(pl.Confidence), spanText(pl.TTL))
		if w.review.Peers > 0 {
			out = append(out, consequence{Text: fmt.Sprintf("This will publish %s to the %s connected now, who pass it on to theirs.",
				verdict, plural(w.review.Peers, "peer", "peers"))})
		} else {
			out = append(out, consequence{Level: "warning", Text: "This will issue " + verdict + ". " + unreachableText})
		}
		if pl.Refreshes && w.review.Verdict != nil {
			out = append(out, consequence{Text: "It replaces your verdict issued " + stamp(w.review.Verdict.IssuedAt).Text + ", adding its events."})
		}
	}
	out = append(out, w.afterVerdicts("This node will block "+w.addr))
	return append(out, consequence{Text: "Every peer decides for itself, with the trust it places in this node. You can revoke the verdict later."})
}

func (w *consequenceWriter) revoke() []consequence {
	v := w.review.Verdict
	if v == nil {
		return nil
	}
	revocation := fmt.Sprintf("a signed revocation of your %s verdict on %s (%s %s, issued %s)", v.Action, w.addr, v.Protocol,
		v.Reason, stamp(v.IssuedAt).Text)
	var out []consequence
	if w.review.Peers > 0 {
		out = append(out, consequence{Text: fmt.Sprintf("This will publish %s to the %s connected now: they stop counting it.",
			revocation, plural(w.review.Peers, "peer", "peers"))})
	} else {
		out = append(out, consequence{Level: "warning", Text: "This will issue " + revocation + ". " + unreachableText})
	}
	out = append(out, w.afterVerdicts(w.addr+" will be blocked"))
	return append(out, consequence{Text: "A revoked verdict cannot be restored; report the address again to issue a new one."})
}

// afterVerdicts says what a change of this node's verdict does to the
// decision here; blocking says a new block.
func (w *consequenceWriter) afterVerdicts(blocking string) consequence {
	now, after := w.review.Now, w.review.After
	switch {
	case now.State != StateBlock && after.State == StateBlock:
		how := ""
		if after.Autoblock {
			how = " on its own report (decision.local_autoblock)"
		}
		return consequence{Text: blocking + how + ". " + w.blockedNote()}
	case now.State == StateBlock && after.State != StateBlock:
		return consequence{Text: w.addr + " will be unblocked on this node: " + after.Reason + ". " + w.unblockedNote()}
	case after.State == StateBlock:
		return consequence{Text: w.addr + " is blocked on this node and stays so: " + after.Reason + "."}
	default:
		return consequence{Text: "On this node " + w.addr + " is not blocked, and will not be: " + after.Reason + "."}
	}
}

func (w *consequenceWriter) overrideDetails() []detail {
	ends := "Never, until you remove it"
	if w.req.TTL > 0 {
		ends = stamp(w.now.Add(w.req.TTL)).Text + ", in " + spanText(w.req.TTL)
	}
	return []detail{{"Address", w.addr}, {"Rule", actionKinds[w.req.Kind].title}, {"Ends", ends}, {"Note", orNone(w.req.Note)}}
}

func (w *consequenceWriter) removedDetails() []detail {
	o := w.review.Override
	if o == nil {
		return []detail{{"Address", w.addr}}
	}
	ends := "Never"
	if !o.ExpiresAt.IsZero() {
		ends = stamp(o.ExpiresAt).Text
	}
	return []detail{{"Address", w.addr}, {"Override", overrideKind(o.Action)}, {"Set", stamp(o.CreatedAt).Text},
		{"Ends", ends}, {"Note", orNone(o.Note)}}
}

func (w *consequenceWriter) reportDetails() []detail {
	rep, pl := w.req.Report, w.review.Planned
	d := []detail{{"Address", w.addr}, {"Protocol", rep.Protocol}, {"Reason", rep.Reason}, {"Events", count(int(rep.Events))}}
	if pl != nil {
		d = append(d, detail{"Verdict", pl.Action}, detail{"Confidence", score(pl.Confidence)},
			detail{"Lifetime", spanText(pl.TTL) + ", until " + stamp(w.now.Add(pl.TTL)).Text})
	}
	return d
}

func (w *consequenceWriter) revokeDetails() []detail {
	d := []detail{{"Address", w.addr}, {"Reason", w.req.Reason}}
	if v := w.review.Verdict; v != nil {
		d = append(d, detail{"Verdict", v.EventID}, detail{"Expires", stamp(v.ExpiresAt).Text})
	}
	return d
}

// spanText says how long d is in words, e.g. "7 days" or "1 day 12
// hours", to the minute.
func spanText(d time.Duration) string {
	d = max(d, 0).Round(time.Minute)
	days, hours, minutes := int(d/(24*time.Hour)), int(d/time.Hour)%24, int(d/time.Minute)%60
	var parts []string
	if days > 0 {
		parts = append(parts, plural(days, "day", "days"))
	}
	if hours > 0 {
		parts = append(parts, plural(hours, "hour", "hours"))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, plural(minutes, "minute", "minutes"))
	}
	return strings.Join(parts, " ")
}

// overrideText describes an override: its kind, where, when and why.
func overrideText(o *Override) string {
	if o == nil {
		return "override"
	}
	s := strings.ToLower(overrideKind(o.Action)) + " override on " + rangeText(o.Range) + ", set " + stamp(o.CreatedAt).Text
	if o.Note != "" {
		s += " with the note “" + o.Note + "”"
	}
	return s
}

// overrideKind names an override's action.
func overrideKind(action string) string {
	if action == ruleForceBlock {
		return "Always-block"
	}
	return "Always-allow"
}

func orNone(s string) string {
	if s == "" {
		return "None"
	}
	return s
}

// actionNotice is an action's outcome on the page the browser returns to.
type actionNotice struct {
	// Level is "ok" or "warning".
	Level string
	Title string
	Lines []string
	// Links lead to the views the action affected.
	Links []noticeLink
}

type noticeLink struct{ Href, Text string }

// outcomeNotice says what the action kind did.
func outcomeNotice(kind string, out *ActionOutcome) actionNotice {
	addr := rangeText(out.Range)
	n := actionNotice{Level: "ok"}
	switch kind {
	case ActionAllow, ActionBlock:
		verb := "allowed"
		if kind == ActionBlock {
			verb = "blocked"
		}
		until := "until you remove the override"
		if o := out.Override; o != nil && !o.ExpiresAt.IsZero() {
			until = "until " + stamp(o.ExpiresAt).Text
		}
		n.Title = fmt.Sprintf("%s is always %s on this node, %s.", addr, verb, until)
		if out.Warning != "" {
			n.Level = "warning"
			n.Lines = append(n.Lines, "Warning: "+out.Warning+".")
		}
	case ActionUnoverride:
		n.Title = "The override on " + addr + " was removed."
	case ActionReport:
		switch {
		case out.Coalesced:
			n.Title = "Your report on " + addr + " was added to the next refresh of your verdict."
		case out.Held:
			n.Level = "warning"
			n.Title = "Your verdict on " + addr + " is stored and counts on this node."
			n.Lines = append(n.Lines, "No peer is reachable now: it will be sent as soon as one is (unless obied restarts before).")
		default:
			n.Title = fmt.Sprintf("Your verdict on %s was published to %s.", addr, plural(out.Peers, "peer", "peers"))
		}
	case ActionRevoke:
		if out.Held {
			n.Level = "warning"
			n.Title = "Your verdict on " + addr + " is revoked on this node."
			n.Lines = append(n.Lines, "No peer is reachable now: the revocation will be sent as soon as one is (unless obied restarts before).")
		} else {
			n.Title = fmt.Sprintf("Your verdict on %s was revoked; the revocation was published to %s.", addr,
				plural(out.Peers, "peer", "peers"))
		}
	}
	n.Lines = append(n.Lines, "Decision now: "+strings.ToLower(stateLabel(out.Decision.State))+" — "+out.Decision.Reason+".")
	n.Links = []noticeLink{{decisionHref(out.Range), "Its decision"},
		{activityQuery{address: addr}.href("/activity"), "In the activity timeline"}}
	return n
}

// outcomes keeps the outcomes of actions for the pages the browsers
// return to, under random IDs, so a URL carries no words (ADR 0026).
type outcomes struct {
	mu   sync.Mutex
	byID map[string]storedOutcome
	// order holds the IDs, oldest first.
	order []string
}

type storedOutcome struct {
	notice actionNotice
	at     time.Time
}

func newOutcomes() *outcomes { return &outcomes{byID: make(map[string]storedOutcome)} }

// put keeps n and returns its ID, forgetting the oldest outcome over
// maxOutcomes.
func (o *outcomes) put(n actionNotice, now time.Time) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never fails; it crashes the program instead.
	id := base64.RawURLEncoding.EncodeToString(b)
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.order) >= maxOutcomes {
		delete(o.byID, o.order[0])
		o.order = o.order[1:]
	}
	o.byID[id] = storedOutcome{notice: n, at: now}
	o.order = append(o.order, id)
	return id
}

// get returns the outcome with the ID id, unless it is older than
// outcomeLifetime.
func (o *outcomes) get(id string, now time.Time) (actionNotice, bool) {
	if id == "" {
		return actionNotice{}, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.byID[id]
	if !ok || now.Sub(s.at) > outcomeLifetime {
		return actionNotice{}, false
	}
	return s.notice, true
}
