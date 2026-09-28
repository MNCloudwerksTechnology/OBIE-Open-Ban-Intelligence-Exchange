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

// Record is one decision change.
type Record struct {
	Action    Action
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
	if err := enc.Encode(l.document(r)); err != nil {
		l.log.Error("encoding an audit record failed", "action", r.Action, "error", err)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		l.log.Error("audit log is not open; record lost", "action", r.Action, "indicator", r.Indicator.Key())
		return
	}
	if _, err := l.f.Write(buf.Bytes()); err != nil {
		l.log.Error("writing the audit log failed; record lost", "action", r.Action, "indicator", r.Indicator.Key(), "error", err)
	}
}

// document is a record in ECS form.
type document struct {
	Timestamp string      `json:"@timestamp"`
	Event     eventFields `json:"event"`
	Source    *source     `json:"source,omitempty"`
	Rule      *rule       `json:"rule,omitempty"`
	Obie      obieFields  `json:"obie"`
}

type eventFields struct {
	Kind    string `json:"kind"`
	Module  string `json:"module"`
	Dataset string `json:"dataset"`
	Action  Action `json:"action"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

type source struct {
	IP string `json:"ip"`
}

type rule struct {
	Name string `json:"name"`
}

type obieFields struct {
	Indicator  string   `json:"indicator"`
	Mode       string   `json:"mode"`
	State      string   `json:"state,omitempty"`
	Score      *float64 `json:"score,omitempty"`
	Threshold  *float64 `json:"threshold,omitempty"`
	Publishers *int     `json:"publishers,omitempty"`
	Cause      string   `json:"cause,omitempty"`
	ExpiresAt  string   `json:"expires_at,omitempty"`
	EventID    string   `json:"event_id,omitempty"`
	Revokes    string   `json:"revokes,omitempty"`
	Note       string   `json:"note,omitempty"`
}

func (l *Log) document(r Record) document {
	d := document{
		Timestamp: l.now().UTC().Format(timeFormat),
		Event: eventFields{Kind: "event", Module: "obie", Dataset: "obie.audit", Action: r.Action, Outcome: OutcomeSuccess,
			Reason: r.Reason},
		Obie: obieFields{Indicator: r.Indicator.Key(), Mode: l.mode(), State: r.State, Cause: r.Cause,
			EventID: r.EventID, Revokes: r.Revokes, Note: r.Note},
	}
	if ip, ok := singleAddress(r.Indicator); ok {
		d.Source = &source{IP: ip.String()}
	}
	if r.Rule != "" {
		d.Rule = &rule{Name: r.Rule}
	}
	if s := r.Scores; s != nil {
		d.Obie.Score, d.Obie.Threshold, d.Obie.Publishers = &s.Score, &s.Threshold, &s.Publishers
	}
	if !r.ExpiresAt.IsZero() {
		d.Obie.ExpiresAt = r.ExpiresAt.UTC().Format(timeFormat)
	}
	return d
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
