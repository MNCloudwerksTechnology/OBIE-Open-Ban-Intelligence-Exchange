// Package eventtrace writes, reads and joins the per-event trace of
// ADR 0032: with mesh.trace_path set, a node appends one JSON line for
// every copy of an event it receives and every event it publishes. Joined
// across the nodes of a mesh, the lines show the hops and the path each
// event took to each node; the simulation harness computes hop counts,
// delays and duplicate factors from them.
package eventtrace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Outcomes of a line. A received copy has the outcome of the gossip
// validator (internal/gossip), e.g. Accepted, or a reason GossipSub
// dropped it before validation: Duplicate for a copy of an event the node
// had seen, or a reject reason of obie_gossip_rejects_total.
const (
	// Published: the node published the event itself.
	Published = "published"
	// Accepted: the node accepted the copy, the first valid one it got.
	Accepted = "accepted"
	// Duplicate: the node had seen the event before.
	Duplicate = "duplicate"
)

// fileMode is the mode of a new trace file, as of the audit log.
const fileMode = 0o640

// Records are batched in memory, so that GossipSub's event loop, which
// traces the copies it drops, never waits for the disk: a goroutine writes
// the batch, whole lines only, every flushInterval, once it holds
// batchSize bytes, and on Close. Beyond maxPending bytes waiting, records
// are dropped rather than let memory grow without bound.
const (
	flushInterval = time.Second
	batchSize     = 64 * 1024
	maxPending    = 16 * 1024 * 1024
)

// Record is one line of a trace file.
type Record struct {
	// Node is the peer ID of the node that wrote the line.
	Node string `json:"node"`
	// Event is the message ID of the copy: the event ID of an event.
	Event string `json:"event"`
	// From is the peer that forwarded the copy; the node itself for an
	// event it published.
	From string `json:"from"`
	// At is when the node received the copy or published the event.
	At time.Time `json:"at"`
	// Outcome is what became of it.
	Outcome string `json:"outcome"`
}

// Writer appends the records of one node to a trace file. It is safe for
// concurrent use, and the methods of a nil *Writer do nothing, so callers
// need not check whether tracing is on.
type Writer struct {
	node string
	log  *slog.Logger
	path string
	// out is the file; only the flushing goroutine writes to it, and Close
	// closes it once that goroutine is done.
	out io.WriteCloser
	// kick asks the flushing goroutine to write at once; stop ends it, and
	// it closes done. closed is closed once Close has closed the file.
	kick         chan struct{}
	stop, done   chan struct{}
	closed       chan struct{}
	closeErr     error
	midLine      bool // owned by the flushing goroutine
	failedLogged bool // owned by the flushing goroutine

	// maxPending bounds the bytes waiting to be written.
	maxPending int

	mu sync.Mutex
	// pending holds the complete lines not written yet, and spare the
	// buffer the last batch was written from, for reuse.
	pending, spare []byte
	// dropping is set while records are dropped for want of room, closing
	// once Close began.
	dropping, closing bool
}

// Open opens the trace file at path for appending the records of the node
// with peer ID node, creating it if needed; its directory must exist.
// Close writes the records still waiting and closes it.
func Open(path, node string, log *slog.Logger) (*Writer, error) {
	midLine := endsMidLine(path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, fileMode) // #nosec G304 -- the operator chooses mesh.trace_path.
	if err != nil {
		return nil, fmt.Errorf("open event trace: %w", err)
	}
	return start(f, path, node, log, midLine), nil
}

// start returns a writer of node's records to out and starts its flushing
// goroutine; midLine says that out ends in a line cut short.
func start(out io.WriteCloser, path, node string, log *slog.Logger, midLine bool) *Writer {
	w := &Writer{node: node, log: log, path: path, out: out, midLine: midLine, maxPending: maxPending,
		kick: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), closed: make(chan struct{})}
	go w.flush()
	return w
}

