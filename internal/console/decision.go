package console

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"time"
)

// ErrNoIndicator is returned by DecisionSource.Explain for a range the
// node cannot decide on, e.g. a network broader than /16.
var ErrNoIndicator = errors.New("not an address or network the node decides on")

// missingDecision explains that an explanation's address is none.
const missingDecision = "It is not an address or network the node can decide on. Open an IPv4 or IPv6 address " +
	"or a network, such as /decisions/203.0.113.7 or /decisions/198.51.100.0/24; networks are /16 to /31 for IPv4 " +
	"and /32 to /127 for IPv6."

// decisionPage is the data of an explanation (ADR 0022). All of it is the
// refreshing region: the decision is evaluated again with every refresh.
type decisionPage struct {
	// Fragment is the path of the refreshing region.
	Fragment string
	// Address is the address or network; Network is set for a network,
	// and Inside then links to the list of the decisions inside it.
	Address string
	Network bool
	Inside  string
	// Notice explains that the decision engine does not run.
	Notice string
	// Err says why the decision could not be evaluated.
	Err       string
	Evaluated timestamp
	// Summary answers "is it blocked?" in one line; Reason is the
	// decision engine's summary.
	Summary summary
	Reason  string
	// Kept says whether and how the node holds a decision on it.
	Kept string
	// Verdicts are the active verdicts, one per publisher; Sum shows how
	// they add up. AllVerdicts links to every verdict on it in the verdicts
	// view, also the revoked and expired ones (ADR 0023); empty without
	// that view.
	Verdicts    []verdictView
	Sum         decisionSum
	AllVerdicts string
	// Ruling says what the allow-list and the overrides do to it, and
	// Protection whether it is protected.
	Ruling     rulingView
	Protection string
	// Firewall says whether the firewall applies it and, if not, why.
	Firewall firewallView
	// Around are the decisions on the networks around it.
	Around []aroundRow
	// Actions are the operator's actions on it; nil in the refreshing
	// region (ADR 0026).
	Actions *actionBar
}

// verdictView is a publisher's verdict in an explanation.
type verdictView struct {
	// Publisher names it; Href links to its peer page, empty for this
	// node's own verdicts.
	Publisher, PeerID, ShortID, Href string
	Local                            bool
	// Weight is its trust weight and WeightFrom where that comes from.
	Weight, WeightFrom string
	Action, Confidence string
	// Score is what it adds to the decision's score.
	Score string
	// Counts says whether it counts in the decision, and if not, why.
	Counts   string
	Counting bool
	Reason   string
	Issued   timestamp
	Expires  timestamp
}

// decisionSum is how the verdicts add up.
type decisionSum struct {
	Score, Threshold string
	ScoreMet         bool
	Contributors     string
	Quorum           string
	QuorumMet        bool
	// Autoblock says whether this node's own verdict decides alone.
	Autoblock string
}

// rulingView is what the allow-list and the overrides do.
type rulingView struct {
	// Rule names the rule that decides, empty if none does; Text explains
	// it.
	Rule, Text string
	// Match is the allow-listed range or the override's indicator; Label
	// and Note describe the entry or the override.
	Match, Label, Note string
	Expires            timestamp
}

// firewallView is where the firewall stands on an explained range.
type firewallView struct {
	// State is for the stylesheet; Label and Text say whether and how the
	// firewall applies it, or why not.
	State, Label, Text string
	// Entry is the range of the entry that applies it, and Expires when
	// that entry ends; Href links to the explanation of a wider one.
	Entry, Href string
	Expires     timestamp
	// Pass says when the last enforcement pass ran, and in which mode.
	Pass string
}

// aroundRow is a decision on a network around the explained range.
type aroundRow struct {
	Href, Address                              string
	State, StateLabel, StateNote               string
	FirewallState, FirewallLabel, FirewallNote string
	// Wins is set for the network whose firewall entry applies the
	// explained range.
	Wins bool
}

