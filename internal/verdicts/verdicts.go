// Package verdicts issues this node's own events: it turns a local
// detection into a signed indicator.verdict, withdraws it again with an
// indicator.revoke and lists the active verdicts the node holds (ADR 0012).
//
// Raw evidence never leaves the package: log lines are reduced to
// evidence.log_hash on receipt and then dropped, and nothing but the hash and
// the counts is stored, published or logged.
package verdicts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Name is the logger name of the verdict service.
const Name = "verdicts"

// CoalesceWindow is the shortest time between two verdicts of this node on
// the same indicator; reports within it are coalesced: only their event
// counts are kept, for the next refresh.
const CoalesceWindow = 60 * time.Second

// publishTimeout bounds handing an event to the publisher.
const publishTimeout = 10 * time.Second

// DefaultConfidence is the confidence of a report that sets none.
const DefaultConfidence = 0.8

// Errors returned by the Service; test with errors.Is.
var (
	// ErrInvalid reports a request that violates a rule; the message names
	// the field.
	ErrInvalid = errors.New("invalid request")
	// ErrRefused reports an indicator this node must not publish: it is
	// allow-listed or not a public address.
	ErrRefused = errors.New("refused")
	// ErrNotFound reports that this node holds no active verdict to revoke.
	ErrNotFound = errors.New("not found")
)

// Publisher stores an event of this node and sends it to the mesh; the mesh
// implements it.
type Publisher interface {
	Publish(ctx context.Context, ev *obieproto.Event) error
}

// Options configures a Service.
type Options struct {
	// Store holds the events; the Service reads this node's active verdicts
	// from it.
	Store store.Store
	// Publisher stores and sends the issued events.
	Publisher Publisher
	// Signer signs the events; its peer ID is the publisher of every event.
	Signer obieproto.Signer
	// DefaultTTL is the lifetime of a verdict whose report sets none
	// (decision.default_ttl); MaxTTL caps longer ones (decision.max_ttl).
	DefaultTTL, MaxTTL time.Duration
	// Allowlist holds the networks that are never reported (allowlist.cidrs).
	Allowlist []netip.Prefix
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// AllowDocumentationRanges issues verdicts on documentation addresses
	// (obieproto.AllowDocumentationRanges). Only for multi-node tests;
	// production nodes never set it.
	AllowDocumentationRanges bool
}

// Report is a local detection to publish as a verdict.
type Report struct {
	Indicator obieproto.Indicator
	// Protocol is the attacked service, e.g. "ssh".
	Protocol string
	// Reason classifies the behavior, e.g. "password_bruteforce".
	Reason string
	// Events is the number of malicious events observed.
	Events int64
	// EvidenceLines is the log excerpt behind the report. Only its hash is
	// kept.
	EvidenceLines []string
	// Confidence is in [0, 1]; DefaultConfidence when nil.
	Confidence *float64
	// TTL is the verdict lifetime; Options.DefaultTTL when 0, capped at
	// Options.MaxTTL.
	TTL time.Duration
	// Action is obieproto.ActionBan (the default for "") or
	// obieproto.ActionWatch.
	Action string
	// MITRE lists MITRE ATT&CK technique IDs; optional.
	MITRE []string
}

// Result is the outcome of a report.
type Result struct {
	// Event is the issued verdict, or the current verdict if the report was
	// coalesced.
	Event *obieproto.Event
	// Coalesced is set when the report issued no new verdict because this
	// node issued one on the indicator less than CoalesceWindow ago; its
	// events are added to the next refresh.
	Coalesced bool
	// Supersedes is the ID of the verdict the new one refreshes; empty for
	// a first verdict and for coalesced reports.
	Supersedes string
}

// Revocation names what to revoke: exactly one of EventID and Indicator.
type Revocation struct {
	// EventID is the ID of a verdict of this node.
	EventID string
	// Indicator selects this node's active verdict on the indicator.
	Indicator *obieproto.Indicator
	// Reason explains the revocation, e.g. "false_positive".
	Reason string
}

// pending holds the events of coalesced reports until the next refresh of
// the verdict they were coalesced into.
type pending struct {
	verdictID string
	expires   time.Time
	events    int64
}

// Service issues and lists this node's verdicts. It is safe for concurrent
// use.
type Service struct {
	opts Options
	log  *slog.Logger

	// mu serializes reports and revocations, so the active verdict read
	// from the store is still the current one when it is refreshed.
	mu      sync.Mutex
	pending map[string]pending
}

