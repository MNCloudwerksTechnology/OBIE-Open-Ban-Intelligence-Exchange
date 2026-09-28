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

// maxRuleRows bounds the rows the overrides view and each group of the
// allow-list view list (ADR 0024).
const maxRuleRows = 1000

// Filters of the overrides view.
const (
	kindAllow = "allow"
	kindBlock = "block"
	// stateExpired lists the overrides that expired.
	stateExpired = "expired"
)

// overridesQuery is what the overrides view shows. It is the view's URL,
// so a link shares the view.
type overridesQuery struct {
	// kind is kindAllow, kindBlock or "" for both.
	kind string
	// address is the address or network as typed.
	address string
	// state is stateExpired or "" for the overrides in effect.
	state string
}

// parseOverridesQuery reads the view's query; unknown values fall back to
// the defaults.
func parseOverridesQuery(v url.Values) overridesQuery {
	q := overridesQuery{address: bounded(strings.TrimSpace(v.Get("address")))}
	if k := v.Get("kind"); k == kindAllow || k == kindBlock {
		q.kind = k
	}
	if v.Get("state") == stateExpired {
		q.state = stateExpired
	}
	return q
}

// href returns the view's path with q as its query, leaving out the
// defaults.
func (q overridesQuery) href() string {
	v := url.Values{}
	for _, kv := range [][2]string{{"kind", q.kind}, {"address", q.address}, {"state", q.state}} {
		if kv[1] != "" {
			v.Set(kv[0], kv[1])
		}
	}
	if len(v) == 0 {
		return "/overrides"
	}
	return "/overrides?" + v.Encode()
}

// overridesPage is the data of the overrides view. It is read when the
// page opens (ADR 0024).
type overridesPage struct {
	ReadAt timestamp
	// Notice explains that the store does not run; Err says why the
	// overrides could not be read.
	Notice, Err string
	// States are the tabs: in effect and expired.
	States []peerFilter
	// Kinds are the options of the kind filter; Address and State are
	// kept by the filter form, AddressErr says why the address is none.
	Kinds               []option
	Address, AddressErr string
	State               string
	// Clear links to the list without filters; empty without any.
	Clear string
	// Heading says what the list shows; StateNote explains the expired
	// ones.
	Heading, StateNote string
	Rows               []overrideRow
	// More counts the overrides that match but are not listed.
	More int
	// Empty explains why no override is listed; empty if some are.
	Empty string
	// Retention says how long the store keeps an expired override, e.g.
	// "7 days".
	Retention string
}

// overrideRow is an override as the list shows it.
type overrideRow struct {
	// Address is the address or network, DecisionHref links to its
	// decision.
	Address, DecisionHref string
	// Kind is kindAllow or kindBlock for the stylesheet; KindLabel names it.
	Kind, KindLabel string
	Note            string
	Set             timestamp
	// Ends is when it ends, or ended; Never is set if it does not end.
	Ends  timestamp
	Never bool
	// NoEffect says why a force-block has no effect; empty if it takes
	// effect.
	NoEffect string
}

// overridesInput is what the overrides view is built from.
type overridesInput struct {
	now   time.Time
	query overridesQuery
	// active and expired are the overrides in effect and those expired;
	// err says why the ones listed could not be read.
	active, expired []Override
	err             error
	// search is the address or network the list is narrowed to;
	// addressErr says why the address typed is none.
	search     netip.Prefix
	addressErr error
	retention  time.Duration
	notice     string
}

// kindOptions are the options of the kind filter.
var kindOptions = []struct{ value, label string }{{"", "Always allow and always block"},
	{kindAllow, "Always allow"}, {kindBlock, "Always block"}}

// buildOverrides builds the overrides view.
func buildOverrides(in overridesInput) overridesPage {
	q := in.query
	p := overridesPage{ReadAt: stamp(in.now), Notice: in.notice, Address: q.address, State: q.state,
		Retention: retentionText(in.retention)}
	if in.err != nil {
		p.Err = in.err.Error()
	}
	if in.addressErr != nil {
		p.AddressErr = in.addressErr.Error()
	}
	for _, o := range kindOptions {
		p.Kinds = append(p.Kinds, option{Value: o.value, Label: o.label, Selected: o.value == q.kind})
	}
	active, expired := filterOverrides(in.active, q.kind, in.search), filterOverrides(in.expired, q.kind, in.search)
	for _, s := range []struct {
		state, label string
		n            int
	}{{"", "In effect", len(active)}, {stateExpired, "Expired", len(expired)}} {
		f := q
		f.state = s.state
		p.States = append(p.States, peerFilter{Label: s.label, Count: s.n, Current: s.state == q.state, Href: f.href()})
	}
	if q.kind != "" || q.address != "" {
		p.Clear = overridesQuery{state: q.state}.href()
	}
	p.Heading = overridesHeading(q, in.search)
	list := active
	if q.state == stateExpired {
		list = expired
		p.StateNote = "Overrides that reached their expiry: they do nothing any more."
		if p.Retention != "" {
			p.StateNote += " The node keeps them for " + p.Retention + " after their expiry, then forgets them."
		}
	}
	sortOverrides(list, q.state == stateExpired)
	if len(list) > maxRuleRows {
		p.More, list = len(list)-maxRuleRows, list[:maxRuleRows]
	}
	for i := range list {
		p.Rows = append(p.Rows, newOverrideRow(&list[i], q.state == stateExpired))
	}
	if len(p.Rows) == 0 && in.err == nil {
		p.Empty = emptyOverrides(q, p.Retention)
	}
	return p
}

