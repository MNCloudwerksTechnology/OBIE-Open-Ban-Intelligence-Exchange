package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/dgraph-io/ristretto/v2"
)

// Default intervals of the background loops.
const (
	DefaultSweepInterval = time.Minute
	DefaultGCInterval    = 10 * time.Minute
)

// DefaultMaxIndicators is the default of Options.MaxIndicators
// (store.max_indicators).
const DefaultMaxIndicators = 1_000_000

// gcDiscardRatio is the share of stale data in a value-log file above which
// the GC rewrites it.
const gcDiscardRatio = 0.5

// Options configures a DB. Zero fields take their defaults.
type Options struct {
	// SweepInterval is how often expired verdicts and overrides are removed
	// and their indicators notified.
	SweepInterval time.Duration
	// GCInterval is how often the Badger value-log GC runs.
	GCInterval time.Duration
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// MaxIndicators caps the verdict records, one per publisher and
	// indicator; beyond it the record expiring first is evicted.
	MaxIndicators int
	// Self is this node's peer ID: its verdicts are never evicted, and
	// always kept once they ended. Empty protects none.
	Self string
	// MaxEnded caps the verdicts of other publishers kept once they were
	// revoked, and those kept once they expired (ADR 0023); 0 means a tenth
	// of MaxIndicators, at least 1,000.
	MaxEnded int
}

func (o Options) withDefaults() Options {
	if o.SweepInterval <= 0 {
		o.SweepInterval = DefaultSweepInterval
	}
	if o.GCInterval <= 0 {
		o.GCInterval = DefaultGCInterval
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.MaxIndicators <= 0 {
		o.MaxIndicators = DefaultMaxIndicators
	}
	if o.MaxEnded <= 0 {
		o.MaxEnded = max(o.MaxIndicators/10, minMaxEnded)
	}
	return o
}

// DB is the BadgerDB-backed Store and the lifecycle subsystem that owns the
// database: Start opens it, Stop closes it. Every method except Start, Stop,
// Name, Ready, Stats and Subscribe returns ErrClosed while it is not open.
type DB struct {
	dir  string // empty for an in-memory database
	log  *slog.Logger
	opts Options

	// mu guards db: operations hold it shared, Start and Stop exclusively,
	// so the database is never closed under a running operation.
	mu sync.RWMutex
	db *badger.DB
	// writeMu serializes all writes, so each read-check-write is atomic.
	writeMu sync.Mutex

	subsMu  sync.Mutex
	subs    map[int]func(Change)
	nextSub int

	counters counters
	// verdicts is the number of verdict records: of expiry index entries
	// pointing at one. Changed only with writeMu held.
	verdicts atomic.Int64
	// full is set once the store reached MaxIndicators; see warnFull.
	full atomic.Bool
	// evictFrom is a lower bound of the expiry index keys of the records
	// that may be evicted; see evictionCandidate. Guarded by writeMu.
	evictFrom []byte
	// endedCounts counts the ended verdicts kept (ADR 0023), and endedFull
	// is set once other publishers' reached MaxEnded; see warnEndedFull.
	endedCounts endedTally
	endedFull   atomic.Bool

	loopMu  sync.Mutex
	loopErr error
	stop    chan struct{}
	done    chan struct{}
}

var _ Store = (*DB)(nil)

// New returns the store subsystem for the database in dir, typically
// <node.state_dir>/db. The database is opened by Start.
func New(dir string, log *slog.Logger, opts Options) *DB {
	opts = opts.withDefaults()
	return &DB{dir: dir, log: log, opts: opts, subs: map[int]func(Change){}, endedCounts: endedTally{self: opts.Self}}
}

// NewMemory returns a store subsystem whose database lives in memory only.
// It behaves exactly like one on disk and is meant for tests.
func NewMemory(log *slog.Logger, opts Options) *DB {
	return New("", log, opts)
}

// Name returns the subsystem name.
func (s *DB) Name() string { return Name }

// Start opens the database and starts the expiry sweep and value-log GC.
func (s *DB) Start(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return errors.New("store already started")
	}
	if s.dir != "" {
		if err := os.MkdirAll(s.dir, 0o700); err != nil {
			return fmt.Errorf("create database directory: %w", err)
		}
		if err := checkFiles(s.dir, s.badgerOptions()); err != nil {
			return openError(s.dir, err)
		}
	}
	db, err := badger.Open(s.badgerOptions())
	if err != nil {
		return openError(s.dir, err)
	}
	s.db = db
	var n int64
	var ended map[string]EndedCount
	err = db.View(func(txn *badger.Txn) error {
		n = countVerdicts(txn)
		var err error
		ended, err = s.countEnded(txn)
		return err
	})
	if err != nil {
		_ = db.Close()
		s.db = nil
		return fmt.Errorf("count verdicts in %s: %w", s.dir, err)
	}
	s.verdicts.Store(0)
	s.addVerdicts(int(n))
	s.endedCounts.reset(ended)
	s.endedFull.Store(false)
	s.evictFrom = nil
	s.setLoopErr(nil)
	s.stop, s.done = make(chan struct{}), make(chan struct{})
	go s.loop(s.stop, s.done)
	s.log.Info("database opened", "dir", s.dir, "verdicts", n, "max_indicators", s.opts.MaxIndicators)
	return nil
}

