package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// reportChecker tells what a report would do; verdicts.Service implements
// it.
type reportChecker interface {
	Check(verdicts.Report) (verdicts.Plan, error)
}

// meshReach tells whether the node's events reach peers; mesh.Mesh
// implements it.
type meshReach interface {
	TopicPeers() int
	Held(id string) bool
}

// consoleActions carries out the console's operator actions with the
// admin API's request checks and the very services behind the admin API,
// so the console obeys the rules obiectl obeys and writes the same audit
// records, from the console (ADR 0026). The fields besides store, self
// and now are set before the node starts.
type consoleActions struct {
	store store.Store
	self  string
	now   func() time.Time
	// overrides and verdicts are the admin API's services, which record
	// the changes in the audit trail; checker checks reports.
	overrides admin.Overrides
	verdicts  admin.VerdictService
	checker   reportChecker
	engine    *decision.Engine
	mesh      meshReach
	mode      func() string
}

var _ console.ActionSource = (*consoleActions)(nil)

// action is a console request checked with the admin API's rules.
type action struct {
	req console.ActionRequest
	ind obieproto.Indicator
	// override is the override an allow or block sets.
	override store.Override
	// report and plan are a report's; revocation is a revocation's.
	report     verdicts.Report
	plan       verdicts.Plan
	revocation verdicts.Revocation
}

// check checks req like the admin API checks obiectl's request for it.
func (a *consoleActions) check(req console.ActionRequest, now time.Time) (*action, error) {
	act := &action{req: req}
	var err error
	switch req.Kind {
	case console.ActionAllow, console.ActionBlock:
		name := admin.ActionForceAllow
		if req.Kind == console.ActionBlock {
			name = admin.ActionForceBlock
		}
		oreq := admin.OverrideRequest{Indicator: req.Address, Action: name, TTLSeconds: int64(req.TTL / time.Second), Note: req.Note}
		if act.ind, err = oreq.Check(); err != nil {
			return nil, invalid(err)
		}
		act.override = store.Override{Indicator: act.ind, Action: store.Action(name), Note: req.Note, CreatedAt: now}
		if req.TTL > 0 {
			act.override.ExpiresAt = now.Add(req.TTL)
		}
		// The store's rules, as storeOverrides.Set words their breach.
		if err := store.CheckOverride(act.override, now); err != nil {
			return nil, invalid(fmt.Errorf("%w: %w", admin.ErrInvalid, err))
		}
	case console.ActionUnoverride:
		if act.ind, err = admin.ParseIndicator(req.Address); err != nil {
			return nil, invalid(err)
		}
	case console.ActionReport:
		rreq := admin.ReportRequest{Protocol: req.Report.Protocol, Reason: req.Report.Reason, Events: req.Report.Events,
			Confidence: req.Report.Confidence, TTL: admin.TTL(req.TTL), Action: req.Report.Action}
		// obiectl report takes a range for a CIDR and anything else for an
		// address.
		if strings.Contains(req.Address, "/") {
			rreq.CIDR = req.Address
		} else {
			rreq.IP = req.Address
		}
		if act.report, err = rreq.Check(); err != nil {
			return nil, invalid(err)
		}
		if act.plan, err = a.checker.Check(act.report); err != nil {
			return nil, actionError(err)
		}
		act.ind = act.plan.Verdict.Indicator
	case console.ActionRevoke:
		vreq := admin.RevocationRequest{Indicator: req.Address, Reason: req.Reason}
		if act.revocation, err = vreq.Check(); err != nil {
			return nil, invalid(err)
		}
		act.ind = *act.revocation.Indicator
	default:
		return nil, invalid(fmt.Errorf("unknown action %q", req.Kind))
	}
	return act, nil
}

