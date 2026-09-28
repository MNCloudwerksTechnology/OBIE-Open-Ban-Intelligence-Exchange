package audit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// MemoryEntries is how many of the last records a Log keeps in memory
// (ADR 0025).
const MemoryEntries = 10_000

// ScanBytes bounds how much of the file one History call reads.
const ScanBytes = 16 << 20

// Why the file cannot be read back.
var (
	// ErrNoFile: the records are kept in memory only (no audit.path).
	ErrNoFile = errors.New("the audit log is off")
	// ErrWriteOnly: the file could be opened for writing only.
	ErrWriteOnly = errors.New("obied may write the audit log file but not read it")
	// ErrNotRegular: the path is no regular file, e.g. /dev/stdout, so
	// what was written to it cannot be read back.
	ErrNotRegular = errors.New("the audit log is not a regular file, so what was written to it cannot be read back")
	// ErrNotOpen: the file is not open, before Start or after Stop.
	ErrNotOpen = errors.New("the audit log file is not open")
	// ErrReopened: a cursor points into a file that was reopened since,
	// e.g. by logrotate.
	ErrReopened = errors.New("the audit log file was reopened since this page was read")
)

// keep adds e to the records in memory. The caller holds mu.
func (l *Log) keep(e *Entry) {
	l.seq++
	l.tail[(l.seq-1)%MemoryEntries] = e
}

// kept returns the records in memory numbered from and above, newest
// first, and how many of the records numbered above after are no longer
// kept. The caller holds mu.
func (l *Log) kept(after uint64) (entries []*Entry, lost int) {
	oldest := uint64(1)
	if l.seq > MemoryEntries {
		oldest = l.seq - MemoryEntries + 1
	}
	from := after + 1
	if from < oldest {
		lost, from = int(oldest-from), oldest // #nosec G115 -- at most l.seq.
	}
	for n := l.seq; n >= from && n > 0; n-- {
		entries = append(entries, l.tail[(n-1)%MemoryEntries])
	}
	return entries, lost
}

// Mark is a point in the audit trail: the last record written, and the
// file as it was then, so that reading the file up to the mark and the
// records after it in memory neither misses nor repeats a record.
type Mark struct {
	// Seq is the number of the last record; 0 before the first.
	Seq uint64
	// Err is why the file cannot be read back; nil if it can.
	Err  error
	f    *os.File
	gen  uint64
	size int64
}

