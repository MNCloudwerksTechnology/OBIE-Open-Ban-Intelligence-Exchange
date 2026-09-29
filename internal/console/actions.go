package console

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// maxActionBody bounds the form of an action.
const maxActionBody = 16 << 10

// Outcomes of actions are kept for the page the browser returns to, and
// actions whose session ended for their confirmation after signing in.
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

// refuseCrossSite refuses action pages opened from another site or another
// port of this host. It runs before the session check: such a navigation
// carries no SameSite=Strict cookie, and would otherwise be sent to sign
// in and on to the page.
func refuseCrossSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if crossSiteNavigation(r) {
			refuse(w, http.StatusForbidden, "refused: an action page opens only from the console's own pages. "+
				"Open the console directly in the address bar and choose the action there.")
			return
		}
		next.ServeHTTP(w, r)
	})
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
	q := r.URL.Query()
	lost := false
	if id := q.Get("pending"); id != "" {
		// The action whose session ended, kept for its confirmation.
		v, ok := c.pending.get(id, c.now())
		if ok {
			q = maps.Clone(v) // the kept values are shared with every request for them
			q.Set("review", "1")
		}
		lost = !ok
	}
	form := readActionForm(q, kind)
	p := newActionPage(kind, form, q.Get("return"))
	if off := c.actionsOff(); off != "" {
		c.renderAction(w, r, http.StatusForbidden, p.off(off))
		return
	}
	if lost {
		p.Err = "The action that waited for you to sign in is no longer kept: it is kept for 15 minutes, and not across a restart of obied. Enter it again."
		c.renderAction(w, r, http.StatusOK, &p)
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
		// The action waits on the node, not in the URL, so neither a long
		// note nor a link carries it through sign-in.
		c.log.Info("console action not carried out: the session ended", append([]any{"action", kind}, userAttrs(r)...)...)
		v := form.values(kind)
		if ret != "" {
			v.Set("return", ret)
		}
		next := "/actions/" + kind + "?pending=" + c.pending.put(v, c.now())
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
	layout := c.page(r, p.Heading, "", p)
	layout.NoShare = true
	c.render(w, code, actionTemplate, layout)
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
// the address, this node's active verdict, and whether a report would
// refresh that verdict or only be added to its next refresh — so that a
// confirmation shown before another change is not carried out over it
// (ADR 0026).
func stateToken(r *ActionReview) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "obie-console-action\x00%s\x00", r.Range)
	if o := r.Override; o != nil {
		_, _ = fmt.Fprintf(h, "override\x00%s\x00%d\x00%d\x00%s\x00", o.Action, o.CreatedAt.UnixNano(), o.ExpiresAt.UnixNano(), o.Note)
	}
	if v := r.Verdict; v != nil {
		_, _ = fmt.Fprintf(h, "verdict\x00%s\x00", v.EventID)
	}
	if pl := r.Planned; pl != nil {
		_, _ = fmt.Fprintf(h, "planned\x00%t\x00%t\x00", pl.Refreshes, pl.Coalesced)
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
