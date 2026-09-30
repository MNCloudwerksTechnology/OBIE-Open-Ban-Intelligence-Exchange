package eventtrace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// TestWriterAppendsRecords: every record is one JSON line with the node's
// peer ID, also when many goroutines write at once; a nil writer does
// nothing.
func TestWriterAppendsRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := os.WriteFile(path, []byte(`{"node":"old","event":"e0","from":"x","at":"2026-09-30T11:00:00Z","outcome":"accepted"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := Open(path, "node-a", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	w.Write("e1", "node-a", t0.In(time.FixedZone("x", 3600)), Published)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { w.Write("e2", "node-b", t0.Add(1500*time.Microsecond), Duplicate) })
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w.Write("e3", "node-b", t0, Accepted) // after Close: dropped

	data, err := os.ReadFile(path) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"node":"node-a","event":"e1","from":"node-a","at":"2026-09-30T12:00:00Z","outcome":"published"}`; !strings.Contains(string(data), "\n"+want+"\n") {
		t.Errorf("trace lacks the line %s:\n%s", want, data)
	}
	recs, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 52 || recs[0].Node != "old" {
		t.Fatalf("%d records, want the earlier one and 51 appended", len(recs))
	}
	for _, r := range recs[2:] {
		if r != (Record{Node: "node-a", Event: "e2", From: "node-b", At: t0.Add(1500 * time.Microsecond), Outcome: Duplicate}) {
			t.Errorf("record = %+v", r)
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("an existing file's mode changed: %v, %v", fi.Mode(), err)
	}

	var none *Writer
	none.Write("e1", "x", t0, Accepted)
	if err := none.Close(); err != nil {
		t.Errorf("Close of a nil writer = %v", err)
	}
}

func TestOpenCreatesTheFileOrFails(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(filepath.Join(dir, "new.jsonl"), "n", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if fi, err := os.Stat(filepath.Join(dir, "new.jsonl")); err != nil || fi.Mode().Perm() != fileMode {
		t.Errorf("new trace file: %v, %v; want mode %v", fi, err, os.FileMode(fileMode))
	}
	if _, err := Open(filepath.Join(dir, "missing", "trace.jsonl"), "n", nil); err == nil {
		t.Error("Open in a missing directory succeeded")
	}
}

func TestReadRejectsBadLines(t *testing.T) {
	recs, err := Read(strings.NewReader("\n" + `{"node":"a","event":"e","from":"b","at":"2026-09-30T12:00:00Z","outcome":"accepted"}` + "\n\n"))
	if err != nil || len(recs) != 1 {
		t.Errorf("blank lines: %v, %v", recs, err)
	}
	if _, err := Read(strings.NewReader(`{"node":"a"}` + "\n" + `{"node":"a","at":"yesterday"}` + "\n")); err == nil ||
		!strings.Contains(err.Error(), "line 2") {
		t.Errorf("bad line: %v, want an error naming line 2", err)
	}
	if _, err := ReadFiles(filepath.Join(t.TempDir(), "none.jsonl")); err == nil {
		t.Error("ReadFiles of a missing file succeeded")
	}
}

// rec is a record of node about event e at t0 plus ms milliseconds.
func rec(node, e, from string, ms int, outcome string) Record {
	return Record{Node: node, Event: e, From: from, At: t0.Add(time.Duration(ms) * time.Millisecond), Outcome: outcome}
}

// TestJoinComputesHopsAndPaths: in the mesh A–B–C–D plus A–C, an event
// published by A reaches B and C in one hop and D in two through C; each
// node's copies are counted; a node that got it only from a peer without
// a trace has no path.
func TestJoinComputesHopsAndPaths(t *testing.T) {
	records := []Record{
		rec("A", "e1", "A", 0, Published),
		rec("B", "e1", "A", 12, Accepted),
		rec("C", "e1", "A", 15, Accepted),
		rec("C", "e1", "B", 30, Duplicate),
		rec("B", "e1", "C", 31, Duplicate),
		rec("D", "e1", "C", 40, Accepted),
		rec("D", "e1", "B", 45, "queue_full"),
		rec("E", "e1", "X", 50, Accepted), // X wrote no trace
		// e2: no publication traced; e0 sorts first.
		rec("B", "e2", "A", 5, Accepted),
		rec("C", "e0", "B", 5, "validation_failed"),
	}
	spreads := Join(records)
	if len(spreads) != 3 || spreads[0].Event != "e0" || spreads[1].Event != "e1" || spreads[2].Event != "e2" {
		t.Fatalf("spreads = %+v, want e0, e1, e2", spreads)
	}
	s := spreads[1]
	if s.Origin != "A" || !s.PublishedAt.Equal(t0) {
		t.Errorf("origin = %q at %v", s.Origin, s.PublishedAt)
	}
	want := map[string]Receipt{
		"B": {From: "A", At: t0.Add(12 * time.Millisecond), Hops: 1, Path: []string{"A", "B"}, Delay: 12 * time.Millisecond},
		"C": {From: "A", At: t0.Add(15 * time.Millisecond), Hops: 1, Path: []string{"A", "C"}, Delay: 15 * time.Millisecond},
		"D": {From: "C", At: t0.Add(40 * time.Millisecond), Hops: 2, Path: []string{"A", "C", "D"}, Delay: 40 * time.Millisecond},
		"E": {From: "X", At: t0.Add(50 * time.Millisecond), Hops: -1, Delay: 50 * time.Millisecond},
	}
	if !reflect.DeepEqual(s.Reached, want) {
		t.Errorf("reached = %+v\nwant %+v", s.Reached, want)
	}
	if wantCopies := map[string]int{"B": 2, "C": 2, "D": 2, "E": 1}; !reflect.DeepEqual(s.Copies, wantCopies) {
		t.Errorf("copies = %v, want %v", s.Copies, wantCopies)
	}

	if e2 := spreads[2]; e2.Origin != "" || e2.Reached["B"].Hops != -1 || e2.Reached["B"].Delay != 0 {
		t.Errorf("an event without a traced publication: %+v", e2)
	}
	if e0 := spreads[0]; len(e0.Reached) != 0 || e0.Copies["C"] != 1 {
		t.Errorf("a rejected event: %+v", e0)
	}
}

// TestJoinSurvivesCycles: forwarding peers that point at each other, as
// corrupt or merged traces might, yield no path instead of looping.
func TestJoinSurvivesCycles(t *testing.T) {
	spreads := Join([]Record{
		rec("A", "e", "A", 0, Published),
		rec("B", "e", "C", 10, Accepted),
		rec("C", "e", "B", 11, Accepted),
		rec("D", "e", "A", 12, Accepted),
		rec("D", "e", "A", 5, Accepted), // the earlier acceptance counts
	})
	r := spreads[0].Reached
	if r["B"].Hops != -1 || r["C"].Hops != -1 || r["D"].Hops != 1 || !r["D"].At.Equal(t0.Add(5*time.Millisecond)) {
		t.Errorf("reached = %+v", r)
	}
}

func TestReadFilesJoinsNodes(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, node := range []string{"A", "B"} {
		path := filepath.Join(dir, node+".jsonl")
		w, err := Open(path, node, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if node == "A" {
			w.Write("e", "A", t0, Published)
		} else {
			w.Write("e", "A", t0.Add(time.Millisecond), Accepted)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	recs, err := ReadFiles(paths...)
	if err != nil {
		t.Fatal(err)
	}
	if s := Join(recs); len(s) != 1 || s[0].Reached["B"].Hops != 1 || s[0].Reached["B"].Delay != time.Millisecond {
		t.Errorf("joined = %+v", s)
	}
}

// TestWriterFlushesWhileOpen: records reach the file within about a
// second without a Close, and closing twice at once is safe.
func TestWriterFlushesWhileOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	w, err := Open(path, "node-a", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.Write("e1", "node-b", t0, Accepted)
	deadline := time.Now().Add(5 * time.Second)
	for {
		recs, err := ReadFiles(path)
		if err == nil && len(recs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the record did not reach the file: %v, %v", recs, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if err := w.Close(); err != nil {
				t.Errorf("Close = %v", err)
			}
		})
	}
	wg.Wait()
}

// TestReadSkipsLinesCutShort: a line a crash or a failed write cut short
// is skipped, the records around it are read.
func TestReadSkipsLinesCutShort(t *testing.T) {
	full := `{"node":"a","event":"e","from":"b","at":"2026-09-30T12:00:00Z","outcome":"accepted"}`
	recs, err := Read(strings.NewReader(full + "\n" + full[:40] + "\n" + full + "\n" + full[:30]))
	if err != nil || len(recs) != 2 {
		t.Errorf("Read = %d records, %v; want the 2 whole ones", len(recs), err)
	}

	// A remote peer chooses the message ID of an invalid message; a line
	// cut right after a "}" in it is skipped too.
	braces := `{"node":"a","event":"}}}}","from":"b","at":"2026-09-30T12:00:00Z","outcome":"invalid_schema"}`
	cut := braces[:strings.Index(braces, "}")+1]
	recs, err = Read(strings.NewReader(braces + "\n" + cut + "\n" + full + "\n"))
	if err != nil || len(recs) != 2 || recs[0].Event != "}}}}" || recs[1].Event != "e" {
		t.Errorf("Read = %+v, %v; want the 2 whole records around %s", recs, err, cut)
	}
}

// sink is a trace file that can fail once, or block, for the tests.
type sink struct {
	mu     sync.Mutex
	data   bytes.Buffer
	writes int
	// failAt makes the write with that number (from 1) fail after all but
	// its last 10 bytes; release, while open, blocks every write.
	failAt  int
	release chan struct{}
	closed  bool
}

func (s *sink) Write(p []byte) (int, error) {
	if s.release != nil {
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	s.writes++
	if s.writes == s.failAt {
		n := max(len(p)-10, 0)
		s.data.Write(p[:n])
		return n, io.ErrShortWrite
	}
	return s.data.Write(p)
}

func (s *sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *sink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.String()
}

// countingHandler counts the log records of each message.
type countingHandler struct {
	mu     sync.Mutex
	counts map[string]int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *countingHandler) WithGroup(string) slog.Handler            { return h }
func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.counts == nil {
		h.counts = map[string]int{}
	}
	h.counts[r.Message]++
	return nil
}

func (h *countingHandler) count(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.counts[msg]
}

// TestWriterNeverSplitsLines: while many goroutines write far more than a
// batch, the file only ever holds whole lines, and every record arrives.
func TestWriterNeverSplitsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	w, err := Open(path, "node-a", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	const writers, each = 8, 250 // about 460 KB, seven batches
	var wg sync.WaitGroup
	for g := range writers {
		wg.Go(func() {
			for i := range each {
				w.Write(fmt.Sprintf("e-%d-%d", g, i), "node-b", t0, Duplicate)
			}
		})
	}
	stopWatching := make(chan struct{})
	watched := make(chan error, 1)
	go func() {
		for {
			data, err := os.ReadFile(path) // #nosec G304 -- test file.
			if err == nil && len(data) > 0 && data[len(data)-1] != '\n' {
				watched <- fmt.Errorf("the file ends in half a line after %d bytes", len(data))
				return
			}
			select {
			case <-stopWatching:
				watched <- nil
				return
			default:
			}
		}
	}()
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	close(stopWatching)
	if err := <-watched; err != nil {
		t.Error(err)
	}
	recs, err := ReadFiles(path)
	if err != nil || len(recs) != writers*each {
		t.Errorf("read %d records, %v; want %d", len(recs), err, writers*each)
	}
}

// TestWriterSurvivesAFailedWrite: a write that fails in the middle of a
// line loses the rest of its batch and is logged once; the next batch
// starts on a line of its own, so the file stays readable and tracing
// goes on.
func TestWriterSurvivesAFailedWrite(t *testing.T) {
	out := &sink{failAt: 1}
	logs := &countingHandler{}
	w := start(out, "trace.jsonl", "node-a", slog.New(logs), false, time.Hour) // written only when kicked
	t.Cleanup(func() { _ = w.Close() })
	w.Write("lost-1", "node-b", t0, Accepted)
	w.Write("lost-2", "node-b", t0, Accepted)
	w.kick <- struct{}{} // both in one batch
	waitFor(t, "the failed write", func() bool { return logs.count("writing the event trace failed; records are lost") == 1 })
	w.Write("kept", "node-b", t0, Accepted)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	recs, err := Read(strings.NewReader(out.String()))
	if err != nil || len(recs) != 2 || recs[0].Event != "lost-1" || recs[1].Event != "kept" {
		t.Errorf("records after the failure = %+v, %v; want the one written whole and the later one", recs, err)
	}
	if n := logs.count("writing the event trace failed; records are lost"); n != 1 {
		t.Errorf("the failure was logged %d times, want once", n)
	}
}

// TestOpenStartsAfterALineCutShort: a trace a crash left in the middle of
// a line gets the next record on a line of its own.
func TestOpenStartsAfterALineCutShort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := os.WriteFile(path, []byte(`{"node":"a","event":"e0","from":"b","at":"2026-09-30T12:00:00Z","outcome":"accepted"}`+"\n"+`{"node":"a","ev`), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := Open(path, "node-a", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	w.Write("e1", "node-b", t0, Accepted)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadFiles(path)
	if err != nil || len(recs) != 2 || recs[0].Event != "e0" || recs[1].Event != "e1" {
		t.Errorf("records = %+v, %v; want e0 and e1", recs, err)
	}
}

// TestWriterDropsWhenFallingBehind: while the disk does not keep up, the
// records beyond the bound are dropped, with one warning, and memory stays
// bounded; once it keeps up, records are kept again.
func TestWriterDropsWhenFallingBehind(t *testing.T) {
	out := &sink{release: make(chan struct{})}
	logs := &countingHandler{}
	w := start(out, "trace.jsonl", "node-a", slog.New(logs), false, time.Hour) // no write makes room
	w.maxPending = 4 * 1024
	for i := range 100 { // about 10 KiB
		w.Write(fmt.Sprintf("e%d", i), "node-b", t0, Accepted)
	}
	w.mu.Lock()
	pending := len(w.pending)
	w.mu.Unlock()
	if pending >= w.maxPending {
		t.Errorf("%d bytes wait, want fewer than %d", pending, w.maxPending)
	}
	if n := logs.count("the event trace falls behind the disk; records are lost"); n != 1 {
		t.Errorf("warned %d times, want once", n)
	}
	close(out.release)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	recs, err := Read(strings.NewReader(out.String()))
	if err != nil || len(recs) == 0 || len(recs) >= 100 {
		t.Errorf("kept %d records, %v; want some, not all", len(recs), err)
	}
}

// TestCloseRacesWrites: writes racing Close neither panic nor break a
// line, and every Close returns once the file is closed.
func TestCloseRacesWrites(t *testing.T) {
	out := &sink{}
	w := start(out, "trace.jsonl", "node-a", slog.New(slog.DiscardHandler), false, flushInterval)
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range 200 {
				w.Write(fmt.Sprintf("e-%d-%d", g, i), "node-b", t0, Accepted)
			}
		})
	}
	for range 3 {
		wg.Go(func() {
			if err := w.Close(); err != nil {
				t.Errorf("Close = %v", err)
			}
			out.mu.Lock()
			closed := out.closed
			out.mu.Unlock()
			if !closed {
				t.Error("Close returned before the file was closed")
			}
		})
	}
	wg.Wait()
	if _, err := Read(strings.NewReader(out.String())); err != nil || !strings.HasSuffix(out.String(), "\n") && out.String() != "" {
		t.Errorf("the file holds a broken line: %v", err)
	}
}

// waitFor waits up to 5 seconds for cond.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
