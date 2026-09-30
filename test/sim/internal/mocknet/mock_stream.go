package mocknet

import (
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

var streamCounter atomic.Int64

// stream implements network.Stream. Unlike go-libp2p's mocknet, a write
// never waits for an earlier one to arrive, and no goroutine carries the
// writes: each is queued in the direction it travels with its arrival
// time, and the reader at the other end waits for that time (see pipe).
type stream struct {
	rstream *stream
	conn    *conn
	id      int64

	// out carries this end's writes to the other end; in carries the
	// other end's writes to this one.
	out, in *pipe

	// teardownOnce removes the stream end from its connection once.
	teardownOnce sync.Once

	protocol atomic.Pointer[protocol.ID]
	stat     network.Stats
}

// ErrClosed is the error of a write after the stream was closed.
var ErrClosed = errors.New("stream closed")

type transportObject struct {
	msg         []byte
	arrivalTime time.Time
}

// pipe is one direction of a stream: the writes of one end, in order,
// each readable at the other end from its arrival time on.
type pipe struct {
	mu sync.Mutex
	// queue holds the writes that have not been read, in arrival order;
	// head is the unread rest of the first one, once it arrived.
	queue []transportObject
	head  []byte
	// last is the arrival time of the latest write: a later write never
	// overtakes it.
	last time.Time
	// writeErr is set once the writer closed (ErrClosed) or either end
	// reset (network.ErrReset); readErr once the reader closed or either
	// end reset. The reader drains the queue before it sees ErrClosed as
	// EOF; a reset discards the queue.
	writeErr, readErr error
	// wake tells the reader that the queue was empty and is not, or that
	// an error was set.
	wake chan struct{}
}

func newPipe() *pipe { return &pipe{wake: make(chan struct{}, 1)} }

func (p *pipe) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// write queues a copy of b to arrive at the given time, but not before
// the previous write.
func (p *pipe) write(b []byte, arrival time.Time) (int, error) {
	cpy := make([]byte, len(b))
	copy(cpy, b)
	p.mu.Lock()
	switch {
	case p.writeErr != nil:
		err := p.writeErr
		p.mu.Unlock()
		return 0, err
	case p.readErr != nil:
		// The reader is gone: the bytes are dropped, as a peer drops what
		// arrives on a stream it stopped reading.
		p.mu.Unlock()
		return len(b), nil
	}
	if arrival.Before(p.last) {
		arrival = p.last
	}
	p.last = arrival
	wasEmpty := len(p.queue) == 0
	p.queue = append(p.queue, transportObject{msg: cpy, arrivalTime: arrival})
	p.mu.Unlock()
	if wasEmpty {
		p.signal()
	}
	return len(b), nil
}

// read reads the data that has arrived, waiting for the next write to
// arrive if none has.
func (p *pipe) read(b []byte) (int, error) {
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		p.mu.Lock()
		if p.readErr != nil {
			err := p.readErr
			p.mu.Unlock()
			return 0, err
		}
		if len(p.head) == 0 && len(p.queue) > 0 && !time.Now().Before(p.queue[0].arrivalTime) {
			p.head = p.queue[0].msg
			p.queue[0] = transportObject{}
			p.queue = p.queue[1:]
		}
		if len(p.head) > 0 {
			n := copy(b, p.head)
			p.head = p.head[n:]
			p.mu.Unlock()
			return n, nil
		}
		var due <-chan time.Time
		switch {
		case len(p.queue) > 0:
			d := time.Until(p.queue[0].arrivalTime)
			if timer == nil {
				timer = time.NewTimer(d)
			} else {
				timer.Reset(d)
			}
			due = timer.C
		case errors.Is(p.writeErr, ErrClosed):
			p.mu.Unlock()
			return 0, io.EOF
		case p.writeErr != nil:
			err := p.writeErr
			p.mu.Unlock()
			return 0, err
		}
		p.mu.Unlock()
		select {
		case <-p.wake:
		case <-due:
		}
	}
}

// closeWrite ends the writes: the reader gets EOF once it read the queue.
func (p *pipe) closeWrite() {
	p.mu.Lock()
	if p.writeErr == nil {
		p.writeErr = ErrClosed
	}
	p.mu.Unlock()
	p.signal()
}

// closeRead ends the reads with err and drops what is queued.
func (p *pipe) closeRead(err error) {
	p.mu.Lock()
	if p.readErr == nil {
		p.readErr = err
	}
	p.queue, p.head = nil, nil
	p.mu.Unlock()
	p.signal()
}

// reset fails both ends of the direction with network.ErrReset.
func (p *pipe) reset() {
	p.mu.Lock()
	p.writeErr, p.readErr = network.ErrReset, network.ErrReset
	p.queue, p.head = nil, nil
	p.mu.Unlock()
	p.signal()
}

func newStreamPair() (*stream, *stream) {
	ab, ba := newPipe(), newPipe()
	sa := newStream(ab, ba, network.DirOutbound)
	sb := newStream(ba, ab, network.DirInbound)
	sa.rstream = sb
	sb.rstream = sa
	return sa, sb
}

func newStream(out, in *pipe, dir network.Direction) *stream {
	return &stream{
		out:  out,
		in:   in,
		id:   streamCounter.Add(1),
		stat: network.Stats{Direction: dir},
	}
}

// Write queues a copy of p for delivery after the link's latency (and its
// bandwidth delay, if any), but not before the previous write.
func (s *stream) Write(p []byte) (n int, err error) {
	l := s.conn.link
	return s.out.write(p, time.Now().Add(l.GetLatency()+l.RateLimit(len(p))))
}

func (s *stream) ID() string {
	return strconv.FormatInt(s.id, 10)
}

func (s *stream) Protocol() protocol.ID {
	p := s.protocol.Load()
	if p == nil {
		return ""
	}
	return *p
}

func (s *stream) Stat() network.Stats {
	return s.stat
}

func (s *stream) SetProtocol(proto protocol.ID) error {
	s.protocol.Store(&proto)
	return nil
}

// CloseWrite ends this end's writes; the other end reads what was
// written, then EOF. The stream end is torn down: nothing more is sent.
func (s *stream) CloseWrite() error {
	s.out.closeWrite()
	s.teardown()
	return nil
}

func (s *stream) CloseRead() error {
	s.in.closeRead(ErrClosed)
	return nil
}

func (s *stream) Close() error {
	_ = s.CloseRead()
	return s.CloseWrite()
}

// Reset fails pending and later reads and writes at both ends with
// network.ErrReset.
func (s *stream) Reset() error {
	s.out.reset()
	s.in.reset()
	s.teardown()
	return nil
}

// ResetWithError resets the stream. It ignores the provided error code.
func (s *stream) ResetWithError(_ network.StreamErrorCode) error {
	return s.Reset()
}

func (s *stream) teardown() {
	s.teardownOnce.Do(func() { s.conn.removeStream(s) })
}

func (s *stream) Conn() network.Conn {
	return s.conn
}

// SetDeadline is a noop for mocknet streams since the underlying pipe
// transport does not support deadlines. Callers should not treat the
// absence of deadline support as an error.
func (s *stream) SetDeadline(_ time.Time) error      { return nil }
func (s *stream) SetReadDeadline(_ time.Time) error  { return nil }
func (s *stream) SetWriteDeadline(_ time.Time) error { return nil }

func (s *stream) Read(b []byte) (int, error) {
	return s.in.read(b)
}

func (s *stream) Scope() network.StreamScope {
	return &network.NullScope{}
}
