package decision

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Name is the subsystem name of the decision engine.
const Name = "decision"

// DefaultRefreshInterval is how often blocks that reached their expiry are
// re-evaluated.
const DefaultRefreshInterval = 10 * time.Second

// ChangeType says how a block decision changed.
type ChangeType string

// Change types.
const (
	// ChangeAdded: the indicator became blocked.
	ChangeAdded ChangeType = "added"
	// ChangeUpdated: the indicator stays blocked, but its expiry, score or
	// contributors changed.
	ChangeUpdated ChangeType = "updated"
	// ChangeRemoved: the indicator is no longer blocked.
	ChangeRemoved ChangeType = "removed"
)

// Causes of a change besides the store's change reasons.
const (
	// CauseStartup: the decision was built when the engine started.
	CauseStartup = "startup"
	// CauseRefresh: a block reached its expiry and was re-evaluated.
	CauseRefresh = "refresh"
)

// Change is an entry of the block change stream.
type Change struct {
	Type ChangeType
	// Key is the indicator's obieproto.Indicator.Key.
	Key string
	// Decision is the new decision, without Publishers; for ChangeRemoved
	// its State is StateNone and Reason says why.
	Decision Decision
	// Cause is what triggered the evaluation: a store.Reason, CauseStartup
	// or CauseRefresh.
	Cause string
}

// Options configures an Engine. Zero fields take their defaults.
type Options struct {
	// RefreshInterval is how often expired blocks are re-evaluated.
	RefreshInterval time.Duration
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.RefreshInterval <= 0 {
		o.RefreshInterval = DefaultRefreshInterval
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// Engine is the decision subsystem. It keeps the decision of every
// indicator with active verdicts in the store, re-evaluates indicators the
// store reports as changed, and streams block changes to subscribers.
type Engine struct {
	store  store.Store
	policy Policy
	log    *slog.Logger
	opts   Options

	// mu guards decisions: the decision of every indicator with active
	// verdicts, by key, without Publishers.
	mu        sync.RWMutex
	decisions map[string]Decision

	// dirty holds the indicators changed in the store since the worker last
	// ran, with the latest reason; wake signals the worker.
	dirtyMu sync.Mutex
	dirty   map[string]store.Reason
	wake    chan struct{}

	// workMu serializes evaluations, so decisions are applied in the order
	// the store state was read.
	workMu sync.Mutex

	subsMu  sync.Mutex
	subs    map[int]func(Change)
	nextSub int

	errMu   sync.Mutex
	lastErr error

	runMu       sync.Mutex
	unsubscribe func()
	stop        chan struct{}
	done        chan struct{}
}

// New returns the decision engine over st under policy p.
func New(st store.Store, p Policy, log *slog.Logger, opts Options) *Engine {
	return &Engine{
		store:     st,
		policy:    p,
		log:       log,
		opts:      opts.withDefaults(),
		decisions: map[string]Decision{},
		dirty:     map[string]store.Reason{},
		wake:      make(chan struct{}, 1),
		subs:      map[int]func(Change){},
	}
}

// Name returns the subsystem name.
func (e *Engine) Name() string { return Name }

// Start subscribes to the store, decides on every indicator with active
// verdicts and starts the worker. Subscribers registered before Start
// receive the initial blocks as ChangeAdded with CauseStartup.
func (e *Engine) Start(ctx context.Context) error {
	e.runMu.Lock()
	defer e.runMu.Unlock()
	if e.stop != nil {
		return errors.New("decision engine already started")
	}
	// Subscribe before loading, so no change between the two is lost; it is
	// only marked dirty and evaluated again by the worker.
	unsubscribe := e.store.Subscribe(e.markDirty)
	if err := e.load(ctx); err != nil {
		unsubscribe()
		return err
	}
	e.unsubscribe = unsubscribe
	e.stop, e.done = make(chan struct{}), make(chan struct{})
	go e.loop(e.stop, e.done)
	e.mu.RLock()
	n := len(e.decisions)
	e.mu.RUnlock()
	e.log.Info("decision engine started", "indicators", n, "blocked", len(e.Decisions(StateBlock)))
	return nil
}

// Stop ends the worker and the store subscription.
func (e *Engine) Stop(ctx context.Context) error {
	e.runMu.Lock()
	defer e.runMu.Unlock()
	if e.stop == nil {
		return nil
	}
	e.unsubscribe()
	close(e.stop)
	stopped := e.done
	e.stop, e.done, e.unsubscribe = nil, nil, nil
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("decision worker did not stop: %w", ctx.Err())
	}
}

// Ready reports the error of the last failed evaluation, nil if the last
// one succeeded.
func (e *Engine) Ready() error {
	e.errMu.Lock()
	defer e.errMu.Unlock()
	return e.lastErr
}

// Detail summarizes the kept decisions for the node status.
func (e *Engine) Detail() string {
	return fmt.Sprintf("%d blocked of %d indicators", len(e.Decisions(StateBlock)), e.count())
}

func (e *Engine) count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.decisions)
}

