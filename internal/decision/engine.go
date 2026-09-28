package decision

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
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
	// CauseSnapshot: the block existed when the subscriber subscribed.
	CauseSnapshot = "snapshot"
	// CauseReload: the configuration was reloaded.
	CauseReload = "reload"
)

// Change is an entry of the block change stream.
type Change struct {
	Type ChangeType
	// Key is the indicator's obieproto.Indicator.Key.
	Key string
	// Decision is the new decision, without Publishers; for ChangeRemoved
	// its State is StateNone and Reason says why.
	Decision Decision
	// Cause is what triggered the evaluation: a store.Reason, CauseStartup,
	// CauseRefresh, CauseSnapshot or CauseReload (and enforce.CauseMode for
	// the changes the mode gate sends).
	Cause string
}

// Options configures an Engine. Zero fields take their defaults.
type Options struct {
	// RefreshInterval is how often expired blocks are re-evaluated.
	RefreshInterval time.Duration
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Allowlist is the effective allow-list. Nil allows nothing beyond the
	// overrides; obied always passes one with the built-in ranges.
	Allowlist *sovereignty.Allowlist
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
	store store.Store
	log   *slog.Logger
	opts  Options

	// rulesMu guards policy and rules, which Reload replaces and the
	// worker updates with the overrides in the store.
	rulesMu sync.RWMutex
	policy  Policy
	rules   Rules

	// mu guards decisions: the decision of every indicator with active
	// verdicts, by key, without Publishers.
	mu        sync.RWMutex
	decisions map[string]Decision

	// dirty holds the indicators changed in the store since the worker last
	// ran, or whose evaluation failed, with the latest cause; wake signals
	// the worker.
	dirtyMu sync.Mutex
	dirty   map[string]string
	// overridesDirty is set when an override may have changed; unlike a
	// cause in dirty, a later verdict on the same key cannot hide it.
	overridesDirty bool
	wake           chan struct{}

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

// New returns the decision engine over st under policy p and the allow-list
// in opts.
func New(st store.Store, p Policy, log *slog.Logger, opts Options) *Engine {
	return &Engine{
		store:     st,
		policy:    p,
		rules:     Rules{Allowlist: opts.Allowlist},
		log:       log,
		opts:      opts.withDefaults(),
		decisions: map[string]Decision{},
		dirty:     map[string]string{},
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

// Ready reports why the last evaluation pass failed, nil if it succeeded.
// Indicators whose evaluation failed are retried every refresh interval.
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

// Subscribe registers fn for the block change stream. Every block that
// exists at that moment is first delivered to fn as ChangeAdded with
// CauseSnapshot, atomically with the registration, so a subscriber sees
// every block exactly once from then on; subscribers registered before
// Start receive the initial blocks with CauseStartup instead. Callbacks run
// one at a time and in order — on the worker goroutine, or on the goroutine
// calling Start or Subscribe — and must be fast. They must not call
// Subscribe, Start or Stop. It returns a function that removes the
// subscription.
func (e *Engine) Subscribe(fn func(Change)) (unsubscribe func()) {
	// workMu keeps evaluations out until fn is registered and has the
	// snapshot, so no change is missed or delivered twice.
	e.workMu.Lock()
	defer e.workMu.Unlock()
	for _, d := range e.list(StateBlock, false) {
		fn(Change{Type: ChangeAdded, Key: d.Indicator.Key(), Decision: d, Cause: CauseSnapshot})
	}
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

// Explain decides on the normalized indicator ind now, with the
// contribution of every publisher and the overrides as stored at this
// moment. An indicator without active verdicts and rules yields
// StateNone. Only verdicts on ind itself count: an address inside a CIDR
// range with verdicts is explained on its own.
func (e *Engine) Explain(ind obieproto.Indicator) (Decision, error) {
	now := e.opts.Now()
	verdicts, err := e.store.ActiveVerdicts(ind.Key(), now)
	if err != nil {
		return Decision{}, fmt.Errorf("read verdicts of %s: %w", ind.Key(), err)
	}
	overrides, err := e.store.Overrides(now)
	if err != nil {
		return Decision{}, fmt.Errorf("read overrides: %w", err)
	}
	p, r := e.current()
	r.Overrides = sovereignty.NewOverrides(overrides)
	return Decide(ind, verdicts, p, r, now), nil
}

// Reload replaces the policy and the allow-list and re-decides every kept
// indicator and every force-block, notifying block changes with
// CauseReload.
func (e *Engine) Reload(p Policy, allow *sovereignty.Allowlist) {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.rulesMu.Lock()
	e.policy, e.rules.Allowlist = p, allow
	e.rulesMu.Unlock()
	if _, err := e.refreshOverrides(e.opts.Now()); err != nil {
		e.log.Error("reading overrides failed; using the previous ones", "error", err)
	}
	keys := e.keptKeys()
	_, r := e.current()
	for _, o := range r.Overrides.ForceBlocks() {
		keys = append(keys, o.Indicator.Key())
	}
	slices.Sort(keys)
	e.reevaluateAll(slices.Compact(keys), func(string) string { return CauseReload })
}

// current returns the policy and rules in effect.
func (e *Engine) current() (Policy, Rules) {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()
	return e.policy, e.rules
}

// decide decides on ind under the current policy and rules.
func (e *Engine) decide(ind obieproto.Indicator, verdicts []*obieproto.Event, now time.Time) Decision {
	p, r := e.current()
	return Decide(ind, verdicts, p, r, now)
}

// refreshOverrides reads the overrides in effect from the store and returns
// the ranges of the force-allow overrides that changed. Callers hold
// workMu.
func (e *Engine) refreshOverrides(now time.Time) ([]netip.Prefix, error) {
	list, err := e.store.Overrides(now)
	if err != nil {
		return nil, err
	}
	next := sovereignty.NewOverrides(list)
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()
	changed := e.rules.Overrides.Changed(next)
	e.rules.Overrides = next
	return changed, nil
}

// keptKeys returns the keys of the kept decisions.
func (e *Engine) keptKeys() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	keys := make([]string, 0, len(e.decisions))
	for key := range e.decisions {
		keys = append(keys, key)
	}
	return keys
}

// overlapping returns the keys of the kept decisions and of the
// force-blocks whose indicator overlaps any of ranges. A force-block that a
// force-allow overruled is not kept, but must be re-decided when the
// force-allow goes.
func (e *Engine) overlapping(ranges []netip.Prefix) []string {
	if len(ranges) == 0 {
		return nil
	}
	overlaps := func(ind obieproto.Indicator) bool {
		p, err := sovereignty.PrefixOf(ind)
		return err == nil && slices.ContainsFunc(ranges, p.Overlaps)
	}
	var keys []string
	_, r := e.current()
	for _, o := range r.Overrides.ForceBlocks() {
		if overlaps(o.Indicator) {
			keys = append(keys, o.Indicator.Key())
		}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for key, d := range e.decisions {
		if overlaps(d.Indicator) {
			keys = append(keys, key)
		}
	}
	return keys
}

// Decisions returns the kept decisions in state, or all for "", ordered by
// indicator key and without Publishers. A block that reached its expiry but
// has not been re-evaluated yet is not listed as a block.
func (e *Engine) Decisions(state State) []Decision {
	return e.list(state, true)
}

// list returns the kept decisions in state, or all for "", by key; with
// hideExpired, blocks that reached their expiry are left out of StateBlock.
func (e *Engine) list(state State, hideExpired bool) []Decision {
	now := e.opts.Now()
	e.mu.RLock()
	out := make([]Decision, 0, len(e.decisions))
	for _, d := range e.decisions {
		expired := hideExpired && d.State == StateBlock && !now.Before(d.ExpiresAt)
		if state == "" || (d.State == state && !expired) {
			out = append(out, d)
		}
	}
	e.mu.RUnlock()
	slices.SortFunc(out, func(a, b Decision) int { return strings.Compare(a.Indicator.Key(), b.Indicator.Key()) })
	return out
}

// load reads the overrides and decides on every indicator with active
// verdicts or a force-block.
func (e *Engine) load(ctx context.Context) error {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	now := e.opts.Now()
	if _, err := e.refreshOverrides(now); err != nil {
		return fmt.Errorf("load overrides: %w", err)
	}
	loaded := map[string]bool{}
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
			loaded[it.Key] = true
			e.apply(it.Key, e.decide(it.Indicator, it.Verdicts, now), CauseStartup)
		}
		if items.Next == "" {
			break
		}
		page.After = items.Next
	}
	_, r := e.current()
	for _, o := range r.Overrides.ForceBlocks() {
		if key := o.Indicator.Key(); !loaded[key] {
			e.apply(key, e.decide(o.Indicator, nil, now), CauseStartup)
		}
	}
	return nil
}

// markDirty is the store subscription: it only queues the indicator, so the
// store's writer is never blocked by an evaluation.
func (e *Engine) markDirty(c store.Change) {
	e.dirtyMu.Lock()
	e.dirty[c.Key] = string(c.Reason)
	if c.Reason == store.ReasonOverride || c.Reason == store.ReasonExpiry {
		e.overridesDirty = true
	}
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
			e.processDirty() // retries failed evaluations
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
	e.dirty = make(map[string]string, len(dirty))
	overridesChanged := e.overridesDirty
	e.overridesDirty = false
	e.dirtyMu.Unlock()

	keys := make([]string, 0, len(dirty))
	for key := range dirty {
		keys = append(keys, key)
	}
	if overridesChanged {
		ranges, err := e.refreshOverrides(e.opts.Now())
		if err != nil {
			e.log.Error("reading overrides failed; keeping the previous decisions and retrying", "error", err)
			e.remark(dirty)
			e.dirtyMu.Lock()
			e.overridesDirty = true
			e.dirtyMu.Unlock()
			e.setErr(fmt.Errorf("read overrides: %w", err))
			return
		}
		// A force-allow covers every indicator it overlaps.
		for _, key := range e.overlapping(ranges) {
			if _, ok := dirty[key]; !ok {
				dirty[key] = string(store.ReasonOverride)
				keys = append(keys, key)
			}
		}
	}
	slices.Sort(keys)
	e.reevaluateAll(keys, func(key string) string { return dirty[key] })
}

// remark marks keys dirty again, without waking the worker, unless a newer
// change was marked meanwhile.
func (e *Engine) remark(keys map[string]string) {
	e.dirtyMu.Lock()
	defer e.dirtyMu.Unlock()
	for key, cause := range keys {
		if _, newer := e.dirty[key]; !newer {
			e.dirty[key] = cause
		}
	}
}

// refreshExpired re-evaluates the blocks that reached their expiry, e.g.
// because it was capped by decision.max_ttl while their verdicts live on,
// and the decisions whose override ended, so an expired force-allow does
// not wait for the store's sweep.
func (e *Engine) refreshExpired() {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	now := e.opts.Now()
	var due []string
	e.mu.RLock()
	for key, d := range e.decisions {
		blockEnded := d.State == StateBlock && !now.Before(d.ExpiresAt)
		overrideEnded := !d.Sovereignty.ExpiresAt.IsZero() && !now.Before(d.Sovereignty.ExpiresAt)
		if blockEnded || overrideEnded {
			due = append(due, key)
		}
	}
	e.mu.RUnlock()
	slices.Sort(due)
	e.reevaluateAll(due, func(string) string { return CauseRefresh })
}

// reevaluateAll re-evaluates the indicators with keys. Those that fail are
// marked dirty again, without waking the worker, so they are retried on the
// next refresh tick, and the first failure is reported by Ready. Callers
// hold workMu.
func (e *Engine) reevaluateAll(keys []string, cause func(key string) string) {
	var firstErr error
	for _, key := range keys {
		err := e.reevaluate(key, cause(key))
		if err == nil {
			continue
		}
		e.log.Error("reading verdicts failed; keeping the previous decision and retrying", "indicator", key, "error", err)
		if firstErr == nil {
			firstErr = err
		}
		e.remark(map[string]string{key: cause(key)})
	}
	e.setErr(firstErr)
}

// reevaluate decides on the indicator with key from the store's current
// state. Callers hold workMu.
func (e *Engine) reevaluate(key, cause string) error {
	now := e.opts.Now()
	verdicts, err := e.store.ActiveVerdicts(key, now)
	if err != nil {
		return fmt.Errorf("read verdicts of %s: %w", key, err)
	}
	ind, ok := e.indicatorOf(key, verdicts)
	if !ok {
		return nil // no verdicts or override before or now: nothing to decide
	}
	e.apply(key, e.decide(ind, verdicts, now), cause)
	return nil
}

// indicatorOf returns the indicator with key, from its verdicts, the kept
// decision or its override.
func (e *Engine) indicatorOf(key string, verdicts []*obieproto.Event) (obieproto.Indicator, bool) {
	if len(verdicts) > 0 {
		return verdicts[0].Indicator, true
	}
	e.mu.RLock()
	d, ok := e.decisions[key]
	e.mu.RUnlock()
	if ok {
		return d.Indicator, true
	}
	_, r := e.current()
	o, ok := r.Overrides.Get(key)
	return o.Indicator, ok
}

// apply keeps d as the decision of key and notifies subscribers if the
// block changed. Decisions without active verdicts are kept only while
// they block (force-block). Callers hold workMu.
func (e *Engine) apply(key string, d Decision, cause string) {
	active := len(d.Publishers) > 0 || d.State == StateBlock
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
	return !a.ExpiresAt.Equal(b.ExpiresAt) || a.Score != b.Score || a.Contributors != b.Contributors ||
		a.Autoblock != b.Autoblock || a.Sovereignty.Rule != b.Sovereignty.Rule
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