// Review checks req and tells what it would do, from the same store and
// rules the action changes.
func (a *consoleActions) Review(req console.ActionRequest) (console.ActionReview, error) {
	now := a.now()
	act, err := a.check(req, now)
	if err != nil {
		return console.ActionReview{}, err
	}
	key := act.ind.Key()
	r := console.ActionReview{Peers: a.mesh.TopicPeers(), Mode: a.mode()}
	r.Range, _ = sovereignty.PrefixOf(act.ind)
	current, err := a.store.Override(key, now)
	switch {
	case err == nil:
		o := consoleOverride(&current)
		r.Override = &o
	case !errors.Is(err, store.ErrNotFound):
		return console.ActionReview{}, err
	}
	own, err := a.ownVerdict(key, now)
	if err != nil {
		return console.ActionReview{}, err
	}
	if own != nil {
		r.Verdict = ownVerdictOf(own)
	}
	switch req.Kind {
	case console.ActionUnoverride:
		if r.Override == nil {
			return console.ActionReview{}, &console.ActionError{Status: http.StatusNotFound,
				Message: fmt.Sprintf("%s: %s", admin.ErrNoOverride, key)}
		}
	case console.ActionRevoke:
		if own == nil {
			return console.ActionReview{}, actionError(fmt.Errorf("%w: this node has no active verdict on %s", verdicts.ErrNotFound, key))
		}
	case console.ActionReport:
		// The verdict the report refreshes is the one the plan read, so the
		// plan and the verdict shown agree.
		pl := act.plan
		r.Verdict = nil
		if pl.Current != nil {
			r.Verdict = ownVerdictOf(pl.Current)
		}
		r.Planned = &console.PlannedVerdict{Action: pl.Verdict.Verdict.SuggestedAction, Confidence: pl.Verdict.Verdict.Confidence,
			TTL: time.Duration(pl.Verdict.Verdict.TTLSeconds) * time.Second, Refreshes: pl.Current != nil && !pl.Coalesced,
			Coalesced: pl.Coalesced}
	}
	before, err := a.engine.Explain(act.ind)
	if err != nil {
		return console.ActionReview{}, err
	}
	after, err := a.engine.ExplainWith(act.ind, func(in *decision.Inputs) { a.edit(act, in) })
	if err != nil {
		return console.ActionReview{}, err
	}
	r.Now, r.After = actionDecision(&before), actionDecision(&after)
	return r, nil
}

// edit changes the decision's inputs as the action act would.
func (a *consoleActions) edit(act *action, in *decision.Inputs) {
	key := act.ind.Key()
	switch act.req.Kind {
	case console.ActionAllow, console.ActionBlock:
		in.Overrides = append(withoutOverride(in.Overrides, key), act.override)
	case console.ActionUnoverride:
		in.Overrides = withoutOverride(in.Overrides, key)
	case console.ActionReport:
		if !act.plan.Coalesced {
			in.Verdicts = append(withoutPublisher(in.Verdicts, act.plan.Verdict.Publisher.PeerID), act.plan.Verdict)
		}
	case console.ActionRevoke:
		in.Verdicts = withoutPublisher(in.Verdicts, a.self)
	}
}

func withoutOverride(list []store.Override, key string) []store.Override {
	return slices.DeleteFunc(slices.Clone(list), func(o store.Override) bool { return o.Indicator.Key() == key })
}

func withoutPublisher(list []*obieproto.Event, peerID string) []*obieproto.Event {
	return slices.DeleteFunc(slices.Clone(list), func(ev *obieproto.Event) bool { return ev.Publisher.PeerID == peerID })
}

