// Package store keeps the node's durable local state in BadgerDB: the events
// it accepted, the latest verdict of every publisher on every indicator,
// revocations and operator overrides.
//
// Events expire with their verdict's TTL. Replayed events, verdicts older than
// the publisher's current one and revocations by anyone but the verdict's
// publisher are ignored. Subscribers are notified whenever the set of active
// verdicts or the override of an indicator changes, including by expiry. See
// ADR 0008 for the key layout and the rules.
package store

import (
	"errors"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Name is the subsystem name of the store.
const Name = "store"

// Errors returned by the store.
var (
	// ErrNotFound is returned when an event or override does not exist or
	// has expired.
	ErrNotFound = errors.New("not found")
	// ErrClosed is returned when the database is not open.
	ErrClosed = errors.New("store is not open")
	// ErrInvalid is returned for events or overrides the store cannot hold.
	ErrInvalid = errors.New("invalid")
	// errCorrupt marks a stored value that cannot be decoded.
	errCorrupt = errors.New("corrupt value")
)

// Store is the node's local state, shared by the mesh, the admin API and the
// decision engine. Events passed to Put must have been validated, e.g. by
// obieproto.Decode.
type Store interface {
	// Put stores ev and reports whether it was accepted. Duplicates, expired
	// events, verdicts not newer than the publisher's current one on the
	// indicator and revocations by anyone but the verdict's publisher are
	// ignored (accepted is false, err is nil). A revocation of a verdict the
	// store has not seen yet is accepted and held until it expires; it takes
	// effect only if the verdict arrives from the same publisher for the same
	// indicator, and is counted as ignored otherwise.
	Put(ev *obieproto.Event) (accepted bool, err error)
	// Get returns the stored, unexpired event with the given ID or
	// ErrNotFound.
	Get(id string) (*obieproto.Event, error)
	// Seen reports whether Put already saw an event with the given ID that
	// has not expired, whether it was accepted or ignored; a Put of that ID
	// would be a duplicate.
	Seen(id string) (bool, error)
	// ActiveVerdicts returns the unrevoked verdicts on the indicator (by
	// obieproto.Indicator.Key) that are active at now, at most one per
	// publisher, ordered by publisher.
	ActiveVerdicts(indicatorKey string, now time.Time) ([]*obieproto.Event, error)
	// ListIndicators returns the indicators with at least one active verdict
	// at now that match filter, ordered by key, one page at a time.
	ListIndicators(now time.Time, filter Filter, page Page) (IndicatorPage, error)
	// Subscribe registers fn to be called after every change of an
	// indicator's active verdicts or override. It returns a function that
	// removes the subscription.
	Subscribe(fn func(Change)) (unsubscribe func())

	// SetOverride stores or replaces the operator override of an indicator.
	SetOverride(o Override) error
	// Override returns the override of the indicator that is in effect at
	// now or ErrNotFound.
	Override(indicatorKey string, now time.Time) (Override, error)
	// DeleteOverride removes the override of the indicator and reports
	// whether one existed.
	DeleteOverride(indicatorKey string) (bool, error)
	// Overrides returns every override in effect at now, ordered by key.
	Overrides(now time.Time) ([]Override, error)
}

// Reason says why an indicator changed.
type Reason string

// Change reasons.
const (
	// ReasonVerdict: a verdict was added or replaced by a newer one.
	ReasonVerdict Reason = "verdict"
	// ReasonRevoke: an active verdict was revoked by its publisher.
	ReasonRevoke Reason = "revoke"
	// ReasonExpiry: an active verdict or an override expired.
	ReasonExpiry Reason = "expiry"
	// ReasonOverride: an override was set or deleted.
	ReasonOverride Reason = "override"
	// ReasonEvict: an active verdict was evicted to keep the store within
	// its capacity (Options.MaxIndicators).
	ReasonEvict Reason = "evict"
)

// Change notifies that the state of an indicator changed. It carries no
// state; subscribers re-read the indicator.
type Change struct {
	// Key is the indicator's obieproto.Indicator.Key.
	Key    string
	Reason Reason
}

// Filter restricts ListIndicators. Zero fields match everything.
type Filter struct {
	// Kind matches the indicator kind, e.g. obieproto.KindIPv4.
	Kind string
	// Publisher matches indicators with an active verdict by this peer ID.
	Publisher string
}

// Page selects a page of ListIndicators.
type Page struct {
	// After is the key of the last indicator of the previous page; empty for
	// the first page.
	After string
	// Limit is the page size; 0 means DefaultPageLimit, larger values than
	// MaxPageLimit are capped.
	Limit int
}

// Page size limits.
const (
	DefaultPageLimit = 100
	MaxPageLimit     = 1000
)

// IndicatorState is an indicator with its active verdicts.
type IndicatorState struct {
	Key       string
	Indicator obieproto.Indicator
	// Verdicts are the active verdicts, one per publisher, by publisher.
	Verdicts []*obieproto.Event
}

// IndicatorPage is one page of ListIndicators.
type IndicatorPage struct {
	Items []IndicatorState
	// Next is the Page.After of the following page; empty on the last page.
	Next string
}

// Action is what an operator override enforces.
type Action string

// Override actions.
const (
	ForceAllow Action = "force_allow"
	ForceBlock Action = "force_block"
)

// Override is an operator decision on an indicator that takes precedence
// over the verdicts of the mesh.
type Override struct {
	Indicator obieproto.Indicator `json:"indicator"`
	Action    Action              `json:"action"`
	Note      string              `json:"note,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
	// ExpiresAt is when the override ends; zero means never.
	ExpiresAt time.Time `json:"expires_at"`
}

// Active reports whether the override is in effect at now.
func (o *Override) Active(now time.Time) bool {
	return o.ExpiresAt.IsZero() || now.Before(o.ExpiresAt)
}

// Stats counts the outcomes of Put since the DB was started; the counts are
// not persisted. Early revocations found to be ignored when their verdict
// arrives are counted in ForeignRevoke or InvalidRevoke although Put had
// accepted them.
type Stats struct {
	Accepted      uint64
	Duplicate     uint64
	Stale         uint64
	Expired       uint64
	ForeignRevoke uint64
	InvalidRevoke uint64
	// Full counts new verdicts refused because the store was full and
	// every stored verdict expires later.
	Full uint64
	// Evicted counts stored verdicts evicted to make room.
	Evicted uint64
}
