package audit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Rule names of records that are not decided by the allow-list or an
// override.
const (
	RuleConsensus      = "consensus"
	RuleLocalAutoblock = "local_autoblock"
	RuleLocalReport    = "local_report"
	RuleRevocation     = "revocation"
)

var changeActions = map[decision.ChangeType]Action{
	decision.ChangeAdded:   ActionBlockAdded,
	decision.ChangeUpdated: ActionBlockUpdated,
	decision.ChangeRemoved: ActionBlockRemoved,
}

// BlockChange returns the record of a change of the decision engine's
// block stream, with the verdicts that count in the block: for a removal,
// those of the block that ended (ADR 0032).
func BlockChange(c decision.Change) Record {
	r := decisionRecord(changeActions[c.Type], &c.Decision, c.Cause)
	r.Contributors = make([]Contributor, len(c.Contributors))
	for i, k := range c.Contributors {
		r.Contributors[i] = Contributor{PeerID: k.PeerID, Weight: k.Weight, Confidence: k.Confidence, VerdictID: k.EventID}
	}
	return r
}

// Allowed returns the record of a transition to decision.StateAllowed.
func Allowed(t decision.Transition) Record {
	return decisionRecord(ActionAllowed, &t.Decision, t.Cause)
}

func decisionRecord(action Action, d *decision.Decision, cause string) Record {
	return Record{
		Action:    action,
		Indicator: d.Indicator,
		Rule:      decisionRule(d),
		Reason:    d.Reason,
		State:     string(d.State),
		Scores:    &Scores{Score: d.Score, Threshold: d.Threshold, Publishers: d.Contributors},
		Cause:     cause,
		ExpiresAt: d.ExpiresAt,
	}
}

// decisionRule names the rule that decided d.
func decisionRule(d *decision.Decision) string {
	switch {
	case d.Sovereignty.Rule != "":
		return string(d.Sovereignty.Rule)
	case d.Autoblock:
		return RuleLocalAutoblock
	default:
		return RuleConsensus
	}
}

// OverrideSet returns the record of an override the operator set.
func OverrideSet(o *store.Override) Record {
	return Record{
		Action:    ActionOverrideSet,
		Indicator: o.Indicator,
		Rule:      string(o.Action),
		Reason:    "operator " + string(o.Action) + " override set",
		ExpiresAt: o.ExpiresAt,
		Note:      o.Note,
	}
}

// OverrideRemoved returns the record of an override the operator deleted;
// action is the deleted override's action, empty if unknown.
func OverrideRemoved(ind obieproto.Indicator, action store.Action) Record {
	return Record{
		Action:    ActionOverrideRemoved,
		Indicator: ind,
		Rule:      string(action),
		Reason:    "operator override removed",
	}
}

// LocalReport returns the record of a verdict this node issued.
func LocalReport(ev *obieproto.Event) Record {
	r := Record{Action: ActionLocalReport, Indicator: ev.Indicator, Rule: RuleLocalReport, ExpiresAt: ev.ExpiresAt(),
		EventID: ev.ID}
	if v, e := ev.Verdict, ev.Evidence; v != nil && e != nil {
		r.Reason = fmt.Sprintf("%s verdict issued: %s %s, %d events, confidence %s", v.SuggestedAction, ev.Protocol,
			e.Reason, e.Events, strconv.FormatFloat(v.Confidence, 'g', -1, 64))
	}
	return r
}

// Revocation returns the record of a revocation this node issued.
func Revocation(ev *obieproto.Event) Record {
	return Record{Action: ActionRevocation, Indicator: ev.Indicator, Rule: RuleRevocation, Reason: ev.Reason,
		EventID: ev.ID, Revokes: ev.Revokes}
}

// Peer is the peer of a connection change.
type Peer struct {
	ID string
	// Name is its trust.publishers name; empty if it has none.
	Name string
	// Bootstrap is set for a peer listed in mesh.bootstrap, Publisher for
	// one listed in trust.publishers.
	Bootstrap, Publisher bool
}

// PeerConnection returns the record of peer p connecting, or losing its
// last connection.
func PeerConnection(p Peer, connected bool) Record {
	r := Record{Action: ActionPeerDisconnected, PeerID: p.ID, PeerName: p.Name}
	verb := "disconnected"
	if connected {
		r.Action, verb = ActionPeerConnected, "connected"
	}
	role := "peer"
	switch {
	case p.Bootstrap:
		role = "bootstrap peer"
	case p.Publisher:
		role = "publisher"
	}
	name := p.ID
	if p.Name != "" {
		name = p.Name + " (" + p.ID + ")"
	}
	r.Reason = role + " " + name + " " + verb
	return r
}

// ConfigReloaded returns the record of a reload of the configuration file
// path that changed and applied the keys settings and left the keys
// restartSettings for a restart.
func ConfigReloaded(path string, settings, restartSettings []string) Record {
	r := Record{Action: ActionConfigReloaded, Settings: settings, RestartSettings: restartSettings,
		Reason: "configuration reloaded"}
	if path != "" {
		r.Reason += " from " + path
	}
	switch n := len(settings); n {
	case 0:
		r.Reason += ": no setting changed"
	case 1:
		r.Reason += ": " + settings[0] + " changed"
	default:
		r.Reason += fmt.Sprintf(": %d settings changed (%s)", n, strings.Join(settings, ", "))
	}
	switch n := len(restartSettings); n {
	case 0:
	case 1:
		r.Reason += "; " + restartSettings[0] + " waits for a restart"
	default:
		r.Reason += fmt.Sprintf("; %d settings wait for a restart (%s)", n, strings.Join(restartSettings, ", "))
	}
	return r
}

// ModeChanged returns the record of node.mode switching from from to to.
func ModeChanged(from, to string) Record {
	r := Record{Action: ActionModeChanged, PreviousMode: from,
		Reason: "node.mode changed from " + from + " to " + to}
	switch to {
	case "enforce":
		r.Reason += ": blocks are applied to the firewall"
	case "observe":
		r.Reason += ": blocks are only logged, and withdrawn from the firewall"
	}
	return r
}
