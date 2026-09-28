package audit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// all matches every record.
func all(*Entry) bool { return true }

// memoryLog returns a Log that keeps its records in memory only.
func memoryLog(t *testing.T) *Log {
	t.Helper()
	l := New("", Options{Mode: func() string { return "observe" }, Now: func() time.Time { return t0 }}, slog.New(slog.DiscardHandler))
	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("Start without a path = %v", err)
	}
	return l
}

// writeN writes n override-removed records, numbered from from, each on
// the address 10.0.0.0 plus its number, so that numbers tells them apart.
func writeN(l *Log, from, n int) {
	for i := range n {
		k := from + i
		l.Write(Record{Action: ActionOverrideRemoved, Indicator: ipv4(fmt.Sprintf("10.%d.%d.%d", k>>16&255, k>>8&255, k&255))})
	}
}

// numbers returns the record numbers written by writeN that entries are,
// in their order.
func numbers(entries []Entry) []int {
	out := make([]int, len(entries))
	for i, e := range entries {
		var a, b, c int
		if _, err := fmt.Sscanf(e.Obie.Indicator, "ipv4:10.%d.%d.%d", &a, &b, &c); err != nil {
			panic(e.Obie.Indicator)
		}
		out[i] = a<<16 | b<<8 | c
	}
	return out
}

// countdown returns from, from-1, ... down to and including to.
func countdown(from, to int) []int {
	var out []int
	for n := from; n >= to; n-- {
		out = append(out, n)
	}
	return out
}

// TestSinceNewestFirst: the live feed returns the records after a number,
// newest first, and continues after the last one.
func TestSinceNewestFirst(t *testing.T) {
	l := memoryLog(t)
	if r := l.Since(0, 50, all); len(r.Entries) != 0 || r.Next != 0 {
		t.Fatalf("empty log: %+v", r)
	}
	writeN(l, 1, 5)
	r := l.Since(0, 50, all)
	if got := numbers(r.Entries); !slices.Equal(got, countdown(5, 1)) || r.Next != 5 || r.More != nil || r.Lost != 0 {
		t.Fatalf("Since(0) = %v, next %d, more %v, lost %d", got, r.Next, r.More, r.Lost)
	}
	writeN(l, 6, 2)
	if r := l.Since(5, 50, all); !slices.Equal(numbers(r.Entries), []int{7, 6}) || r.Next != 7 {
		t.Errorf("Since(5) = %v, next %d", numbers(r.Entries), r.Next)
	}
	if r := l.Since(7, 50, all); len(r.Entries) != 0 || r.Next != 7 {
		t.Errorf("Since(7) = %+v", r)
	}
	// A number from the future, e.g. of an earlier obied, starts at the end.
	if r := l.Since(1000, 50, all); len(r.Entries) != 0 || r.Next != 7 {
		t.Errorf("Since(1000) = %+v", r)
	}
}

// TestSinceSummarizesBursts: beyond the limit, records are counted by
// action with the times they span; records no longer in memory are lost.
func TestSinceSummarizesBursts(t *testing.T) {
	now := t0
	l := New("", Options{Mode: func() string { return "enforce" }, Now: func() time.Time { return now }}, slog.New(slog.DiscardHandler))
	writeN(l, 1, 3) // removed overrides at t0
	now = t0.Add(time.Second)
	for range 4 {
		l.Write(Record{Action: ActionPeerConnected, PeerID: "12D3KooWA"})
	}
	now = t0.Add(2 * time.Second)
	writeN(l, 100, 2)

	r := l.Since(0, 3, all)
	if len(r.Entries) != 3 || r.Entries[0].Event.Action != ActionOverrideRemoved || r.Entries[2].Event.Action != ActionPeerConnected {
		t.Fatalf("entries = %+v", r.Entries)
	}
	want := map[Action]int{ActionPeerConnected: 3, ActionOverrideRemoved: 3}
	if fmt.Sprint(r.More) != fmt.Sprint(want) || !r.From.Equal(t0) || !r.To.Equal(t0.Add(time.Second)) {
		t.Errorf("more = %v from %v to %v; want %v from %v to %v", r.More, r.From, r.To, want, t0, t0.Add(time.Second))
	}

	peers := l.Since(0, 10, func(e *Entry) bool { return e.Event.Action == ActionPeerConnected })
	if len(peers.Entries) != 4 || peers.More != nil {
		t.Errorf("filtered: %d entries, more %v", len(peers.Entries), peers.More)
	}

	writeN(l, 1000, MemoryEntries)
	r = l.Since(2, 1, all)
	if r.Lost != 7 || r.Next != MemoryEntries+9 || len(r.Entries) != 1 {
		t.Errorf("after overflowing memory: lost %d, next %d, %d entries", r.Lost, r.Next, len(r.Entries))
	}
}

