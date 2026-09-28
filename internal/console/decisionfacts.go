package console

import (
	"context"
	"net/netip"
	"time"
)

// Firewall filters of the decisions list (ADR 0022).
const (
	FirewallApplied    = "applied"
	FirewallNotApplied = "not_applied"
)

// Skip reasons of a decided block the firewall does not apply, as the
// reconciler names them.
const (
	SkipAllowlist  = "allowlist"
	SkipMaxEntries = "max_entries"
)

// Decision states, as the decision engine names them.
const (
	StateBlock   = "block"
	StateNone    = "none"
	StateAllowed = "allowed"
)

// DecisionQuery selects and orders a page of the decisions the node holds
// (ADR 0022). The zero value is the first page of every decision, the
// last decided first.
type DecisionQuery struct {
	// State keeps the decisions in that state; Category those holding an
	// active verdict of that category ("reason/protocol"); Publisher those
	// holding one of the publisher with that peer ID; Firewall those the
	// firewall applies (FirewallApplied) or does not (FirewallNotApplied).
	// Empty keeps all.
	State, Category, Publisher, Firewall string
	// Search, if valid, keeps the decisions whose range overlaps it: for an
	// address, its own and those of the networks around it.
	Search netip.Prefix
	// Sort is the order: decided, address, state, score, publishers or
	// expires.
	Sort string
	// After and Before are cursors of the order: the page after or before
	// that decision. Last selects the last page. None selects the first.
	After, Before string
	Last          bool
	Limit         int
}

// DecisionPage is a page of the decisions the node holds.
type DecisionPage struct {
	Items []DecisionItem
	// Total counts the decisions the query selects; Offset is the position
	// of the first item among them.
	Total, Offset int
	// States counts the decisions the query selects without its State, by
	// state.
	States map[string]int
	// Generation is the decision engine's count of re-evaluations when the
	// page was read.
	Generation uint64
}

// DecisionItem is a decision the node holds, as the list shows it.
type DecisionItem struct {
	// Key is the indicator key; Range is the address or network.
	Key   string
	Range netip.Prefix
	// State is StateBlock, StateNone or StateAllowed.
	State            string
	Score, Threshold float64
	// Contributors counts the contributing publishers.
	Contributors, Quorum int
	// Autoblock is set when this node's own verdict alone decided a block.
	Autoblock bool
	// Rule names the operator's rule that decides, if any: force_allow,
	// allowlist or force_block; Source is where an allow-list entry comes
	// from.
	Rule, Source string
	// Categories are the categories of its active verdicts, the most
	// counting first; Verdicts counts them.
	Categories []string
	Verdicts   int
	// DecidedAt is when it was last evaluated; ExpiresAt when a block
	// ends, zero otherwise.
	DecidedAt, ExpiresAt time.Time
	// Cursor is its place in the order of the query.
	Cursor string
	// Firewall is how the firewall's last pass left its range.
	Firewall Coverage
}

// Coverage is how the firewall's last successful pass left a range.
type Coverage struct {
	// Applied is set if an entry holds the range: Entry, its own if Entry
	// is the range, else a wider one, until EntryExpires.
	Applied      bool
	Entry        netip.Prefix
	EntryExpires time.Time
	// Skipped is why the pass did not apply the range, SkipAllowlist or
	// SkipMaxEntries; Within is the range it was skipped with: its own, or
	// a wider one it lies in.
	Skipped string
	Within  netip.Prefix
	// Deferred is set if its addition waits until an entry it overlaps
	// expired.
	Deferred bool
}