// decisionInput is what an explanation is built from.
type decisionInput struct {
	now      time.Time
	ex       Explanation
	firewall Firewall
	// mode is the node mode now.
	mode   string
	notice string
	self   string
	// names are the names of the configured peers, by peer ID.
	names map[string]string
	// allVerdicts links to every verdict on the range; empty without the
	// verdicts view.
	allVerdicts string
}

// buildDecision builds the explanation of ex.
func buildDecision(in decisionInput) decisionPage {
	ex := &in.ex
	addr := rangeText(ex.Range)
	p := decisionPage{
		Fragment:  "/api" + decisionHref(ex.Range),
		Address:   addr,
		Network:   isNetwork(ex.Range),
		Notice:    in.notice,
		Evaluated: stamp(ex.EvaluatedAt),
		Summary:   decisionSummary(ex),
		Reason:    ex.Reason,
		Kept:      keptNote(ex),
		Sum: decisionSum{
			Score: score(ex.Score), Threshold: score(ex.Threshold), ScoreMet: ex.Score >= ex.Threshold-scoreTolerance,
			Contributors: count(ex.Contributors), Quorum: count(ex.Quorum), QuorumMet: ex.Contributors >= ex.Quorum,
			Autoblock: autoblockText(ex),
		},
		Ruling:      newRulingView(&ex.Ruling),
		Protection:  protection(&ex.Ruling),
		Firewall:    newFirewallView(ex, &in.firewall, in.mode, in.now),
		AllVerdicts: in.allVerdicts,
	}
	if p.Network {
		p.Inside = decisionsQuery{search: addr, sort: sortAddress}.href("/decisions")
	}
	for _, c := range ex.Verdicts {
		p.Verdicts = append(p.Verdicts, newVerdictView(&c, in.self, in.names))
	}
	for i := range ex.Around {
		it := &ex.Around[i]
		row := aroundRow{Href: decisionHref(it.Range), Address: rangeText(it.Range), State: it.State,
			StateLabel: stateLabel(it.State), StateNote: stateNote(it),
			Wins: ex.Firewall.Applied && ex.Firewall.Entry == it.Range}
		row.FirewallState, row.FirewallLabel, row.FirewallNote = firewallCell(it, &in.firewall, in.now)
		p.Around = append(p.Around, row)
	}
	return p
}

// decisionSummary answers "is it blocked?" for the summary card.
func decisionSummary(ex *Explanation) summary {
	s := summary{Title: headline(ex)}
	switch ex.State {
	case StateBlock:
		s.State = stateAttention
	case StateAllowed:
		s.State = stateOK
	default:
		s.State = stateWaiting
	}
	switch {
	case ex.State == StateAllowed:
		s.Text = "The operator's rules decide; the verdicts do not count."
	case ex.State == StateBlock && ex.Ruling.Rule == ruleForceBlock:
		s.Text = "The operator's rule decides; the verdicts do not count."
	case len(ex.Verdicts) == 0:
		s.Text = "The node holds no active verdict on it."
	default:
		s.Text = fmt.Sprintf("Score %s of threshold %s, %s of quorum %s.", score(ex.Score), score(ex.Threshold),
			plural(ex.Contributors, "publisher", "publishers"), count(ex.Quorum))
	}
	return s
}

// keptNote says whether the node holds a decision on the range, as the
// decisions list shows it.
func keptNote(ex *Explanation) string {
	switch {
	case !ex.Kept:
		return "The node holds no decision on it: no active verdict and no force-block. It is evaluated here as it would be."
	case ex.KeptState != ex.State:
		return fmt.Sprintf("The node still holds it as %s; the decision engine re-evaluates it within seconds.",
			stateLabel(ex.KeptState))
	default:
		return ""
	}
}

// autoblockText says whether this node's own ban verdict decides alone.
func autoblockText(ex *Explanation) string {
	switch {
	case ex.Autoblock:
		return "Yes: this node's own ban verdict blocks it alone (decision.local_autoblock)."
	case ex.LocalAutoblock:
		return "No: on (decision.local_autoblock), but this node holds no ban verdict of its own on it that decides."
	default:
		return "No: off (decision.local_autoblock); this node's own verdicts count like a peer's."
	}
}