// Subscribe registers fn for the block change stream. Callbacks run on the
// engine's worker goroutine, one at a time and in order; they must be fast
// and must not call back into the engine's Subscribe. It returns a function
// that removes the subscription.
func (e *Engine) Subscribe(fn func(Change)) (unsubscribe func()) {
	e.subsMu.Lock()
	defer e.subsMu.Unlock()
	id := e.nextSub
	e.nextSub++
	e.subs[id] = fn
	return func() {
		e.subsMu.Lock()
		defer e.subsMu.Unlock()
		delete(e.subs, id)
	}
}

// Explain evaluates the normalized indicator ind now, with the
// contribution of every publisher. An indicator without active verdicts
// yields StateNone.
func (e *Engine) Explain(ind obieproto.Indicator) (Decision, error) {
	now := e.opts.Now()
	verdicts, err := e.store.ActiveVerdicts(ind.Key(), now)
	if err != nil {
		return Decision{}, fmt.Errorf("read verdicts of %s: %w", ind.Key(), err)
	}
	return Evaluate(ind, verdicts, e.policy, now), nil
}

// Decisions returns the kept decisions in state, or all for "", ordered by
// indicator key and without Publishers.
func (e *Engine) Decisions(state State) []Decision {
	e.mu.RLock()
	out := make([]Decision, 0, len(e.decisions))
	for _, d := range e.decisions {
		if state == "" || d.State == state {
			out = append(out, d)
		}
	}
	e.mu.RUnlock()
	slices.SortFunc(out, func(a, b Decision) int { return strings.Compare(a.Indicator.Key(), b.Indicator.Key()) })
	return out
}

// load decides on every indicator with active verdicts.
func (e *Engine) load(ctx context.Context) error {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	now := e.opts.Now()
	page := store.Page{Limit: store.MaxPageLimit}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := e.store.ListIndicators(now, store.Filter{}, page)
		if err != nil {
			return fmt.Errorf("load indicators: %w", err)
		}
		for _, it := range items.Items {
			e.apply(it.Key, Evaluate(it.Indicator, it.Verdicts, e.policy, now), CauseStartup)
		}
		if items.Next == "" {
			return nil
		}
		page.After = items.Next
	}
}

// markDirty is the store subscription: it only queues the indicator, so the
// store's writer is never blocked by an evaluation.
func (e *Engine) markDirty(c store.Change) {
	e.dirtyMu.Lock()
	e.dirty[c.Key] = c.Reason
	e.dirtyMu.Unlock()
	select {
	case e.wake <- struct{}{}:
	default: // a wake-up is already pending
	}
}

// loop re-evaluates dirty indicators and refreshes expired blocks until
// stop is closed.
func (e *Engine) loop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	refresh := time.NewTicker(e.opts.RefreshInterval)
	defer refresh.Stop()
	// Changes marked while loading are still pending.
	e.processDirty()
	for {
		select {
		case <-stop:
			return
		case <-e.wake:
			e.processDirty()
		case <-refresh.C:
			e.refreshExpired()
		}
	}
}