// Mark returns the current point in the audit trail.
func (l *Log) Mark() Mark {
	if l == nil {
		return Mark{Err: ErrNoFile}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	m := Mark{Seq: l.seq, gen: l.gen}
	switch {
	case l.path == "":
		m.Err = ErrNoFile
	case l.f == nil:
		m.Err = ErrNotOpen
	case l.readErr != nil:
		m.Err = l.readErr
	default:
		info, err := l.f.Stat()
		if err != nil {
			m.Err = fmt.Errorf("the audit log file cannot be read: %w", err)
			break
		}
		m.f, m.size = l.f, info.Size()
	}
	return m
}

// Recent are the records written after a sequence number, for the live
// feed.
type Recent struct {
	// Entries are the newest matching records, newest first.
	Entries []Entry
	// Next is the number of the last record: where the feed continues.
	Next uint64
	// More counts the matching records not in Entries, by action; From and
	// To are the times of the oldest and newest of them.
	More     map[Action]int
	From, To time.Time
	// Lost counts the records that were no longer kept in memory, matching
	// or not.
	Lost int
}

// Since returns up to limit of the records written after the record
// numbered after that match, newest first, and counts the rest. A number
// the log has not reached yet counts as its last record.
func (l *Log) Since(after uint64, limit int, match func(*Entry) bool) Recent {
	if l == nil {
		return Recent{}
	}
	l.mu.Lock()
	next := l.seq
	after = min(after, next)
	entries, lost := l.kept(after)
	l.mu.Unlock()
	r := Recent{Next: next, Lost: lost}
	for _, e := range entries {
		if !match(e) {
			continue
		}
		if len(r.Entries) < limit {
			r.Entries = append(r.Entries, *e)
			continue
		}
		if r.More == nil {
			r.More = map[Action]int{}
			r.To = e.Time()
		}
		r.More[e.Event.Action]++
		r.From = e.Time()
	}
	return r
}

// Cursor is where a page of the audit trail ends and the next older one
// starts: a byte offset in the file (of a generation, counting reopens),
// or a record number in memory.
type Cursor struct {
	Memory bool
	Gen    uint64
	At     int64
}

// String returns c in the form ParseCursor reads: "f<gen>-<offset>" or
// "m<number>".
func (c Cursor) String() string {
	if c.Memory {
		return "m" + strconv.FormatInt(c.At, 10)
	}
	return "f" + strconv.FormatUint(c.Gen, 10) + "-" + strconv.FormatInt(c.At, 10)
}

// ParseCursor reads a cursor that String returned.
func ParseCursor(s string) (Cursor, error) {
	bad := fmt.Errorf("%q is not a position in the audit trail", s)
	switch {
	case strings.HasPrefix(s, "m"):
		at, err := strconv.ParseInt(s[1:], 10, 64)
		if err != nil || at < 1 {
			return Cursor{}, bad
		}
		return Cursor{Memory: true, At: at}, nil
	case strings.HasPrefix(s, "f"):
		gen, off, ok := strings.Cut(s[1:], "-")
		g, err1 := strconv.ParseUint(gen, 10, 64)
		at, err2 := strconv.ParseInt(off, 10, 64)
		if !ok || err1 != nil || err2 != nil || at < 1 {
			return Cursor{}, bad
		}
		return Cursor{Gen: g, At: at}, nil
	default:
		return Cursor{}, bad
	}
}

// Page is a page of the audit trail, newest first.
type Page struct {
	Entries []Entry
	// Memory is set if the page comes from the records kept in memory,
	// because the file cannot be read back (FileErr says why) or the
	// cursor pointed there.
	Memory  bool
	FileErr error
	// Older is where the next older page starts; nil at the start of the
	// file, or of the records kept in memory.
	Older *Cursor
	// Searched is set if the page ends because ScanBytes of the file were
	// read; SearchedTo is the time of the oldest record read then.
	Searched   bool
	SearchedTo time.Time
	// Skipped counts the lines of the file that are no audit records.
	Skipped int
	// FileSince is the time of the first record of the file; zero for a
	// page from memory, or if the file's first line is no record.
	FileSince time.Time
	// Forgotten is set if older records than those kept in memory were
	// written, so a page from memory does not reach the first record.
	Forgotten bool
}

// History returns up to limit records that match, newest first: from the
// file if it can be read back, else from memory. The first page (before
// nil) starts at the mark m; before continues an earlier page.
func (l *Log) History(m Mark, before *Cursor, limit int, match func(*Entry) bool) Page {
	if l == nil {
		return Page{Memory: true, FileErr: ErrNoFile}
	}
	switch {
	case before != nil && before.Memory:
		p := l.memoryPage(uint64(before.At), limit, match) // #nosec G115 -- ParseCursor refuses < 1.
		p.FileErr = m.Err
		return p
	case m.Err != nil:
		p := l.memoryPage(m.Seq+1, limit, match)
		p.FileErr = m.Err
		return p
	}
	end := m.size
	var err error
	if before != nil {
		if before.Gen != m.gen {
			err = ErrReopened
		} else {
			end = min(before.At, m.size)
		}
	}
	p := l.filePage(m, end, limit, match)
	if p.FileErr == nil {
		p.FileErr = err
	}
	return p
}

// memoryPage returns up to limit matching records kept in memory that are
// numbered below before, newest first.
func (l *Log) memoryPage(before uint64, limit int, match func(*Entry) bool) Page {
	l.mu.Lock()
	entries, _ := l.kept(0)
	forgotten := l.seq > MemoryEntries
	next := l.seq
	l.mu.Unlock()
	p := Page{Memory: true}
	for i, e := range entries {
		n := next - uint64(i) // #nosec G115 -- i < len(entries) <= next.
		if n >= before || !match(e) {
			continue
		}
		if len(p.Entries) == limit {
			p.Older = &Cursor{Memory: true, At: int64(n + 1)} // #nosec G115 -- record numbers fit.
			return p
		}
		p.Entries = append(p.Entries, *e)
	}
	p.Forgotten = forgotten
	return p
}

// filePage returns up to limit matching records of the file that end at
// or before the offset end, newest first.
func (l *Log) filePage(m Mark, end int64, limit int, match func(*Entry) bool) Page {
	var p Page
	next, err := scanBack(m.f, end, ScanBytes, func(line []byte) bool {
		if line != nil && len(bytes.TrimSpace(line)) == 0 {
			return true // an empty line
		}
		e, ok := parseEntry(line)
		if !ok {
			p.Skipped++
			return true
		}
		p.SearchedTo = e.Time()
		if !match(&e) {
			return true
		}
		if len(p.Entries) == limit {
			return false
		}
		p.Entries = append(p.Entries, e)
		return true
	})
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			err = errors.New("it became shorter, e.g. truncated by logrotate's copytruncate")
		}
		p := l.memoryPage(m.Seq+1, limit, match)
		p.FileErr = fmt.Errorf("the audit log file cannot be read: %w", err)
		return p
	}
	if next > 0 {
		p.Older = &Cursor{Gen: m.gen, At: next}
		p.Searched = len(p.Entries) < limit
	}
	p.FileSince = firstTime(m.f, m.size)
	return p
}

// firstTime returns the time of the record on the first line of the file
// r of the given size; zero if that line is no record, or longer than a
// scan's chunk.
func firstTime(r io.ReaderAt, size int64) time.Time {
	head := make([]byte, min(size, scanChunk))
	if _, err := r.ReadAt(head, 0); err != nil {
		return time.Time{}
	}
	line, _, ok := bytes.Cut(head, []byte{'\n'})
	if !ok {
		return time.Time{}
	}
	e, ok := parseEntry(line)
	if !ok {
		return time.Time{}
	}
	return e.Time()
}