// New returns a Service.
func New(opts Options, log *slog.Logger) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{opts: opts, log: log, pending: make(map[string]pending)}
}

// PeerID returns the peer ID this node publishes under.
func (s *Service) PeerID() string { return s.opts.Signer.PeerID() }

// Report issues a verdict for r, refreshes this node's active verdict on
// the indicator, or coalesces r into it; see Result. The evidence lines are
// hashed and dropped. Errors match ErrInvalid or ErrRefused for requests
// that are not acted on.
func (s *Service) Report(ctx context.Context, r Report) (Result, error) {
	logHash := hashEvidence(r.EvidenceLines)
	r.EvidenceLines = nil

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now()
	plan, err := s.plan(r, logHash, now)
	if err != nil {
		return Result{}, err
	}
	ev, prev := plan.Verdict, plan.Current
	if plan.Coalesced {
		s.coalesce(prev, r.Events)
		s.log.Info("report coalesced into the current verdict", "event", prev.ID, "indicator", ev.Key(),
			"events", r.Events)
		return Result{Event: prev, Coalesced: true}, nil
	}

	var supersedes string
	if prev != nil {
		supersedes = prev.ID
		ev.Evidence.Events = addEvents(ev.Evidence.Events, prev.Evidence.Events, s.takePending(ev.Key(), prev.ID))
		if ev.Evidence.LogHash == "" {
			ev.Evidence.LogHash = prev.Evidence.LogHash
		}
	}
	if err := s.publish(ctx, ev); err != nil {
		return Result{}, err
	}
	delete(s.pending, ev.Key())
	s.log.Info("verdict issued", "event", ev.ID, "indicator", ev.Key(), "action", ev.Verdict.SuggestedAction,
		"events", ev.Evidence.Events, "ttl", (time.Duration(ev.Verdict.TTLSeconds) * time.Second).String(),
		"supersedes", supersedes, "log_hash", ev.Evidence.LogHash)
	return Result{Event: ev, Supersedes: supersedes}, nil
}

// Plan is what a report would do, as Check tells it.
type Plan struct {
	// Verdict is the verdict the report would issue, unsigned: its
	// indicator normalized, its lifetime, confidence and action resolved.
	// A refresh would add Current's events to it.
	Verdict *obieproto.Event
	// Current is this node's active verdict on the indicator, which the
	// report would refresh or be coalesced into; nil if there is none.
	Current *obieproto.Event
	// Coalesced is set if the report would be coalesced into Current,
	// issued less than CoalesceWindow ago, rather than issue a verdict.
	Coalesced bool
}

// Check applies every rule Report applies to r and tells what the report
// would do, without issuing or publishing anything; its errors are those
// Report would return. The console checks a report with it before asking
// to confirm it (ADR 0026).
func (s *Service) Check(r Report) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plan(r, hashEvidence(r.EvidenceLines), s.opts.Now())
}