// processDirty re-evaluates every indicator marked dirty.
func (e *Engine) processDirty() {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.dirtyMu.Lock()
	dirty := e.dirty
	e.dirty = make(map[string]store.Reason, len(dirty))
	e.dirtyMu.Unlock()

	keys := make([]string, 0, len(dirty))
	for key := range dirty {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		e.reevaluate(key, string(dirty[key]))
	}
}

// refreshExpired re-evaluates the blocks that reached their expiry, e.g.
// because it was capped by decision.max_ttl while their verdicts live on.
func (e *Engine) refreshExpired() {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	now := e.opts.Now()
	var due []string
	e.mu.RLock()
	for key, d := range e.decisions {
		if d.State == StateBlock && !now.Before(d.ExpiresAt) {
			due = append(due, key)
		}
	}
	e.mu.RUnlock()
	slices.Sort(due)
	for _, key := range due {
		e.reevaluate(key, CauseRefresh)
	}
}

// reevaluate decides on the indicator with key from the store's current
// state. Callers hold workMu.
func (e *Engine) reevaluate(key, cause string) {
	now := e.opts.Now()
	verdicts, err := e.store.ActiveVerdicts(key, now)
	if err != nil {
		e.log.Error("reading verdicts failed; keeping the previous decision", "indicator", key, "error", err)
		e.setErr(fmt.Errorf("read verdicts of %s: %w", key, err))
		return
	}
	e.setErr(nil)
	ind, ok := e.indicatorOf(key, verdicts)
	if !ok {
		return // no verdicts before or now, e.g. an override was set: nothing to decide
	}
	e.apply(key, Evaluate(ind, verdicts, e.policy, now), cause)
}

// indicatorOf returns the indicator with key, from its verdicts or else
// from the kept decision.
func (e *Engine) indicatorOf(key string, verdicts []*obieproto.Event) (obieproto.Indicator, bool) {
	if len(verdicts) > 0 {
		return verdicts[0].Indicator, true
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	d, ok := e.decisions[key]
	return d.Indicator, ok
}

// apply keeps d as the decision of key and notifies subscribers if the
// block changed. Callers hold workMu.
func (e *Engine) apply(key string, d Decision, cause string) {
	active := len(d.Publishers) > 0
	d.Publishers = nil
	e.mu.Lock()
	prev, had := e.decisions[key]
	if active {
		e.decisions[key] = d
	} else {
		delete(e.decisions, key)
	}
	e.mu.Unlock()

	wasBlock := had && prev.State == StateBlock
	isBlock := d.State == StateBlock
	var typ ChangeType
	switch {
	case !wasBlock && isBlock:
		typ = ChangeAdded
	case wasBlock && !isBlock:
		typ = ChangeRemoved
	case wasBlock && blockChanged(&prev, &d):
		typ = ChangeUpdated
	default:
		return
	}
	e.log.Debug("block decision changed", "indicator", key, "change", typ, "cause", cause, "reason", d.Reason)
	e.notify(Change{Type: typ, Key: key, Decision: d, Cause: cause})
}

// blockChanged reports whether a block differs in what an enforcer or an
// operator would notice.
func blockChanged(a, b *Decision) bool {
	return !a.ExpiresAt.Equal(b.ExpiresAt) || a.Score != b.Score || a.Contributors != b.Contributors || a.Autoblock != b.Autoblock
}

func (e *Engine) notify(c Change) {
	e.subsMu.Lock()
	ids := make([]int, 0, len(e.subs))
	for id := range e.subs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	subs := make([]func(Change), len(ids))
	for i, id := range ids {
		subs[i] = e.subs[id]
	}
	e.subsMu.Unlock()
	for _, fn := range subs {
		fn(c)
	}
}

func (e *Engine) setErr(err error) {
	e.errMu.Lock()
	defer e.errMu.Unlock()
	e.lastErr = err
}
