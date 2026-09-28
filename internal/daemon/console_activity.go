package daemon

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// consoleActivity reads the audit trail for the console's activity
// timeline (ADR 0025).
type consoleActivity struct {
	log *audit.Log
}

var _ console.ActivitySource = consoleActivity{}

func (a consoleActivity) Timeline(f console.ActivityFilter, before string, limit int) (console.ActivityPage, error) {
	var cursor *audit.Cursor
	if before != "" {
		c, err := audit.ParseCursor(before)
		if err != nil {
			return console.ActivityPage{}, err
		}
		cursor = &c
	}
	mark := a.log.Mark()
	p := a.log.History(mark, cursor, limit, activityMatch(f))
	page := console.ActivityPage{Entries: activityEntries(p.Entries), Live: mark.Seq, Path: a.log.Path(), Memory: p.Memory,
		Kept: audit.MemoryEntries, Forgotten: p.Forgotten, Searched: p.Searched, SearchedTo: p.SearchedTo, Skipped: p.Skipped,
		FileSince: p.FileSince}
	if p.Older != nil {
		page.Older = p.Older.String()
	}
	if p.FileErr != nil && !errors.Is(p.FileErr, audit.ErrNoFile) {
		page.FileErr = p.FileErr.Error()
	}
	return page, nil
}

func (a consoleActivity) Live(f console.ActivityFilter, after uint64, limit int) console.ActivityBatch {
	r := a.log.Since(after, limit, activityMatch(f))
	b := console.ActivityBatch{Entries: activityEntries(r.Entries), Next: r.Next, From: r.From, To: r.To, Lost: r.Lost}
	if r.More != nil {
		b.More = make(map[string]int, len(r.More))
		for action, n := range r.More {
			b.More[string(action)] = n
		}
	}
	return b
}

// activityMatch returns whether a record is one f selects.
func activityMatch(f console.ActivityFilter) func(*audit.Entry) bool {
	return func(e *audit.Entry) bool {
		if len(f.Actions) > 0 && !slices.Contains(f.Actions, string(e.Event.Action)) {
			return false
		}
		if !f.Range.IsValid() {
			return true
		}
		p, ok := indicatorRange(e.Obie.Indicator)
		return ok && p.Overlaps(f.Range)
	}
}

// indicatorRange returns the address range of an indicator key such as
// "ipv4:203.0.113.7"; ok is false for a record about no address.
func indicatorRange(key string) (p netip.Prefix, ok bool) {
	kind, value, found := strings.Cut(key, ":")
	if !found {
		return netip.Prefix{}, false
	}
	p, err := sovereignty.PrefixOf(obieproto.Indicator{Kind: kind, Value: value})
	return p, err == nil
}

// activityEntries converts audit records for the console.
func activityEntries(entries []audit.Entry) []console.ActivityEntry {
	out := make([]console.ActivityEntry, len(entries))
	for i := range entries {
		e := &entries[i]
		out[i] = console.ActivityEntry{Time: e.Time(), Action: string(e.Event.Action), Reason: e.Event.Reason,
			Cause: e.Obie.Cause, Mode: e.Obie.Mode, Note: e.Obie.Note, PeerID: e.Obie.PeerID, PeerName: e.Obie.PeerName}
		out[i].Range, _ = indicatorRange(e.Obie.Indicator)
		if e.Rule != nil {
			out[i].Rule = e.Rule.Name
		}
		if t, err := time.Parse(time.RFC3339Nano, e.Obie.ExpiresAt); err == nil {
			out[i].ExpiresAt = t
		}
	}
	return out
}
