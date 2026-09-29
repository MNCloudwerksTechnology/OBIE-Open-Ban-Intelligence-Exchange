package console

import (
	"context"
	"net/netip"
	"time"
)

// Operator actions the console carries out after a confirmation, as
// obiectl allow, block, unoverride, report and revoke do (ADR 0026).
const (
	ActionAllow      = "allow"
	ActionBlock      = "block"
	ActionUnoverride = "unoverride"
	ActionReport     = "report"
	ActionRevoke     = "revoke"
)

// ActionRequest is an operator action as the operator entered it. The node
// checks it with the rules of the admin API, so the console never decides
// what is allowed.
type ActionRequest struct {
	// Kind is one of the Action constants.
	Kind string
	// Address is the address or network as entered; the node parses it
	// like obiectl.
	Address string
	// TTL is how long an override lasts (0: until removed) or a reported
	// verdict lives (0: decision.default_ttl).
	TTL time.Duration
	// Note is an override's note.
	Note string
	// Report holds a report's details.
	Report ReportDetails
	// Reason is why a verdict is revoked, e.g. false_positive.
	Reason string
}

// ReportDetails are what a report says about the address, as obiectl
// report's flags.
type ReportDetails struct {
	// Protocol is the attacked service, e.g. ssh; Reason the behavior,
	// e.g. password_bruteforce.
	Protocol, Reason string
	// Events is the number of malicious events observed.
	Events int64
	// Confidence is in [0, 1]; nil for the default.
	Confidence *float64
	// Action is ban or watch.
	Action string
}

// Actor is the local user who asks for an action: the owner of the
// console connection.
type Actor struct {
	UID uint32
	// Known is set if the node could tell the user.
	Known bool
}

// OwnVerdict is this node's active verdict on an address.
type OwnVerdict struct {
	EventID string
	// Action is ban or watch; Protocol and Reason say what it is about.
	Action, Protocol, Reason string
	Events                   int64
	Confidence               float64
	IssuedAt, ExpiresAt      time.Time
}

// ActionDecision is the decision on an address before or after an
// action.
type ActionDecision struct {
	// State is StateBlock, StateNone or StateAllowed; Reason sums it up
	// like obiectl explain.
	State, Reason string
	// Rule is the operator's rule that decides — force_allow, allowlist or
	// force_block — or empty; Protected is set for a protected allow-list
	// entry, which even a force-block does not beat.
	Rule      string
	Protected bool
	// Autoblock is set if this node's own ban verdict blocks it alone.
	Autoblock bool
	// ExpiresAt is when a block ends.
	ExpiresAt time.Time
}

// PlannedVerdict is the verdict a report would issue.
type PlannedVerdict struct {
	// Action is ban or watch; Confidence and TTL are resolved: the
	// defaults applied, the lifetime capped at decision.max_ttl.
	Action     string
	Confidence float64
	TTL        time.Duration
	// Refreshes is set if it would replace this node's active verdict;
	// Coalesced if the report would be added to that verdict's next
	// refresh instead, because it was issued less than a minute ago.
	Refreshes, Coalesced bool
}

// ActionReview is what an action would do, told before it is carried
// out; the node computes it with the same rules and data it acts on.
type ActionReview struct {
	// Range is the address or network the action is on.
	Range netip.Prefix
	// Override is the override on exactly Range now; nil if there is none.
	Override *Override
	// Verdict is this node's active verdict on Range now; nil if there is
	// none.
	Verdict *OwnVerdict
	// Now and After are the decision on Range now and after the action.
	Now, After ActionDecision
	// Planned is the verdict a report would issue; nil for other actions.
	Planned *PlannedVerdict
	// Peers counts the peers a report or revocation would be sent to now.
	Peers int
	// Backlog counts this node's events that wait to be sent before it,
	// and BacklogWait estimates how long sending them takes once a peer
	// is on the topic.
	Backlog     int
	BacklogWait time.Duration
	// Mode is node.mode.
	Mode string
}

// ActionOutcome is what an action did.
type ActionOutcome struct {
	Range netip.Prefix
	// Decision is the decision on Range after the action.
	Decision ActionDecision
	// Override is the override that was set; nil for other actions.
	Override *Override
	// Warning is what obiectl warns about, e.g. a force-block without
	// effect.
	Warning string
	// EventIDs are the events issued: the verdict or the revocation.
	// Coalesced is set if a report issued none, its events added to the
	// next refresh of the verdict in EventIDs.
	EventIDs  []string
	Coalesced bool
	// Held is set if the events issued wait to be sent: for a peer, with
	// Peers 0, or behind other events waiting for Peers peers. Peers
	// counts the peers on the topic; Backlog the events that wait, these
	// included, and BacklogWait estimates how long sending them takes.
	Held        bool
	Peers       int
	Backlog     int
	BacklogWait time.Duration
}

// ActionError is why the node does not carry out an action: the
// operator's input or one of its rules, in the words of the admin API.
type ActionError struct {
	// Status is the admin API's answer: 400 for invalid input, 404 when
	// there is nothing to act on, 422 when a rule refuses it.
	Status  int
	Message string
}

func (e *ActionError) Error() string { return e.Message }

// ActionSource checks and carries out operator actions for the console
// with the rules and services of the admin API (ADR 0026). Errors of type
// *ActionError are the operator's to fix; others are the node's.
type ActionSource interface {
	// Review checks req and tells what carrying it out would do, without
	// changing anything.
	Review(req ActionRequest) (ActionReview, error)
	// Do carries req out for actor, recording it in the audit trail as
	// coming from the console, and returns once the views read the new
	// state.
	Do(ctx context.Context, req ActionRequest, actor Actor) (ActionOutcome, error)
}