// newVerdictView describes publisher c's verdict.
func newVerdictView(c *Contribution, self string, names map[string]string) verdictView {
	v := verdictView{
		PeerID: c.PeerID, ShortID: shortPeerID(c.PeerID), Local: c.Local || c.PeerID == self, Action: c.Action,
		Weight: score(c.Weight), Confidence: score(c.Confidence), Score: score(c.Score),
		Counting: c.Contributes, Reason: reasonText(c.Reason, c.Protocol),
		Issued: stamp(c.IssuedAt), Expires: stamp(c.ExpiresAt),
	}
	name := c.Name
	if name == "" {
		name = names[c.PeerID]
	}
	switch {
	case v.Local:
		v.Publisher, v.WeightFrom = "This node", "trust.local_weight"
	case name != "":
		v.Publisher, v.Href = name, "/peers/"+url.PathEscape(c.PeerID)
	default:
		v.Publisher, v.Href = shortPeerID(c.PeerID), "/peers/"+url.PathEscape(c.PeerID)
	}
	switch {
	case v.Local:
	case c.Listed:
		v.WeightFrom = "trust.publishers"
	default:
		v.WeightFrom = "trust.default_weight"
	}
	switch {
	case c.Contributes:
		v.Counts = "Yes"
	case c.Action != "ban":
		v.Counts = "No: a " + c.Action + " verdict"
	default:
		v.Counts = "No: weight 0"
	}
	return v
}

// newRulingView describes what the allow-list and the overrides do.
func newRulingView(r *Ruling) rulingView {
	v := rulingView{Match: r.Match, Label: r.Label, Note: r.Note, Expires: stamp(r.ExpiresAt)}
	switch {
	case r.Rule == ruleAllowlist && r.Protected:
		v.Rule, v.Text = "Protected address", "A protected allow-list entry ("+sourceText(r.Source)+
			") covers it. It is never blocked; not even the operator's force-block overrules it."
	case r.Rule == ruleAllowlist:
		v.Rule, v.Text = "Allow-list", "The operator's allow-list entry ("+sourceText(r.Source)+
			") covers it. It is not blocked, whatever the verdicts; only a force-block on it overrules the entry."
	case r.Rule == ruleForceAllow:
		v.Rule, v.Text = "Force-allow override", "The operator's force-allow override covers it. It is not blocked, "+
			"whatever the verdicts, until the override ends."
	case r.Rule == ruleForceBlock:
		v.Rule, v.Text = "Force-block override", "The operator's force-block override blocks it, whatever the "+
			"verdicts, until the override ends; it is capped at decision.max_ttl like every block."
	default:
		v.Text = "No allow-list entry or override covers it: the verdicts decide."
	}
	return v
}

// sourceText names where an allow-list entry comes from.
func sourceText(source string) string {
	switch source {
	case "builtin":
		return "built in: special-purpose addresses"
	case "self":
		return "this node's own address"
	case "bootstrap":
		return "a bootstrap peer's address"
	case "config":
		return "allowlist.cidrs"
	case "file":
		return "allowlist.files"
	default:
		return source
	}
}