// Stop ends the background loops and closes the database. If ctx expires
// before a running sweep finished, the database is still closed once the
// sweep's current step has.
func (s *DB) Stop(ctx context.Context) error {
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.stop = nil // a concurrent Stop returns right away
	s.mu.Unlock()
	if stop == nil {
		return nil
	}
	close(stop)
	select {
	case <-done:
	case <-ctx.Done():
		s.log.Warn("background loop did not stop in time; closing database anyway")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	if err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

// Ready reports whether the database is open and the last sweep succeeded.
func (s *DB) Ready() error {
	s.mu.RLock()
	open := s.db != nil
	s.mu.RUnlock()
	if !open {
		return ErrClosed
	}
	s.loopMu.Lock()
	defer s.loopMu.Unlock()
	return s.loopErr
}

// CacheBytes returns the bytes held in Badger's block and index caches,
// which fill up to their configured sizes; 0 while the database is closed.
func (s *DB) CacheBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return 0
	}
	var n int64
	for _, m := range []*ristretto.Metrics{s.db.BlockCacheMetrics(), s.db.IndexCacheMetrics()} {
		if m != nil {
			n += int64(m.CostAdded()) - int64(m.CostEvicted()) // #nosec G115 -- bounded by the cache sizes.
		}
	}
	return n
}

// Stats returns the Put outcomes counted so far.
func (s *DB) Stats() Stats {
	return s.counters.stats()
}

// Subscribe registers fn for change notifications; see Store.
func (s *DB) Subscribe(fn func(Change)) func() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = fn
	return func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		delete(s.subs, id)
	}
}

// notify calls every subscriber for each change. It must not be called with
// writeMu held, so that subscribers may read the store.
func (s *DB) notify(changes []Change) {
	if len(changes) == 0 {
		return
	}
	s.subsMu.Lock()
	subs := make([]func(Change), 0, len(s.subs))
	for _, fn := range s.subs {
		subs = append(subs, fn)
	}
	s.subsMu.Unlock()
	for _, c := range changes {
		for _, fn := range subs {
			fn(c)
		}
	}
}

// view runs fn in a read-only transaction.
func (s *DB) view(fn func(txn *badger.Txn) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return ErrClosed
	}
	return s.db.View(fn)
}

// update runs fn in a read-write transaction. Callers hold writeMu.
func (s *DB) update(fn func(txn *badger.Txn) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return ErrClosed
	}
	return s.db.Update(fn)
}

// loop runs the expiry sweep and the value-log GC until stop is closed.
func (s *DB) loop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	sweep := time.NewTicker(s.opts.SweepInterval)
	defer sweep.Stop()
	gc := time.NewTicker(s.opts.GCInterval)
	defer gc.Stop()
	for {
		select {
		case <-stop:
			return
		case <-sweep.C:
			err := s.Sweep(s.opts.Now())
			select {
			case <-stop:
				return // stopping: the result no longer reflects readiness
			default:
			}
			if err != nil {
				s.log.Error("expiry sweep failed", "error", err)
				err = fmt.Errorf("expiry sweep: %w", err)
			}
			s.setLoopErr(err)
		case <-gc.C:
			if err := s.runValueLogGC(); err != nil {
				s.log.Warn("value log GC failed", "error", err)
			}
		}
	}
}

func (s *DB) setLoopErr(err error) {
	s.loopMu.Lock()
	defer s.loopMu.Unlock()
	s.loopErr = err
}

// runValueLogGC rewrites value-log files until none is worth rewriting.
func (s *DB) runValueLogGC() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil || s.dir == "" {
		return nil // closed, or in memory: there is no value log
	}
	for {
		err := s.db.RunValueLogGC(gcDiscardRatio)
		switch {
		case errors.Is(err, badger.ErrNoRewrite):
			return nil
		case err != nil:
			return err
		}
	}
}

// badgerOptions sizes Badger for a small VPS (ADR 0008). Every block's
// checksum is verified when it is read, so damage on disk is reported
// instead of read as data; checkFiles verifies all tables before the
// database opens (ADR 0017).
func (s *DB) badgerOptions() badger.Options {
	opts := badger.DefaultOptions(s.dir).
		WithChecksumVerificationMode(options.OnBlockRead).
		WithLogger(badgerLogger{s.log}).
		WithMetricsEnabled(false).
		WithMemTableSize(16 << 20).
		WithNumMemtables(2).
		WithBlockCacheSize(32 << 20).
		WithIndexCacheSize(16 << 20).
		WithValueLogFileSize(64 << 20)
	if s.dir == "" {
		opts = opts.WithInMemory(true)
	}
	return opts
}

// badgerLogger forwards Badger's log output to slog. Badger's info messages
// are routine (compactions, flushes) and logged at debug level.
type badgerLogger struct{ log *slog.Logger }

func (l badgerLogger) Errorf(format string, args ...any) {
	l.log.Error(strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func (l badgerLogger) Warningf(format string, args ...any) {
	l.log.Warn(strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func (l badgerLogger) Infof(format string, args ...any) {
	l.log.Debug(strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func (l badgerLogger) Debugf(format string, args ...any) {
	l.log.Debug(strings.TrimSpace(fmt.Sprintf(format, args...)))
}
