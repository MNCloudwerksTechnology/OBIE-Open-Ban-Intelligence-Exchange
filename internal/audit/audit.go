// Package audit writes the decision audit log: one JSON object per line
// for every decision change, with Elastic Common Schema (ECS) field names,
// so Loki, Elasticsearch, Splunk and other SIEMs ingest it as is. The file
// is only ever appended to and is reopened on SIGHUP, so logrotate can
// move it away (ADR 0015).
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Name is the subsystem name of the audit log.
const Name = "audit"

// fileMode is the mode of a newly created audit log.
const fileMode = 0o640

// timeFormat is the format of @timestamp: UTC with milliseconds.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// Action is the event.action of a record.
type Action string

// Actions, one per kind of decision change.
const (
	// ActionBlockAdded: an indicator became blocked.
	ActionBlockAdded Action = "block-added"
	// ActionBlockUpdated: a block's expiry, score, publishers or rule changed.
	ActionBlockUpdated Action = "block-updated"
	// ActionBlockRemoved: an indicator is no longer blocked.
	ActionBlockRemoved Action = "block-removed"
	// ActionAllowed: the allow-list or a force-allow override keeps an
	// indicator with verdicts from being blocked.
	ActionAllowed Action = "allowed-by-allowlist"
	// ActionOverrideSet: the operator set or replaced an override.
	ActionOverrideSet Action = "override-set"
	// ActionOverrideRemoved: the operator deleted an override.
	ActionOverrideRemoved Action = "override-removed"
	// ActionLocalReport: this node issued a verdict on a local detection.
	ActionLocalReport Action = "local-report"
	// ActionRevocation: this node revoked one of its verdicts.
	ActionRevocation Action = "revocation"
	// ActionPeerConnected: the mesh connected to a peer.
	ActionPeerConnected Action = "peer-connected"
	// ActionPeerDisconnected: the mesh lost its last connection to a peer.
	ActionPeerDisconnected Action = "peer-disconnected"
	// ActionConfigReloaded: a reload of the configuration took effect.
	ActionConfigReloaded Action = "config-reloaded"
	// ActionModeChanged: a reload switched node.mode.
	ActionModeChanged Action = "mode-changed"
)

// OutcomeSuccess is the event.outcome of every record: only changes that
// took effect are recorded.
const OutcomeSuccess = "success"

// Scores are the numbers behind a decision.
type Scores struct {
	Score     float64
	Threshold float64
	// Publishers counts the publishers whose verdicts contribute.
	Publishers int
}

// Record is one change: of a decision, an override, a verdict this node
// issued, a peer's connection, the configuration or the mode (ADR 0025).
type Record struct {
	Action Action
	// Indicator is the address or range the change is about; zero for
	// changes about no address.
	Indicator obieproto.Indicator
	// Rule names the rule behind the change (rule.name), e.g. "consensus",
	// "allowlist" or "force_block".
	Rule string
	// Reason explains the change in one line (event.reason).
	Reason string
	// State is the decision state after the change; empty for records that
	// are not decisions.
	State string
	// Scores are set for decisions.
	Scores *Scores
	// Cause is what triggered a decision change, e.g. "verdict" or "reload".
	Cause string
	// ExpiresAt is when a block, override or verdict ends; zero for never.
	ExpiresAt time.Time
	// EventID is the ID of the event this node issued.
	EventID string
	// Revokes is the ID of the verdict a revocation withdraws.
	Revokes string
	// Note is the operator's note on an override.
	Note string
	// PreviousMode is node.mode before a mode change.
	PreviousMode string
	// PeerID and PeerName are the peer of a connection change; PeerName
	// is its trust.publishers name, if any.
	PeerID, PeerName string
	// Settings are the keys a reload changed and applied; RestartSettings
	// the keys that wait for a restart.
	Settings, RestartSettings []string
}

// Log appends Records to the audit log file. It implements
// lifecycle.Subsystem: Start opens the file, Stop closes it. The methods of
// a nil *Log do nothing, so callers need not check whether auditing is
// enabled.
type Log struct {
	path string
	mode func() string
	now  func() time.Time
	log  *slog.Logger

	mu sync.Mutex
	f  *os.File
}

// Options configures a Log.
type Options struct {
	// Mode returns the current node.mode for obie.mode. Required.
	Mode func() string
	// Now is the clock of @timestamp; time.Now when nil.
	Now func() time.Time
}

// New returns the audit log at path.
func New(path string, opts Options, log *slog.Logger) *Log {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Log{path: path, mode: opts.Mode, now: opts.Now, log: log}
}

// Name returns the subsystem name.
func (l *Log) Name() string { return Name }

// Start opens the file, creating it if needed.
func (l *Log) Start(context.Context) error {
	f, err := l.open()
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = f.Close()
		return errors.New("audit log already started")
	}
	l.f = f
	l.log.Info("audit log opened", "path", l.path)
	return nil
}

// Stop closes the file.
func (l *Log) Stop(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	if err != nil {
		return fmt.Errorf("close audit log: %w", err)
	}
	return nil
}