// Do carries req out for actor through the admin API's services, which
// record it as coming from the console, and returns once the decision
// engine evaluated the change, so the views read the new state.
func (a *consoleActions) Do(ctx context.Context, req console.ActionRequest, actor console.Actor) (console.ActionOutcome, error) {
	now := a.now()
	act, err := a.check(req, now)
	if err != nil {
		return console.ActionOutcome{}, err
	}
	origin := audit.Origin{Via: audit.OriginConsole}
	if actor.Known {
		origin = audit.LocalUser(audit.OriginConsole, actor.UID)
	}
	ctx = audit.WithOrigin(ctx, origin)
	out := console.ActionOutcome{}
	out.Range, _ = sovereignty.PrefixOf(act.ind)
	switch req.Kind {
	case console.ActionAllow, console.ActionBlock:
		o, err := a.overrides.Set(ctx, act.ind, string(act.override.Action), req.TTL, req.Note)
		if err != nil {
			return console.ActionOutcome{}, actionError(err)
		}
		set := console.Override{Range: out.Range, Action: o.Action, Note: o.Note, CreatedAt: o.CreatedAt}
		if o.ExpiresAt != nil {
			set.ExpiresAt = *o.ExpiresAt
		}
		out.Override = &set
	case console.ActionUnoverride:
		deleted, err := a.overrides.Delete(ctx, act.ind)
		switch {
		case err != nil:
			return console.ActionOutcome{}, err
		case !deleted:
			return console.ActionOutcome{}, &console.ActionError{Status: http.StatusNotFound,
				Message: fmt.Sprintf("%s: %s", admin.ErrNoOverride, act.ind.Key())}
		}
	case console.ActionReport:
		res, err := a.verdicts.Report(ctx, act.report)
		if err != nil {
			return console.ActionOutcome{}, actionError(err)
		}
		out.EventIDs, out.Coalesced = []string{res.Event.ID}, res.Coalesced
		out.Held = !res.Coalesced && a.mesh.Held(res.Event.ID)
	case console.ActionRevoke:
		events, err := a.verdicts.Revoke(ctx, act.revocation)
		if err != nil {
			return console.ActionOutcome{}, actionError(err)
		}
		for _, ev := range events {
			out.EventIDs = append(out.EventIDs, ev.ID)
			out.Held = out.Held || a.mesh.Held(ev.ID)
		}
	}
	if !out.Held {
		out.Peers = a.mesh.TopicPeers()
	}
	a.engine.Flush()
	d, err := a.engine.Explain(act.ind)
	if err != nil {
		// The action took effect; only its decision cannot be told.
		out.Decision = console.ActionDecision{Reason: "the decision could not be evaluated: " + err.Error()}
		return out, nil
	}
	out.Decision = actionDecision(&d)
	if req.Kind == console.ActionBlock {
		out.Warning = admin.OverrideWarning(admin.ActionForceBlock, &admin.DecisionResponse{State: string(d.State), Reason: d.Reason})
	}
	return out, nil
}

// ownVerdict returns this node's active verdict on the indicator key, or
// nil.
func (a *consoleActions) ownVerdict(key string, now time.Time) (*obieproto.Event, error) {
	active, err := a.store.ActiveVerdicts(key, now)
	if err != nil {
		return nil, err
	}
	for _, v := range active {
		if v.Publisher.PeerID == a.self {
			return v, nil
		}
	}
	return nil, nil
}

// ownVerdictOf converts this node's verdict for the console.
func ownVerdictOf(v *obieproto.Event) *console.OwnVerdict {
	o := &console.OwnVerdict{EventID: v.ID, Protocol: v.Protocol, IssuedAt: v.IssuedAt.UTC(), ExpiresAt: v.ExpiresAt().UTC()}
	if v.Verdict != nil {
		o.Action, o.Confidence = v.Verdict.SuggestedAction, v.Verdict.Confidence
	}
	if v.Evidence != nil {
		o.Reason, o.Events = v.Evidence.Reason, v.Evidence.Events
	}
	return o
}

// actionDecision converts a decision for the console.
func actionDecision(d *decision.Decision) console.ActionDecision {
	return console.ActionDecision{State: string(d.State), Reason: d.Reason, Rule: string(d.Sovereignty.Rule),
		Protected: d.Sovereignty.Rule == sovereignty.RuleAllowlist && d.Sovereignty.Source.Protected(),
		Autoblock: d.Autoblock, ExpiresAt: d.ExpiresAt}
}

// invalid is input the admin API answers with 400.
func invalid(err error) error {
	return &console.ActionError{Status: http.StatusBadRequest, Message: err.Error()}
}

// actionError turns an error of the admin API's services into what the
// console shows, with the status the admin API answers; others are the
// node's.
func actionError(err error) error {
	switch {
	case errors.Is(err, verdicts.ErrInvalid), errors.Is(err, admin.ErrInvalid):
		return &console.ActionError{Status: http.StatusBadRequest, Message: err.Error()}
	case errors.Is(err, verdicts.ErrRefused):
		return &console.ActionError{Status: http.StatusUnprocessableEntity, Message: err.Error()}
	case errors.Is(err, verdicts.ErrNotFound):
		return &console.ActionError{Status: http.StatusNotFound, Message: err.Error()}
	default:
		return err
	}
}
