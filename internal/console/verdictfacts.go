package console

import (
	"net/netip"
	"time"
)

// States of a verdict in the verdicts view (ADR 0023).
const (
	VerdictActive  = "active"
	VerdictRevoked = "revoked"
	VerdictExpired = "expired"
)

// VerdictQuery selects a page of the verdicts the node holds, or kept once
// they ended, ordered by indicator key and publisher (ADR 0023).
type VerdictQuery struct {
	// State is VerdictActive, VerdictRevoked or VerdictExpired.
	State string
	// Publisher keeps the verdicts of the publisher with that peer ID,
	// Except leaves out those of the publisher with that peer ID, and
	// Category keeps those of that category ("reason/protocol"). Empty
	// keeps all.
	Publisher, Except, Category string
	// Range, if valid, keeps the verdicts on exactly that address or
	// network.
	Range netip.Prefix
	// After is the Cursor of the last verdict of the previous page; empty
	// for the first page.
	After string
	Limit int
}

// VerdictList is a page of verdicts.
type VerdictList struct {
	Items []VerdictItem
	// Total counts the verdicts the query selects; Offset is the position
	// of the first item among them.
	Total, Offset int
	// Next is the After of the following page; empty on the last page.
	Next string
	// States counts the verdicts the query selects without its State, by
	// state.
	States map[string]int
}

// VerdictItem is a verdict as the verdicts view shows it.
type VerdictItem struct {
	// Range is the address or network; EventID the verdict's event ID.
	Range   netip.Prefix
	EventID string
	// Publisher is the publisher's peer ID; Local is set for this node's
	// own verdicts. Weight is the trust weight the decisions give it.
	Publisher string
	Local     bool
	Weight    float64
	// Action is ban or watch.
	Action     string
	Confidence float64
	// Reason classifies the behavior; Protocol names the attacked service.
	Reason, Protocol string
	// Events counts the malicious events behind it; LogHash is the hash of
	// the log lines, the only trace of the evidence that left its
	// publisher, empty if none were given.
	Events              int64
	LogHash             string
	IssuedAt, ExpiresAt time.Time
	// State is VerdictActive, VerdictRevoked or VerdictExpired.
	State string
	// Revocation is the revocation of a revoked verdict; nil if not known.
	Revocation *VerdictRevocation
	// Counts is set if an active verdict counts in its decision.
	Counts bool
	// Decision is the state of the decision the node holds on Range; empty
	// if it holds none.
	Decision string
	// Cursor is its place in the order, for VerdictQuery.After.
	Cursor string
}

// VerdictRevocation is the revocation that ended a verdict.
type VerdictRevocation struct {
	// ID is the revocation event's ID; Reason says why, e.g.
	// "false_positive"; At is when it was issued.
	ID, Reason string
	At         time.Time
}

// VerdictTotals count the verdicts by publisher.
type VerdictTotals struct {
	// ByPublisher counts the verdicts of every publisher by peer ID; this
	// node's own are under its peer ID.
	ByPublisher map[string]VerdictCounts
	// Retention is how long the node keeps a verdict after its expiry once
	// it was revoked or expired; EndedMax is how many of other publishers'
	// it keeps at most in each of those states, and EndedFull says, by
	// VerdictRevoked and VerdictExpired, where it keeps that many and so no
	// more until older ones are forgotten.
	Retention time.Duration
	EndedMax  int
	EndedFull map[string]bool
}

// VerdictCounts count one publisher's verdicts.
type VerdictCounts struct {
	// Active counts its active verdicts, Counting those that count in
	// decisions; Revoked and Expired those the node keeps that ended.
	Active, Counting, Revoked, Expired int
}

// VerdictSource reads the verdicts for the verdicts view (ADR 0023). The
// daemon implements it with cheap reads only; every read works while its
// subsystem is stopped.
type VerdictSource interface {
	// Verdicts reads the page of the verdicts q selects; it fails with
	// ErrNoIndicator for a Range no verdict can be on.
	Verdicts(q VerdictQuery) (VerdictList, error)
	// Totals counts the verdicts by publisher.
	Totals() (VerdictTotals, error)
	// Categories counts the decisions holding an active verdict of each
	// category.
	Categories() map[string]int
}
