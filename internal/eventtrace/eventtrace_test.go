package eventtrace

import (
	"bytes"
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
	if _, err := Read(strings.NewReader(`{"node":"a"}` + "\nnot json\n")); err == nil || !strings.Contains(err.Error(), "line 2") {
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