// Reopen opens the file at the configured path again, e.g. after
// logrotate moved it away, and closes the previous one. If the file cannot
// be opened, writing goes on to the previous one.
func (l *Log) Reopen() error {
	if l == nil {
		return nil
	}
	f, err := l.open()
	if err != nil {
		l.log.Error("reopening the audit log failed; writing on to the previous file", "path", l.path, "error", err)
		return err
	}
	l.mu.Lock()
	old := l.f
	l.f = f
	l.mu.Unlock()
	if old != nil {
		if err := old.Close(); err != nil {
			l.log.Warn("closing the previous audit log failed", "error", err)
		}
	}
	l.log.Info("audit log reopened", "path", l.path)
	return nil
}

func (l *Log) open() (*os.File, error) {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, fileMode) // #nosec G304 -- the operator chooses audit.path.
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return f, nil
}

// Write appends r as one line. A failure is logged; the change it records
// takes effect anyway.
func (l *Log) Write(r Record) {
	if l == nil {
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf) // one line, ending in a newline
	enc.SetEscapeHTML(false)     // reasons hold ">=" and "<"
	if err := enc.Encode(l.entry(r)); err != nil {
		l.log.Error("encoding an audit record failed", "action", r.Action, "error", err)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		l.log.Error("audit log is not open; record lost", "action", r.Action, "indicator", indicatorKey(r.Indicator))
		return
	}
	if _, err := l.f.Write(buf.Bytes()); err != nil {
		l.log.Error("writing the audit log failed; record lost", "action", r.Action, "indicator", indicatorKey(r.Indicator),
			"error", err)
	}
}

// Entry is a record in ECS form: one line of the audit log, and one entry
// of the console's activity timeline (ADR 0025).
type Entry struct {
	Timestamp string        `json:"@timestamp"`
	Event     EventFields   `json:"event"`
	Source    *SourceFields `json:"source,omitempty"`
	Rule      *RuleFields   `json:"rule,omitempty"`
	Obie      ObieFields    `json:"obie"`
}

// EventFields are the ECS event.* fields.
type EventFields struct {
	Kind    string `json:"kind"`
	Module  string `json:"module"`
	Dataset string `json:"dataset"`
	Action  Action `json:"action"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

// SourceFields are the ECS source.* fields.
type SourceFields struct {
	IP string `json:"ip"`
}

// RuleFields are the ECS rule.* fields.
type RuleFields struct {
	Name string `json:"name"`
}

// ObieFields are OBIE's own fields.
type ObieFields struct {
	Indicator       string   `json:"indicator,omitempty"`
	Mode            string   `json:"mode"`
	State           string   `json:"state,omitempty"`
	Score           *float64 `json:"score,omitempty"`
	Threshold       *float64 `json:"threshold,omitempty"`
	Publishers      *int     `json:"publishers,omitempty"`
	Cause           string   `json:"cause,omitempty"`
	ExpiresAt       string   `json:"expires_at,omitempty"`
	EventID         string   `json:"event_id,omitempty"`
	Revokes         string   `json:"revokes,omitempty"`
	Note            string   `json:"note,omitempty"`
	PreviousMode    string   `json:"previous_mode,omitempty"`
	PeerID          string   `json:"peer_id,omitempty"`
	PeerName        string   `json:"peer_name,omitempty"`
	Settings        []string `json:"settings,omitempty"`
	RestartSettings []string `json:"restart_settings,omitempty"`
}

// Time returns @timestamp as a time; zero if it is not one.
func (e *Entry) Time() time.Time {
	t, err := time.Parse(time.RFC3339Nano, e.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (l *Log) entry(r Record) Entry {
	e := Entry{
		Timestamp: l.now().UTC().Format(timeFormat),
		Event: EventFields{Kind: "event", Module: "obie", Dataset: "obie.audit", Action: r.Action, Outcome: OutcomeSuccess,
			Reason: r.Reason},
		Obie: ObieFields{Indicator: indicatorKey(r.Indicator), Mode: l.mode(), State: r.State, Cause: r.Cause,
			EventID: r.EventID, Revokes: r.Revokes, Note: r.Note, PreviousMode: r.PreviousMode, PeerID: r.PeerID,
			PeerName: r.PeerName, Settings: r.Settings, RestartSettings: r.RestartSettings},
	}
	if ip, ok := singleAddress(r.Indicator); ok {
		e.Source = &SourceFields{IP: ip.String()}
	}
	if r.Rule != "" {
		e.Rule = &RuleFields{Name: r.Rule}
	}
	if s := r.Scores; s != nil {
		e.Obie.Score, e.Obie.Threshold, e.Obie.Publishers = &s.Score, &s.Threshold, &s.Publishers
	}
	if !r.ExpiresAt.IsZero() {
		e.Obie.ExpiresAt = r.ExpiresAt.UTC().Format(timeFormat)
	}
	return e
}

// indicatorKey returns the key of ind, or "" for the zero indicator of a
// record about no address.
func indicatorKey(ind obieproto.Indicator) string {
	if ind.Kind == "" {
		return ""
	}
	return ind.Key()
}

// singleAddress returns the address of an indicator that names one
// address; source.ip holds no ranges.
func singleAddress(ind obieproto.Indicator) (netip.Addr, bool) {
	p, err := sovereignty.PrefixOf(ind)
	if err != nil || !p.IsSingleIP() {
		return netip.Addr{}, false
	}
	return p.Addr(), true
}