// TestMemoryHistory: without a file, pages come from memory, newest
// first, and say that the file is off and when records were forgotten.
func TestMemoryHistory(t *testing.T) {
	l := memoryLog(t)
	writeN(l, 1, 250)
	m := l.Mark()
	if !errors.Is(m.Err, ErrNoFile) || m.Seq != 250 {
		t.Fatalf("mark = %+v", m)
	}
	writeN(l, 251, 5) // after the mark: for the live feed only
	var got []int
	var before *Cursor
	for page := 0; ; page++ {
		p := l.History(m, before, 100, all)
		if !p.Memory || !errors.Is(p.FileErr, ErrNoFile) || p.Forgotten {
			t.Fatalf("page %d: %+v", page, p)
		}
		got = append(got, numbers(p.Entries)...)
		if p.Older == nil {
			break
		}
		if !p.Older.Memory {
			t.Fatalf("memory page continues in the file: %+v", p.Older)
		}
		c, err := ParseCursor(p.Older.String())
		if err != nil || c != *p.Older {
			t.Fatalf("cursor %v does not survive the URL: %v, %v", p.Older, c, err)
		}
		before = &c
	}
	if !slices.Equal(got, countdown(250, 1)) {
		t.Errorf("pages = %v", got)
	}
	if live := l.Since(m.Seq, 50, all); !slices.Equal(numbers(live.Entries), countdown(255, 251)) {
		t.Errorf("after the mark: %v", numbers(live.Entries))
	}

	writeN(l, 1000, MemoryEntries)
	p := l.History(l.Mark(), nil, MemoryEntries+1, all)
	if len(p.Entries) != MemoryEntries || !p.Forgotten || p.Older != nil {
		t.Errorf("after overflowing memory: %d entries, forgotten %v, older %v", len(p.Entries), p.Forgotten, p.Older)
	}
}

// TestFileHistory: pages are read from the file backwards, match its
// lines exactly, stop at the mark and continue where they ended.
func TestFileHistory(t *testing.T) {
	var logs bytes.Buffer
	l, path := startLog(t, "enforce", &logs)
	const n = 1200 // well over one chunk of the file
	writeN(l, 1, n)
	m := l.Mark()
	if m.Err != nil || m.Seq != n {
		t.Fatalf("mark = %+v", m)
	}
	writeN(l, n+1, 3)

	lines := strings.Split(strings.TrimSuffix(readFile(t, path), "\n"), "\n")
	var got []Entry
	var before *Cursor
	for {
		p := l.History(m, before, 100, all)
		if p.Memory || p.FileErr != nil || p.Skipped != 0 || p.Searched {
			t.Fatalf("page: %+v", p)
		}
		got = append(got, p.Entries...)
		if p.Older == nil {
			break
		}
		before = p.Older
	}
	if want := countdown(n, 1); !slices.Equal(numbers(got), want) {
		t.Fatalf("pages = %v", numbers(got))
	}
	if p := l.History(m, nil, 1, all); !p.FileSince.Equal(t0) {
		t.Errorf("file since %v, want %v", p.FileSince, t0)
	}
	for i, e := range got {
		if line, _ := parseEntry([]byte(lines[n-1-i])); !reflect.DeepEqual(line, e) {
			t.Fatalf("entry %d = %+v, line %s", i, e, lines[n-1-i])
		}
	}
}

