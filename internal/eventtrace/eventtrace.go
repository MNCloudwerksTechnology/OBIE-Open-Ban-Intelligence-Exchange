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

// Records are buffered, so that GossipSub's event loop, which traces the
// copies it drops, does not wait for the disk; they reach the file every
// flushInterval, when bufferSize is full, and on Close.
const (
	flushInterval = time.Second
	bufferSize    = 64 * 1024
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
	// stop ends the flushing goroutine, which closes done.
	stop, done chan struct{}

	mu  sync.Mutex
	f   *os.File
	buf *bufio.Writer
	// failed is set once a write failed, so that the failure is logged
	// once, not for every record; closing once Close began.
	failed, closing bool
}

// Open opens the trace file at path for appending the records of the node
// with peer ID node, creating it if needed; its directory must exist.
// Close flushes and closes it.
func Open(path, node string, log *slog.Logger) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, fileMode) // #nosec G304 -- the operator chooses mesh.trace_path.
	if err != nil {
		return nil, fmt.Errorf("open event trace: %w", err)
	}
	w := &Writer{node: node, log: log, stop: make(chan struct{}), done: make(chan struct{}), f: f,
		buf: bufio.NewWriterSize(f, bufferSize)}
	go w.flushEvery(flushInterval)
	return w, nil
}

// Write appends the record of a copy of the event with ID event that
// peer from forwarded, or the node published, at at with outcome. A
// failure is logged once; the record is lost.
func (w *Writer) Write(event, from string, at time.Time, outcome string) {
	if w == nil {
		return
	}
	line, err := json.Marshal(Record{Node: w.node, Event: event, From: from, At: at.UTC(), Outcome: outcome})
	if err != nil {
		return // a Record always encodes
	}
	line = append(line, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return
	}
	if _, err := w.buf.Write(line); err != nil {
		w.failedWith(err)
	}
}

// flushEvery writes the buffered records to the file every interval until
// Close.
func (w *Writer) flushEvery(interval time.Duration) {
	defer close(w.done)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-tick.C:
			w.mu.Lock()
			if err := w.buf.Flush(); err != nil {
				w.failedWith(err)
			}
			w.mu.Unlock()
		}
	}
}

// failedWith logs the first failed write. The caller holds mu.
func (w *Writer) failedWith(err error) {
	if w.failed {
		return
	}
	w.failed = true
	w.log.Error("writing the event trace failed; records are lost", "path", w.f.Name(), "error", err)
}

// Close writes the buffered records and closes the file; later records
// are dropped.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closing {
		w.mu.Unlock()
		return nil
	}
	w.closing = true
	close(w.stop)
	w.mu.Unlock()
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	flushErr := w.buf.Flush()
	err := errors.Join(flushErr, w.f.Close())
	w.f = nil
	if err != nil {
		return fmt.Errorf("close event trace: %w", err)
	}
	return nil
}

// maxLine bounds a line of a trace file: a record with a message ID of
// the longest kind and two peer IDs takes about 300 bytes.
const maxLine = 64 * 1024

// Read decodes the records of a trace file, one JSON object per line;
// blank lines are skipped.
func Read(r io.Reader) ([]Record, error) {
	var out []Record
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
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
