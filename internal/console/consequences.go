package console

import (
	"fmt"
	"strings"
	"time"
)

// The words of an action's confirmation (ADR 0026): what it will do,
// computed on the node, and its details.

// consequence is one thing an action will do; Level is "warning" for one
// the operator should weigh.
type consequence struct{ Text, Level string }

type detail struct{ Label, Value string }

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
		issued := ""
		if v := w.review.Verdict; v != nil {
			issued = " issued " + stamp(v.IssuedAt).Text
		}
		out = append(out, consequence{Text: fmt.Sprintf("You reported %s less than a minute ago, so nothing is published now: %s added to the next refresh of your verdict%s.",
			w.addr, plural(int(rep.Events), "event is", "events are"), issued)})
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