// TestFileHistorySkipsOtherLines: lines that are no records are skipped
// and counted; empty lines are ignored; a torn last line is skipped.
func TestFileHistorySkipsOtherLines(t *testing.T) {
	var logs bytes.Buffer
	l, path := startLog(t, "enforce", &logs)
	writeN(l, 1, 2)
	appendFile(t, path, "not json\n\n"+`{"event":{"dataset":"other"}}`+"\n"+strings.Repeat("x", maxLine+int(scanChunk))+"\n")
	writeN(l, 3, 1)
	appendFile(t, path, `{"@timestamp":"2026-09-28T12:00:00Z","event":{"dataset":"obie.audit","action":"block-ad`)
	p := l.History(l.Mark(), nil, 100, all)
	if got := numbers(p.Entries); !slices.Equal(got, []int{3, 2, 1}) || p.Skipped != 4 || p.Older != nil {
		t.Errorf("entries %v, skipped %d, older %v", got, p.Skipped, p.Older)
	}
}

// TestFileHistoryFilterAndBudget: a filter is applied while reading, and
// a page that reads ScanBytes without filling up offers to go on.
func TestFileHistoryFilterAndBudget(t *testing.T) {
	var logs bytes.Buffer
	l, _ := startLog(t, "enforce", &logs)
	l.Write(Record{Action: ActionPeerConnected, PeerID: "12D3KooWOld"})
	for l.Mark().size < ScanBytes+scanChunk {
		writeN(l, 1, 1000)
	}
	l.Write(Record{Action: ActionPeerConnected, PeerID: "12D3KooWNew"})
	peers := func(e *Entry) bool { return e.Event.Action == ActionPeerConnected }

	p := l.History(l.Mark(), nil, 10, peers)
	if len(p.Entries) != 1 || p.Entries[0].Obie.PeerID != "12D3KooWNew" || !p.Searched || p.Older == nil ||
		!p.SearchedTo.Equal(t0) {
		t.Fatalf("first page: %d entries, searched %v to %v, older %v", len(p.Entries), p.Searched, p.SearchedTo, p.Older)
	}
	p = l.History(l.Mark(), p.Older, 10, peers)
	if len(p.Entries) != 1 || p.Entries[0].Obie.PeerID != "12D3KooWOld" || p.Searched || p.Older != nil {
		t.Errorf("second page: %+v, searched %v, older %v", p.Entries, p.Searched, p.Older)
	}
}

// TestHistoryAfterReopen: a cursor into a file that was reopened since
// starts again at the newest records, and says so.
func TestHistoryAfterReopen(t *testing.T) {
	var logs bytes.Buffer
	l, path := startLog(t, "enforce", &logs)
	writeN(l, 1, 3)
	p := l.History(l.Mark(), nil, 1, all)
	if p.Older == nil {
		t.Fatal("no older page")
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := l.Reopen(); err != nil {
		t.Fatal(err)
	}
	writeN(l, 4, 1)
	p = l.History(l.Mark(), p.Older, 10, all)
	if !errors.Is(p.FileErr, ErrReopened) || p.Memory || !slices.Equal(numbers(p.Entries), []int{4}) {
		t.Errorf("after reopen: %v, err %v, memory %v", numbers(p.Entries), p.FileErr, p.Memory)
	}
}

// TestReopenOfTheSameFileKeepsPositions: a reload that reopens a file
// logrotate did not move keeps the positions of earlier pages valid.
func TestReopenOfTheSameFileKeepsPositions(t *testing.T) {
	var logs bytes.Buffer
	l, _ := startLog(t, "enforce", &logs)
	writeN(l, 1, 3)
	p := l.History(l.Mark(), nil, 1, all)
	if err := l.Reopen(); err != nil {
		t.Fatal(err)
	}
	writeN(l, 4, 1)
	p = l.History(l.Mark(), p.Older, 10, all)
	if p.FileErr != nil || !slices.Equal(numbers(p.Entries), []int{2, 1}) {
		t.Errorf("after reopening the same file: %v, err %v", numbers(p.Entries), p.FileErr)
	}
}

// TestNotARegularFile: what is written to a device or a pipe, such as
// /dev/stdout, cannot be read back; the history comes from memory.
func TestNotARegularFile(t *testing.T) {
	l := New(os.DevNull, Options{Mode: func() string { return "observe" }}, slog.New(slog.DiscardHandler))
	if err := l.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Stop(context.Background()) })
	writeN(l, 1, 2)
	if p := l.History(l.Mark(), nil, 10, all); !errors.Is(p.FileErr, ErrNotRegular) || !p.Memory || len(p.Entries) != 2 {
		t.Errorf("page = %+v", p)
	}
}

