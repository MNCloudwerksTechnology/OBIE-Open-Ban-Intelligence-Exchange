package console

import (
	"net/netip"
	"time"
)

// ActivityEntry is an entry of the activity timeline: one record of the
// node's audit trail (ADR 0025).
type ActivityEntry struct {
	// Time is when it happened.
	Time time.Time
	// Action is the record's event.action, e.g. "block-added".
	Action string
	// Range is the address or network the entry is about; not valid for
	// an entry about none.
	Range netip.Prefix
	// Reason is the record's event.reason, Rule its rule.name and Cause
	// what triggered a decision change.
	Reason, Rule, Cause string
	// Mode is node.mode at the time, the new one for a mode change.
	Mode string
	// ExpiresAt is when a block, override or verdict ends; zero if never.
	ExpiresAt time.Time
	// Note is the note of an override.
	Note string
	// PeerID and PeerName are the peer of a connection change.
	PeerID, PeerName string
}

// ActivityFilter selects entries of the timeline.
type ActivityFilter struct {
	// Actions are the actions selected; all if empty.
	Actions []string
	// Range, if valid, selects the entries about an address or network
	// that overlaps it.
	Range netip.Prefix
}

// ActivityPage is a page of the timeline, newest first.
type ActivityPage struct {
	Entries []ActivityEntry
	// Live is where the live feed continues after the page: the number of
	// the last entry recorded when it was read.
	Live uint64
	// Older is where the next older page starts; empty at the first entry.
	Older string
	// Path is the audit log file; empty if the audit log is off.
	Path string
	// Memory is set if the page comes from the entries kept in memory
	// rather than the file; FileErr then says why, unless the audit log is
	// off.
	Memory  bool
	FileErr string
	// Kept is how many of the last entries are kept in memory; Forgotten
	// is set if older ones were written, so memory lacks them.
	Kept      int
	Forgotten bool
	// Searched is set if the page ends because the node read as much of
	// the file as it reads for one page; SearchedTo is how far back.
	Searched   bool
	SearchedTo time.Time
	// Skipped counts the lines of the file that are no records.
	Skipped int
	// FileSince is when the file's first record happened; zero if not
	// known.
	FileSince time.Time
}

// ActivityBatch are the entries recorded after a point, for the live
// feed.
type ActivityBatch struct {
	// Entries are the newest matching entries, newest first.
	Entries []ActivityEntry
	// Next is where the feed continues.
	Next uint64
	// More counts the matching entries not in Entries by action; From and
	// To are when the oldest and newest of them happened.
	More     map[string]int
	From, To time.Time
	// Lost counts the entries no longer kept in memory, matching or not.
	Lost int
}

// ActivitySource reads the node's activity from its audit trail
// (ADR 0025).
type ActivitySource interface {
	// Timeline reads up to limit entries that match f, newest first: the
	// newest if before is empty, else those before the position an
	// earlier page returned as Older.
	Timeline(f ActivityFilter, before string, limit int) (ActivityPage, error)
	// Live reads up to limit entries that match f recorded after the
	// entry numbered after, newest first, and counts the rest.
	Live(f ActivityFilter, after uint64, limit int) ActivityBatch
}