// plan builds and checks the verdict r asks for at now and finds this
// node's active verdict it would refresh or be coalesced into. The caller
// holds mu, so the active verdict is still the current one when it is
// refreshed.
func (s *Service) plan(r Report, logHash string, now time.Time) (Plan, error) {
	ind := r.Indicator
	if err := ind.Normalize(); err != nil {
		return Plan{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := s.checkAllowlist(ind); err != nil {
		return Plan{}, err
	}
	ttl, err := s.ttl(r.TTL)
	if err != nil {
		return Plan{}, err
	}
	confidence := DefaultConfidence
	if r.Confidence != nil {
		confidence = *r.Confidence
	}
	action := r.Action
	if action == "" {
		action = obieproto.ActionBan
	}
	mitre := r.MITRE
	if len(mitre) == 0 {
		mitre = nil
	}
	ev := &obieproto.Event{
		ID:        obieproto.NewID(now),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(now),
		Indicator: ind,
		Protocol:  r.Protocol,
		Evidence:  &obieproto.Evidence{Events: r.Events, Reason: r.Reason, LogHash: logHash},
		Verdict:   &obieproto.Verdict{SuggestedAction: action, Confidence: confidence, TTLSeconds: int64(ttl / time.Second)},
		MITRE:     mitre,
		Publisher: obieproto.Publisher{PeerID: s.PeerID()},
	}
	// The report is validated on its own before counts are merged, so an
	// invalid report never touches the active verdict.
	if err := s.validate(ev, now); err != nil {
		return Plan{}, err
	}

	s.prunePending(now)
	prev, err := s.activeVerdict(ind.Key(), now)
	if err != nil {
		return Plan{}, err
	}
	// issued_at is truncated to whole seconds, so the window is extended by
	// one second to never let two verdicts be less than CoalesceWindow apart.
	coalesced := prev != nil && now.Before(prev.IssuedAt.Add(CoalesceWindow+time.Second))
	return Plan{Verdict: ev, Current: prev, Coalesced: coalesced}, nil
}

// Revoke withdraws this node's active verdict named by r and returns the
// issued revocations. Errors match ErrInvalid for a malformed request and
// ErrNotFound when there is nothing to revoke.
func (s *Service) Revoke(ctx context.Context, r Revocation) ([]*obieproto.Event, error) {
	if (r.EventID == "") == (r.Indicator == nil) {
		return nil, fmt.Errorf("%w: give exactly one of event_id and indicator", ErrInvalid)
	}
	if r.Reason == "" {
		return nil, fmt.Errorf("%w: reason: missing", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now()
	targets, err := s.revocationTargets(r, now)
	if err != nil {
		return nil, err
	}
	revocations := make([]*obieproto.Event, 0, len(targets))
	for _, target := range targets {
		ev := &obieproto.Event{
			ID:        obieproto.NewID(now),
			Spec:      obieproto.Spec,
			Type:      obieproto.TypeRevoke,
			IssuedAt:  obieproto.NewTimestamp(now),
			Indicator: target.Indicator,
			Revokes:   target.ID,
			Reason:    r.Reason,
			Publisher: obieproto.Publisher{PeerID: s.PeerID()},
		}
		if err := s.validate(ev, now); err != nil {
			return revocations, err
		}
		if err := s.publish(ctx, ev); err != nil {
			return revocations, err
		}
		delete(s.pending, target.Key())
		s.log.Info("verdict revoked", "event", ev.ID, "revokes", target.ID, "indicator", target.Key(), "reason", r.Reason)
		revocations = append(revocations, ev)
	}
	return revocations, nil
}

// revocationTargets returns this node's active verdicts that r names, or
// an error matching ErrNotFound if there are none.
func (s *Service) revocationTargets(r Revocation, now time.Time) ([]*obieproto.Event, error) {
	if r.Indicator != nil {
		ind := *r.Indicator
		if err := ind.Normalize(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		v, err := s.activeVerdict(ind.Key(), now)
		if err != nil {
			return nil, err
		}
		if v == nil {
			return nil, fmt.Errorf("%w: this node has no active verdict on %s", ErrNotFound, ind.Key())
		}
		return []*obieproto.Event{v}, nil
	}

	ev, err := s.opts.Store.Get(r.EventID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: no event %s", ErrNotFound, r.EventID)
	}
	if err != nil {
		return nil, err
	}
	if ev.Type != obieproto.TypeVerdict || ev.Publisher.PeerID != s.PeerID() {
		return nil, fmt.Errorf("%w: event %s is not a verdict of this node", ErrNotFound, r.EventID)
	}
	v, err := s.activeVerdict(ev.Key(), now)
	if err != nil {
		return nil, err
	}
	if v == nil || v.ID != ev.ID {
		return nil, fmt.Errorf("%w: verdict %s is no longer active (refreshed, revoked or expired)", ErrNotFound, r.EventID)
	}
	return []*obieproto.Event{v}, nil
}

// List returns a page of the indicators with active verdicts, restricted
// to those with a verdict by publisher unless it is empty.
func (s *Service) List(publisher string, page store.Page) (store.IndicatorPage, error) {
	return s.opts.Store.ListIndicators(s.opts.Now(), store.Filter{Publisher: publisher}, page)
}

// Lookup returns every active verdict on the indicator, by publisher.
func (s *Service) Lookup(ind obieproto.Indicator) ([]*obieproto.Event, error) {
	if err := ind.Normalize(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return s.opts.Store.ActiveVerdicts(ind.Key(), s.opts.Now())
}

// hashEvidence returns the evidence.log_hash of lines: SHA-256 over the
// lines joined with "\n", or "" for no lines.
func hashEvidence(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// checkAllowlist refuses an indicator that overlaps an allow-listed network.
func (s *Service) checkAllowlist(ind obieproto.Indicator) error {
	text := ind.Value
	if ind.Kind != obieproto.KindCIDR {
		text += ind.Scope
	}
	prefix, err := netip.ParsePrefix(text)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	for _, allowed := range s.opts.Allowlist {
		if prefix.Overlaps(allowed) {
			return fmt.Errorf("%w: %s overlaps the allow-listed network %s (allowlist.cidrs); allow-listed addresses are never reported",
				ErrRefused, ind.Key(), allowed)
		}
	}
	return nil
}

// ttl resolves the requested lifetime: the default for 0, capped at the
// maximum, in whole seconds.
func (s *Service) ttl(requested time.Duration) (time.Duration, error) {
	switch {
	case requested < 0:
		return 0, fmt.Errorf("%w: ttl: must not be negative", ErrInvalid)
	case requested == 0:
		requested = s.opts.DefaultTTL
	}
	maxTTL := time.Duration(obieproto.MaxTTLSeconds) * time.Second
	if s.opts.MaxTTL > 0 {
		maxTTL = min(maxTTL, s.opts.MaxTTL)
	}
	requested = min(requested, maxTTL).Truncate(time.Second)
	if requested < obieproto.MinTTLSeconds*time.Second {
		return 0, fmt.Errorf("%w: ttl: %s is shorter than the minimum of %ds", ErrInvalid, requested, obieproto.MinTTLSeconds)
	}
	return requested, nil
}

// validate checks an event this node is about to issue; a non-public
// indicator is refused, other violations are invalid.
func (s *Service) validate(ev *obieproto.Event, now time.Time) error {
	opts := []obieproto.Option{obieproto.WithClock(func() time.Time { return now })}
	if s.opts.AllowDocumentationRanges {
		opts = append(opts, obieproto.AllowDocumentationRanges())
	}
	err := ev.Validate(opts...)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, obieproto.ErrNonPublicIndicator):
		// The detail names the range, e.g. "192.168.1.9/32 overlaps
		// special-purpose range 192.168.0.0/16".
		detail := err.Error()
		var fe *obieproto.FieldError
		if errors.As(err, &fe) && fe.Detail != "" {
			detail = fe.Detail
		}
		return fmt.Errorf("%w: %s is not a public address: %s; OBIE never publishes internal or special-purpose addresses",
			ErrRefused, ev.Key(), detail)
	default:
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
}

// activeVerdict returns this node's active verdict on the indicator, or nil.
func (s *Service) activeVerdict(key string, now time.Time) (*obieproto.Event, error) {
	active, err := s.opts.Store.ActiveVerdicts(key, now)
	if err != nil {
		return nil, fmt.Errorf("read active verdicts on %s: %w", key, err)
	}
	for _, v := range active {
		if v.Publisher.PeerID == s.PeerID() {
			return v, nil
		}
	}
	return nil, nil
}

// publish signs ev and hands it to the publisher.
func (s *Service) publish(ctx context.Context, ev *obieproto.Event) error {
	if err := obieproto.SignWith(ev, s.opts.Signer); err != nil {
		return fmt.Errorf("sign event %s: %w", ev.ID, err)
	}
	// A client that disconnects must not interrupt a publish that may
	// already have stored the event locally.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	if err := s.opts.Publisher.Publish(ctx, ev); err != nil {
		return fmt.Errorf("publish event %s: %w", ev.ID, err)
	}
	return nil
}

// coalesce adds the events of a report to the next refresh of verdict v.
func (s *Service) coalesce(v *obieproto.Event, events int64) {
	p := s.pending[v.Key()]
	if p.verdictID != v.ID {
		p = pending{verdictID: v.ID, expires: v.ExpiresAt()}
	}
	p.events = addEvents(p.events, events)
	s.pending[v.Key()] = p
}

// takePending returns the coalesced events of verdict id on the indicator.
func (s *Service) takePending(key, id string) int64 {
	p, ok := s.pending[key]
	if !ok || p.verdictID != id {
		return 0
	}
	return p.events
}

// prunePending drops the coalesced events of expired verdicts.
func (s *Service) prunePending(now time.Time) {
	for key, p := range s.pending {
		if !now.Before(p.expires) {
			delete(s.pending, key)
		}
	}
}

// addEvents sums event counts, saturating at obieproto.MaxEvidenceEvents.
func addEvents(counts ...int64) int64 {
	var sum int64
	for _, n := range counts {
		if n > obieproto.MaxEvidenceEvents-sum {
			return obieproto.MaxEvidenceEvents
		}
		sum += n
	}
	return sum
}