// TestConcurrentWritesReopensAndReads: pages and the live feed read
// while records are written and the file is reopened see every record
// exactly once (run with -race).
func TestConcurrentWritesReopensAndReads(t *testing.T) {
	var logs bytes.Buffer
	l, _ := startLog(t, "enforce", &logs)
	const writers, each = 4, 300
	done := make(chan struct{})
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() { writeN(l, 1+w*each, each) })
	}
	wg.Go(func() {
		for range 20 {
			if err := l.Reopen(); err != nil {
				t.Error(err)
			}
		}
	})
	go func() { wg.Wait(); close(done) }()
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		m := l.Mark()
		seen := map[int]bool{}
		for _, n := range numbers(l.History(m, nil, writers*each, all).Entries) {
			seen[n] = true
		}
		live := numbers(l.Since(m.Seq, writers*each, all).Entries)
		for _, n := range live {
			if seen[n] {
				t.Fatalf("record %d both on the page and in the live feed", n)
			}
			seen[n] = true
		}
		if uint64(len(seen)) != l.Mark().Seq && !reading {
			t.Fatalf("page and live feed hold %d records, %d were written", len(seen), l.Mark().Seq)
		}
	}
}

// TestScanBackReadsEveryLine: with any chunk size, budget and stop, the
// scanner gives every line once, the last first, and resumes where it
// stopped.
func TestScanBackReadsEveryLine(t *testing.T) {
	defer func(chunk int64, long int) { scanChunk, maxLine = chunk, long }(scanChunk, maxLine)
	rng := rand.New(rand.NewPCG(1688, 25)) // #nosec G404 -- a reproducible test input, no secret.
	for round := range 2000 {
		scanChunk, maxLine = int64(1+rng.IntN(16)), 8+rng.IntN(24)
		var text strings.Builder
		var want [][]byte
		for i := range rng.IntN(20) {
			line := strings.Repeat(string(rune('a'+i%26)), rng.IntN(maxLine))
			text.WriteString(line + "\n")
			want = append(want, []byte(line))
		}
		slices.Reverse(want)
		r := strings.NewReader(text.String())
		stopEvery := 1 + rng.IntN(5)
		var got [][]byte
		for end := int64(text.Len()); end > 0; {
			n := 0
			next, err := scanBack(r, end, int64(maxLine)+scanChunk*2+int64(rng.IntN(64)), func(line []byte) bool {
				if n == stopEvery {
					return false
				}
				n++
				got = append(got, slices.Clone(line))
				return true
			})
			if err != nil || next >= end {
				t.Fatalf("round %d: scanBack(%d) = %d, %v", round, end, next, err)
			}
			end = next
		}
		if !slices.EqualFunc(got, want, bytes.Equal) {
			t.Fatalf("round %d (chunk %d, max line %d): got %q, want %q", round, scanChunk, maxLine, got, want)
		}
	}
}