// newFirewallView says where the firewall stands on the explained range,
// in the node mode mode.
func newFirewallView(ex *Explanation, fw *Firewall, mode string, now time.Time) firewallView {
	cov, block := &ex.Firewall, ex.State == StateBlock
	var v firewallView
	if pass := fw.Pass; pass != nil {
		v.Pass = fmt.Sprintf("Last enforcement pass: %s, in %s mode.", stamp(pass.At).Text, pass.Mode)
		if fw.Facts.Failures > 0 {
			v.Pass += fmt.Sprintf(" The passes since failed: %s.", fw.Facts.Err)
		}
	}
	if cov.Applied {
		v.Entry, v.Expires = rangeText(cov.Entry), stamp(cov.EntryExpires)
		if cov.Entry != ex.Range {
			v.Href = decisionHref(cov.Entry)
		}
	}
	// The firewall applies the kept decision, as the engine last decided
	// it; one it does not hold yet was decided after the last pass.
	decided := ex.EvaluatedAt
	if ex.Kept {
		decided = ex.KeptAt
	}
	item := DecisionItem{Range: ex.Range, State: ex.State, DecidedAt: decided, ExpiresAt: ex.ExpiresAt, Firewall: *cov}
	v.State, v.Label, _ = firewallCell(&item, fw, now)
	switch {
	case fw.Pass == nil:
		v.Text = "The firewall has not run an enforcement pass yet."
	case fw.Pass.Mode == modeObserve || mode == modeObserve:
		v.State, v.Label = stateIdle, "Not applied"
		v.Text = "The node is in observe mode: the firewall applies nothing, by design. The decision is made and logged; " +
			"set node.mode to enforce to apply blocks."
	case cov.Applied && cov.Entry == ex.Range:
		v.Text = "The firewall's own entry for it drops its traffic."
	case cov.Applied && block:
		v.Text = "The firewall's entry for the wider network " + v.Entry + " drops its traffic; it needs no entry of its own."
	case cov.Applied:
		v.Text = "It is not a block, yet the firewall drops its traffic: the entry for the wider network " + v.Entry +
			", a block, contains it and wins."
	case !block:
		v.Text = "It is not a block, and no firewall entry covers it."
	case cov.Skipped == SkipAllowlist:
		v.Text = "The allow-list refused the block right before it was applied, e.g. because the allow-list changed " +
			"and the decision engine has not yet re-evaluated it."
	case cov.Skipped == SkipMaxEntries:
		v.Text = fmt.Sprintf("enforce.max_entries (%s) is reached: the blocks with a higher score, and the operator's "+
			"force-blocks, take the entries.", count(fw.Facts.MaxEntries))
		if cov.Within != ex.Range {
			v.Text += " It is left out with the wider network " + rangeText(cov.Within) + "."
		}
	case cov.Deferred:
		v.Text = "It waits until an entry it overlaps expires: the firewall's sets cannot hold overlapping ranges."
	default:
		v.Text = "Not applied yet: " + notYetApplied(&item, fw.Pass, now) + "."
	}
	return v
}

// decisionContent explains the address or network r names, and whether it
// is one. With region set, the page is the refreshing region.
func (c *Console) decisionContent(r *http.Request, region bool) (string, any, bool) {
	p, err := parseRange(r.PathValue("id"))
	if err != nil {
		return "", decisionPage{Err: err.Error()}, false
	}
	title := "Decision on " + rangeText(p)
	src := c.node.Decisions
	if src == nil {
		return title, decisionPage{Fragment: "/api" + decisionHref(p), Address: rangeText(p),
			Err: "The node passes the console no decisions."}, true
	}
	ex, err := src.Explain(p)
	switch {
	case errors.Is(err, ErrNoIndicator):
		return "", decisionPage{Err: err.Error()}, false
	case err != nil:
		return title, decisionPage{Fragment: "/api" + decisionHref(p), Address: rangeText(p), Network: isNetwork(p),
			Err: "The decision could not be evaluated: " + err.Error()}, true
	}
	set := c.peerSet()
	names := make(map[string]string, len(set.Peers))
	for _, peer := range set.Peers {
		names[peer.ID] = peer.Name
	}
	all, _ := c.detailLink(verdictsHref(verdictsQuery{address: rangeText(p)}), "")
	page := buildDecision(decisionInput{now: c.now(), ex: ex, firewall: src.Firewall(), mode: c.node.Mode(),
		notice: decisionNotice(c.node.Status()), self: c.node.PeerID, names: names, allVerdicts: all})
	if !region {
		own := slices.ContainsFunc(ex.Verdicts, func(v Contribution) bool { return v.Local })
		page.Actions = c.actionBarOf(r, p, own)
	}
	return title, page, true
}

// isNetwork reports whether p is a network rather than one address.
func isNetwork(p netip.Prefix) bool { return p.Bits() < p.Addr().BitLen() }