// endsMidLine reports whether the file at path ends in a line a crash or a
// failed write cut short, so that the next record starts on a line of its
// own.
func endsMidLine(path string) bool {
	f, err := os.Open(path) // #nosec G304 -- the operator chooses mesh.trace_path.
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false
	}
	var last [1]byte
	_, err = f.ReadAt(last[:], info.Size()-1)
	return err == nil && last[0] != '\n'
}

// Write appends the record of a copy of the event with ID event that
// peer from forwarded, or the node published, at at with outcome. It never
// waits for the disk; the record reaches the file within flushInterval.
func (w *Writer) Write(event, from string, at time.Time, outcome string) {
	if w == nil {
		return
	}
	line, err := json.Marshal(Record{Node: w.node, Event: event, From: from, At: at.UTC(), Outcome: outcome})
	if err != nil {
		return // a Record always encodes
	}
	w.mu.Lock()
	if w.closing {
		w.mu.Unlock()
		return
	}
	if len(w.pending)+len(line) >= w.maxPending {
		if !w.dropping {
			w.dropping = true
			w.log.Warn("the event trace falls behind the disk; records are lost", "path", w.path)
		}
		w.mu.Unlock()
		return
	}
	w.dropping = false
	w.pending = append(append(w.pending, line...), '\n')
	full := len(w.pending) >= batchSize
	w.mu.Unlock()
	if full {
		select {
		case w.kick <- struct{}{}:
		default: // a write is pending
		}
	}
}

// flush writes the waiting records every flushInterval and when kicked,
// and a last time when stopped.
func (w *Writer) flush() {
	defer close(w.done)
	tick := time.NewTicker(flushInterval)
	defer tick.Stop()
	for {
		select {
		case <-w.stop:
			w.writeBatch()
			return
		case <-w.kick:
		case <-tick.C:
		}
		w.writeBatch()
	}
}

// writeBatch writes the waiting records, whole lines only. A failed write
// loses them, is logged once until a write succeeds again, and makes the
// next batch start on a line of its own.
func (w *Writer) writeBatch() {
	w.mu.Lock()
	batch := w.pending
	w.pending, w.spare = w.spare[:0], nil
	w.mu.Unlock()
	if len(batch) == 0 {
		w.recycle(batch)
		return
	}
	if w.midLine {
		batch = append([]byte{'\n'}, batch...)
	}
	n, err := w.out.Write(batch)
	switch {
	case err != nil:
		if n > 0 {
			w.midLine = batch[n-1] != '\n'
		}
		if !w.failedLogged {
			w.failedLogged = true
			w.log.Error("writing the event trace failed; records are lost", "path", w.path, "error", err)
		}
	default:
		w.midLine, w.failedLogged = false, false
	}
	w.recycle(batch)
}

// recycle keeps the buffer of a written batch for the next one, unless it
// grew beyond a few batches.
func (w *Writer) recycle(batch []byte) {
	if cap(batch) > 4*batchSize {
		return
	}
	w.mu.Lock()
	w.spare = batch[:0]
	w.mu.Unlock()
}

// Close writes the waiting records and closes the file; later records are
// dropped. Every call returns once the file is closed.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closing {
		w.mu.Unlock()
		<-w.closed
		return nil
	}
	w.closing = true
	w.mu.Unlock()
	close(w.stop)
	<-w.done
	if err := w.out.Close(); err != nil {
		w.closeErr = fmt.Errorf("close event trace: %w", err)
	}
	close(w.closed)
	return w.closeErr
}

// maxLine bounds a line of a trace file: a record with a message ID of
// the longest kind and two peer IDs takes about 300 bytes.
const maxLine = 64 * 1024

