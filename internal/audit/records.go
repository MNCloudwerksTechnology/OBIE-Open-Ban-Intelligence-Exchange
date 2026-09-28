package audit

import (
	"fmt"
	"strconv"

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
// block stream.
func BlockChange(c decision.Change) Record {
	return decisionRecord(changeActions[c.Type], &c.Decision, c.Cause)
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