// Explanation is why the node decides as it does on one address or
// network, as obiectl explain tells it, with where the firewall stands.
type Explanation struct {
	Key   string
	Range netip.Prefix
	// State, Score, Threshold, Contributors, Quorum and Autoblock are the
	// decision's, evaluated at EvaluatedAt from the verdicts, the
	// overrides and the allow-list as stored then; Reason sums it up.
	State                string
	Score, Threshold     float64
	Contributors, Quorum int
	Autoblock            bool
	// LocalAutoblock is decision.local_autoblock.
	LocalAutoblock bool
	Reason         string
	// ExpiresAt is when a block ends.
	ExpiresAt, EvaluatedAt time.Time
	// Verdicts are the active verdicts, one per publisher, by peer ID.
	Verdicts []Contribution
	// Ruling is what the allow-list and the overrides do to the range.
	Ruling Ruling
	// Kept is set if the node holds a decision on the range: KeptState,
	// as the decisions list shows it, last evaluated at KeptAt.
	Kept      bool
	KeptState string
	KeptAt    time.Time
	// Around are the decisions the node holds on the networks around the
	// range, the widest first.
	Around []DecisionItem
	// Firewall is how the firewall's last pass left the range.
	Firewall Coverage
}

// Contribution is one publisher's active verdict in an explanation.
type Contribution struct {
	PeerID string
	// Name is the publisher's name in trust.publishers; empty if unlisted.
	Name string
	// Local is set for this node's own verdicts; Listed for publishers in
	// trust.publishers. Others carry trust.default_weight.
	Local, Listed bool
	// Action is ban or watch; Reason and Protocol say what it is about.
	Action, Reason, Protocol string
	Weight, Confidence       float64
	// Score is Weight × Confidence if the verdict contributes, else 0.
	Score       float64
	Contributes bool
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

// Ruling is what the operator's allow-list and overrides do to a range.
type Ruling struct {
	// Effect is allow, block or empty if no rule applies; Rule is
	// force_allow, allowlist or force_block.
	Effect, Rule string
	// Source is where an allow-list entry comes from (builtin, self,
	// bootstrap, config, file) or "override"; Protected is set for the
	// built-in, own and bootstrap entries, which even a force-block cannot
	// overrule.
	Source    string
	Protected bool
	// Match is the allow-listed range or the override's indicator key;
	// Label describes an allow-list entry, Note an override.
	Match, Label, Note string
	// ExpiresAt is when an override ends; zero if it does not.
	ExpiresAt time.Time
	// Reason explains the ruling in one line.
	Reason string
}

// Firewall is the firewall's condition for the firewall view.
type Firewall struct {
	// Facts are the enforcement settings and the condition after the last
	// pass, as the overview shows them.
	Facts EnforceFacts
	// Pass is the last successful pass; nil before the first one.
	Pass *FirewallPass
	// ExpiryTolerance is how far an entry's expiry may drift from the
	// decided one before the reconciler replaces it.
	ExpiryTolerance time.Duration
}

// FirewallPass is what the last successful enforcement pass left.
type FirewallPass struct {
	// Mode is the node mode of the pass; At is when it ended.
	Mode string
	At   time.Time
	// Entries counts the entries the backend holds after it; Deferred the
	// additions that wait until an entry they overlap expired.
	Entries, Deferred int
	// Seq changes whenever what the pass left changes.
	Seq uint64
}

// FirewallEntry is an entry the backend applies, with the decision the
// node holds on the same range.
type FirewallEntry struct {
	Range   netip.Prefix
	Expires time.Time
	// Key, State and ExpiresAt are those of the decision on Range; Key is
	// empty if the node holds none.
	Key, State string
	ExpiresAt  time.Time
}

// DecisionSource reads the decisions and the firewall for the decisions,
// explanation and firewall views (ADR 0022). The daemon implements it with
// cheap reads only, and every read works while its subsystem is stopped.
type DecisionSource interface {
	// Decisions reads the page of the decisions q selects.
	Decisions(q DecisionQuery) DecisionPage
	// Generation is the decision engine's count of re-evaluations.
	Generation() uint64
	// Categories counts the decisions holding an active verdict of each
	// category.
	Categories() map[string]int
	// Explain re-evaluates the address or network p like obiectl explain;
	// it fails for a range that is no indicator, e.g. one broader than
	// /16, and when the verdicts cannot be read.
	Explain(p netip.Prefix) (Explanation, error)
	// Firewall reads the firewall's condition.
	Firewall() Firewall
	// FirewallEntries lists the entries the backend applies right now,
	// ordered by range; none in observe mode.
	FirewallEntries(ctx context.Context) ([]FirewallEntry, error)
}