// Read decodes the records of a trace file, one JSON object per line.
// Blank lines are skipped, and so is a line that a crash or a failed write
// cut short: a record ends in the only "}" it holds.
func Read(r io.Reader) ([]Record, error) {
	var out []Record
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || !bytes.HasSuffix(line, []byte("}")) {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadFiles reads the trace files at paths, one per node, and returns all
// their records.
func ReadFiles(paths ...string) ([]Record, error) {
	var out []Record
	for _, path := range paths {
		recs, err := readFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	return out, nil
}

func readFile(path string) ([]Record, error) {
	f, err := os.Open(path) // #nosec G304 -- the harness names its own trace files.
	if err != nil {
		return nil, err
	}
	recs, err := Read(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("event trace %s: %w", path, err)
	}
	return recs, nil
}

// Spread is how one event spread over the traced nodes.
type Spread struct {
	Event string
	// Origin is the node that published the event and PublishedAt when;
	// empty if no trace records its publication.
	Origin      string
	PublishedAt time.Time
	// Reached holds, for every node that accepted the event, its first
	// acceptance.
	Reached map[string]Receipt
	// Copies counts the copies each node received, whatever their
	// outcome: the first accepted one, duplicates and dropped ones.
	Copies map[string]int
}

// Receipt is a node's first acceptance of an event.
type Receipt struct {
	// From is the peer that forwarded it, At when it arrived.
	From string
	At   time.Time
	// Hops counts the links from the origin: 1 for a node the origin sent
	// it to. It is -1, and Path nil, if the forwarding peers do not lead
	// back to the origin, e.g. through a node that wrote no trace.
	Hops int
	// Path lists the nodes from the origin to this one.
	Path []string
	// Delay is At minus Spread.PublishedAt; 0 without a publication.
	Delay time.Duration
}

// Join joins the records of several nodes by event, ordered by event ID.
func Join(records []Record) []Spread {
	byEvent := map[string]*Spread{}
	for _, r := range records {
		s := byEvent[r.Event]
		if s == nil {
			s = &Spread{Event: r.Event, Reached: map[string]Receipt{}, Copies: map[string]int{}}
			byEvent[r.Event] = s
		}
		switch r.Outcome {
		case Published:
			if s.Origin == "" || r.At.Before(s.PublishedAt) {
				s.Origin, s.PublishedAt = r.Node, r.At
			}
		case Accepted:
			s.Copies[r.Node]++
			if first, ok := s.Reached[r.Node]; !ok || r.At.Before(first.At) {
				s.Reached[r.Node] = Receipt{From: r.From, At: r.At}
			}
		default:
			s.Copies[r.Node]++
		}
	}
	out := make([]Spread, 0, len(byEvent))
	for _, s := range byEvent {
		s.trace()
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b Spread) int { return strings.Compare(a.Event, b.Event) })
	return out
}

// errNoPath marks a receipt whose forwarding peers do not lead back to
// the origin.
var errNoPath = errors.New("no path to the origin")

// trace fills in the hops, paths and delays of the receipts.
func (s *Spread) trace() {
	paths := map[string][]string{}
	for node, r := range s.Reached {
		path, err := s.path(node, paths, map[string]bool{})
		if err != nil {
			r.Hops, r.Path = -1, nil
		} else {
			r.Hops, r.Path = len(path)-1, path
		}
		if s.Origin != "" {
			r.Delay = r.At.Sub(s.PublishedAt)
		}
		s.Reached[node] = r
	}
}

// path returns the nodes from the origin to node along the forwarding
// peers of the first acceptances; known holds the paths found so far,
// visiting the nodes on the way, which guards against cycles.
func (s *Spread) path(node string, known map[string][]string, visiting map[string]bool) ([]string, error) {
	if node == s.Origin && s.Origin != "" {
		return []string{node}, nil
	}
	if p, ok := known[node]; ok {
		return p, nil
	}
	r, ok := s.Reached[node]
	if !ok || visiting[node] {
		return nil, errNoPath
	}
	visiting[node] = true
	parent, err := s.path(r.From, known, visiting)
	if err != nil {
		return nil, err
	}
	p := append(slices.Clip(parent), node)
	known[node] = p
	return p, nil
}