// filterOverrides returns the overrides of kind ("" for both) that overlap
// search (any if invalid).
func filterOverrides(list []Override, kind string, search netip.Prefix) []Override {
	var out []Override
	for _, o := range list {
		switch {
		case kind == kindAllow && o.Action != ruleForceAllow, kind == kindBlock && o.Action != ruleForceBlock:
		case search.IsValid() && !o.Range.Overlaps(search):
		default:
			out = append(out, o)
		}
	}
	return out
}

// sortOverrides orders the overrides in effect by when they were set,
// the expired ones by when they ended; the most recent first.
func sortOverrides(list []Override, expired bool) {
	slices.SortStableFunc(list, func(a, b Override) int {
		ta, tb := a.CreatedAt, b.CreatedAt
		if expired {
			ta, tb = a.ExpiresAt, b.ExpiresAt
		}
		return cmp.Or(tb.Compare(ta), a.Range.Addr().Compare(b.Range.Addr()), cmp.Compare(a.Range.Bits(), b.Range.Bits()))
	})
}

// newOverrideRow describes the override o for the list.
func newOverrideRow(o *Override, expired bool) overrideRow {
	r := overrideRow{Address: rangeText(o.Range), DecisionHref: decisionHref(o.Range), Kind: kindAllow, KindLabel: "Always allow",
		Note: o.Note, Set: stamp(o.CreatedAt), Ends: stamp(o.ExpiresAt), Never: o.ExpiresAt.IsZero()}
	if o.Action == ruleForceBlock {
		r.Kind, r.KindLabel = kindBlock, "Always block"
	}
	if o.Overruled != nil && !expired {
		r.NoEffect = noEffectText(o.Overruled)
	}
	return r
}

// noEffectText says which rule beats a force-block, so it has no effect.
func noEffectText(r *Ruling) string {
	switch {
	case r.Rule == ruleForceAllow:
		return "No effect: the always-allow override on " + matchText(r.Match) + " covers it and wins."
	case r.Rule == ruleAllowlist && r.Protected:
		return "No effect: it is protected by " + protectingText(r) + ", which not even an override blocks."
	default:
		return "No effect: " + r.Reason + "."
	}
}

// matchText returns what a ruling matched as the operator types it: an
// override's indicator key without its kind, an allow-list entry as is.
func matchText(match string) string {
	if p, err := parseRange(match); err == nil {
		return rangeText(p)
	}
	return match
}

// overridesHeading says what the list under q shows.
func overridesHeading(q overridesQuery, search netip.Prefix) string {
	heading := "Overrides in effect"
	switch {
	case q.state == stateExpired && q.kind == kindAllow:
		heading = "Expired always-allow overrides"
	case q.state == stateExpired && q.kind == kindBlock:
		heading = "Expired always-block overrides"
	case q.state == stateExpired:
		heading = "Expired overrides"
	case q.kind == kindAllow:
		heading = "Always-allow overrides in effect"
	case q.kind == kindBlock:
		heading = "Always-block overrides in effect"
	}
	if search.IsValid() {
		heading += " on or around " + rangeText(search)
	}
	return heading
}

// emptyOverrides explains why the list under q shows no override.
func emptyOverrides(q overridesQuery, retention string) string {
	switch {
	case q.kind != "" || q.address != "":
		return "No override matches this view."
	case q.state == stateExpired && retention != "":
		return "No override expired in the last " + retention + "."
	case q.state == stateExpired:
		return "No expired override is kept."
	default:
		return "You have set no override: the verdicts and the allow-list decide. Set one with " +
			"sudo obiectl allow or sudo obiectl block."
	}
}

// partNotice explains that the part of the node named part, which what
// names in a sentence, does not run, from its lifecycle status: starting
// says what happens once it runs, stopped what cannot happen until then.
// Empty while it runs.
func partNotice(statuses []lifecycle.Status, part, what, starting, stopped string) string {
	for _, s := range statuses {
		if s.Name != part {
			continue
		}
		switch s.State {
		case lifecycle.StateRunning:
			return ""
		case lifecycle.StatePending, lifecycle.StateStarting:
			return what + " is starting: " + starting + "."
		default:
			return what + " is not running: " + stopped + "."
		}
	}
	return ""
}

// overridesContent reads the overrides under the request's filters.
func (c *Console) overridesContent(r *http.Request) any {
	q := parseOverridesQuery(r.URL.Query())
	in := overridesInput{now: c.now(), query: q, notice: partNotice(c.node.Status(), partStore, "The store",
		"the overrides appear once it runs", "the overrides cannot be read until it runs again")}
	if q.address != "" {
		p, err := parseRange(q.address)
		if err != nil {
			in.addressErr = fmt.Errorf("%w; the list is not narrowed to it", err)
		} else {
			in.search = p
		}
	}
	src := c.node.Rules
	if src == nil {
		in.err = errNoRules
		return buildOverrides(in)
	}
	in.retention = src.OverrideRetention()
	var activeErr, expiredErr error
	in.active, activeErr = src.Overrides(false)
	in.expired, expiredErr = src.Overrides(true)
	in.err = activeErr
	if q.state == stateExpired {
		in.err = expiredErr
	}
	return buildOverrides(in)
}

// errNoRules says that the node passes the console no rules.
var errNoRules = errors.New("the node passes the console no overrides, allow-list or configuration")