// TestScanBackSkipsALineOverTheBudget: a line longer than what one call
// may read is reported as skipped, and the scan goes on before the part
// it read.
func TestScanBackSkipsALineOverTheBudget(t *testing.T) {
	defer func(chunk int64) { scanChunk = chunk }(scanChunk)
	scanChunk = 4
	text := "ok\n" + strings.Repeat("x", 30) + "\n"
	var lines [][]byte
	next, err := scanBack(strings.NewReader(text), int64(len(text)), 8, func(line []byte) bool {
		lines = append(lines, line)
		return true
	})
	if err != nil || next != int64(len(text))-8 || len(lines) != 1 || lines[0] != nil {
		t.Errorf("scanBack = %d, %v, lines %q", next, err, lines)
	}
}

// TestWriteOnlyFile: a file obied may only write is written as before;
// the history then comes from memory and says why.
func TestWriteOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(path, nil, 0o200); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	l := New(path, Options{Mode: func() string { return "observe" }, Now: func() time.Time { return t0 }},
		slog.New(slog.NewJSONHandler(&logs, nil)))
	if err := l.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Stop(context.Background()) })
	writeN(l, 1, 2)
	p := l.History(l.Mark(), nil, 10, all)
	if !errors.Is(p.FileErr, ErrWriteOnly) || !p.Memory || !slices.Equal(numbers(p.Entries), []int{2, 1}) {
		t.Errorf("page: %v, err %v", numbers(p.Entries), p.FileErr)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); strings.Count(got, "\n") != 2 {
		t.Errorf("file:\n%s", got)
	}
	if !strings.Contains(logs.String(), `"msg":"the console cannot show the audit log's history"`) {
		t.Errorf("not logged: %s", logs.String())
	}
}

// TestHistoryAfterStop: once the file is closed, e.g. while the node
// shuts down, the history comes from memory.
func TestHistoryAfterStop(t *testing.T) {
	var logs bytes.Buffer
	l, _ := startLog(t, "observe", &logs)
	writeN(l, 1, 2)
	if err := l.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p := l.History(l.Mark(), nil, 10, all); !errors.Is(p.FileErr, ErrNotOpen) || len(p.Entries) != 2 {
		t.Errorf("page: %+v", p)
	}
}

// TestHistoryOfTruncatedFile: a file that became shorter than the mark is
// read from memory, with the reason.
func TestHistoryOfTruncatedFile(t *testing.T) {
	var logs bytes.Buffer
	l, path := startLog(t, "observe", &logs)
	writeN(l, 1, 2)
	m := l.Mark()
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	if p := l.History(m, nil, 10, all); !p.Memory || p.FileErr == nil || !strings.Contains(p.FileErr.Error(), "shorter") ||
		len(p.Entries) != 2 {
		t.Errorf("page: %+v", p)
	}
}

func TestParseCursor(t *testing.T) {
	for _, c := range []Cursor{{Memory: true, At: 1}, {Gen: 3, At: 12345}} {
		if got, err := ParseCursor(c.String()); err != nil || got != c {
			t.Errorf("ParseCursor(%q) = %+v, %v", c.String(), got, err)
		}
	}
	for _, s := range []string{"", "x1", "m", "m0", "m-2", "f1", "f1-0", "fa-1", "f1-b", "m1.5"} {
		if _, err := ParseCursor(s); err == nil {
			t.Errorf("ParseCursor(%q) accepted", s)
		}
	}
}

func TestNilLogReadsNothing(t *testing.T) {
	var l *Log
	if m := l.Mark(); !errors.Is(m.Err, ErrNoFile) {
		t.Errorf("Mark = %+v", m)
	}
	if r := l.Since(0, 10, all); len(r.Entries) != 0 {
		t.Errorf("Since = %+v", r)
	}
	if p := l.History(Mark{}, nil, 10, all); len(p.Entries) != 0 || !p.Memory {
		t.Errorf("History = %+v", p)
	}
	if l.Path() != "" {
		t.Error("nil log has a path")
	}
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}
