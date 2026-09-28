package enforce

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// Name is the subsystem name of the reconciler.
const Name = "enforce"

// Defaults of the Options a caller leaves zero.
const (
	// DefaultDebounce coalesces the block changes that arrive together
	// into one reconciliation.
	DefaultDebounce = 250 * time.Millisecond
	// DefaultMinBackoff is the first retry delay after a failure; it
	// doubles up to the reconcile interval.
	DefaultMinBackoff = time.Second
)

// ExpiryTolerance is how far an applied entry's expiry may drift from the
// decided one before the entry is replaced; it absorbs the rounding of
// backends that report remaining timeouts.
const ExpiryTolerance = 5 * time.Second

// MinTimeout is the shortest remaining timeout an entry is added with;
// blocks about to expire are left out rather than handed to a backend
// that may reject a zero timeout.
const MinTimeout = time.Second

// DeferDelay is the delay of the next pass after additions were deferred
// until the entries they overlap, with less than ExpiryTolerance left,
// expired.
const DeferDelay = ExpiryTolerance + time.Second

// PassTimeout bounds one reconciliation pass, so a hanging backend call
// fails, is retried and shows in the readiness.
const PassTimeout = 30 * time.Second

// Skip reasons of decided blocks that are not applied.
const (
	SkipAllowlist  = "allowlist"
	SkipMaxEntries = "max_entries"
)

// maxLoggedPrefixes bounds the prefixes named in one log line.
const maxLoggedPrefixes = 10

// Options configures a Reconciler.
type Options struct {
	// Backend names the enforcement backend in the status.
	Backend string
	// MaxEntries caps the applied entries; the lowest-score blocks beyond
	// it are skipped. Required.
	MaxEntries int
	// Interval is enforce.reconcile_interval. Required.
	Interval time.Duration
	// Debounce delays a reconciliation after a block change; DefaultDebounce
	// when zero.
	Debounce time.Duration
	// MinBackoff is the first retry delay; DefaultMinBackoff when zero.
	MinBackoff time.Duration
	// Allowlist returns the effective allow-list, checked again right
	// before every apply; nil checks nothing.
	Allowlist func() *sovereignty.Allowlist
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.Debounce <= 0 {
		o.Debounce = DefaultDebounce
	}
	if o.MinBackoff <= 0 {
		o.MinBackoff = DefaultMinBackoff
	}
	if o.Allowlist == nil {
		o.Allowlist = func() *sovereignty.Allowlist { return nil }
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// backendState is what the reconciler knows about the backend.
type backendState int

const (
	// backendUnknown: not touched yet; it may hold entries of an earlier run.
	backendUnknown backendState = iota
	backendSetUp
	backendTornDown
)

// Status is the reconciler's condition after its last pass.
type Status struct {
	// Mode is the mode of the last pass; empty before the first one.
	Mode config.Mode
	// Applied counts the entries applied after the last successful pass.
	Applied int
	// Skipped counts the decided blocks not applied, by skip reason.
	Skipped map[string]int
	// Failures counts the consecutive failed passes; 0 after a success.
	Failures int
	// Err is the error of the last failed pass.
	Err error
	// RetryIn is the delay before the next attempt after a failure.
	RetryIn time.Duration
}

// Reconciler is the enforcement subsystem. In enforce mode it makes the
// backend's entries match the Gate's blocks, on every notification
// (debounced) and every Interval: it computes the desired entries, diffs
// them against Enforcer.List and applies the minimal change. In observe
// mode it never sets up, lists or applies anything; it tears the backend
// down once, withdrawing what an earlier enforce run left behind.
type Reconciler struct {
	gate *Gate
	enf  Enforcer
	log  *slog.Logger
	opts Options
	kick chan struct{}

	// enfMu serializes the calls into the backend.
	enfMu   sync.Mutex
	backend backendState
	// skipped are the prefixes skipped in the last pass, by reason, so
	// only new ones are logged.
	skipped map[netip.Prefix]string
	// deferred is set when the last pass deferred additions until the
	// entries they overlap expired.
	deferred bool

	statusMu sync.Mutex
	status   Status

	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewReconciler returns the reconciler of gate's blocks into enf. The gate
// must be created with the reconciler's Trigger as its notify function.
func NewReconciler(gate *Gate, enf Enforcer, opts Options, log *slog.Logger) *Reconciler {
	return &Reconciler{
		gate:    gate,
		enf:     enf,
		log:     log,
		opts:    opts.withDefaults(),
		kick:    make(chan struct{}, 1),
		skipped: map[netip.Prefix]string{},
	}
}

// Name returns the subsystem name.
func (r *Reconciler) Name() string { return Name }

// Trigger requests a reconciliation within the debounce delay. It never
// blocks.
func (r *Reconciler) Trigger() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// Start starts the reconciliation loop; the first pass runs at once.
func (r *Reconciler) Start(context.Context) error {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	if r.cancel != nil {
		return errors.New("reconciler already started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan struct{})
	go r.loop(ctx, r.done)
	return nil
}

// Stop ends the loop, canceling a backend call in progress. The applied
// entries stay: they expire on their own, and the next start reconciles
// them.
func (r *Reconciler) Stop(ctx context.Context) error {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	if r.cancel == nil {
		return nil
	}
	r.cancel()
	done := r.done
	r.cancel, r.done = nil, nil
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("reconciler did not stop: %w", ctx.Err())
	}
}

// Status returns the condition after the last pass.
func (r *Reconciler) Status() Status {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	s := r.status
	s.Skipped = maps.Clone(s.Skipped)
	return s
}

// Ready reports the last failure while the loop retries it; a failed
// direct call of Reconcile is not counted.
func (r *Reconciler) Ready() error {
	s := r.Status()
	if s.Failures == 0 {
		return nil
	}
	return fmt.Errorf("enforcement failed %d time(s), retrying in %s: %w", s.Failures, s.RetryIn, s.Err)
}

// Detail summarizes the enforcement for the node status.
func (r *Reconciler) Detail() string {
	s := r.Status()
	switch s.Mode {
	case "":
		return "starting"
	case config.ModeObserve:
		if s.Failures > 0 {
			return "observing (withdrawing the applied blocks failed)"
		}
		return "observing"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "enforcing via %s: %d entries", r.opts.Backend, s.Applied)
	if n := s.Skipped[SkipMaxEntries]; n > 0 {
		fmt.Fprintf(&b, ", %d skipped over enforce.max_entries", n)
	}
	if n := s.Skipped[SkipAllowlist]; n > 0 {
		fmt.Fprintf(&b, ", %d refused by the allow-list", n)
	}
	return b.String()
}

// Entries lists the entries the backend currently applies; none once
// observe mode tore it down. It waits for a pass in progress, at most
// PassTimeout.
func (r *Reconciler) Entries(ctx context.Context) ([]Entry, error) {
	r.enfMu.Lock()
	defer r.enfMu.Unlock()
	if r.backend == backendTornDown {
		return nil, nil
	}
	return r.enf.List(ctx)
}

// loop reconciles at once, then after every notification (debounced) and
// every Interval; after a failure it retries with exponential backoff and
// ignores notifications until then (a mode switch then takes effect with
// the retry, at most Interval later).
func (r *Reconciler) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	timer := time.NewTimer(0)
	defer timer.Stop()
	next := time.Now()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.kick:
			if failures == 0 && time.Until(next) > r.opts.Debounce {
				timer.Reset(r.opts.Debounce)
				next = time.Now().Add(r.opts.Debounce)
			}
		case <-timer.C:
			delay := r.opts.Interval
			if err := r.pass(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				failures++
				delay = r.backoff(failures)
				r.log.Error("enforcement failed; retrying", "error", err, "failures", failures, "retry_in", delay.String())
			} else {
				failures = 0
				if r.deferredAdditions() {
					delay = min(delay, DeferDelay)
				}
			}
			r.setFailure(failures, delay)
			timer.Reset(delay)
			next = time.Now().Add(delay)
		}
	}
}

// deferredAdditions reports whether the last pass deferred additions.
func (r *Reconciler) deferredAdditions() bool {
	r.enfMu.Lock()
	defer r.enfMu.Unlock()
	return r.deferred
}

// pass runs Reconcile within PassTimeout.
func (r *Reconciler) pass(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, PassTimeout)
	defer cancel()
	return r.Reconcile(ctx)
}

// backoff returns the delay before retry n (from 1): MinBackoff doubling
// per failure, capped at Interval.
func (r *Reconciler) backoff(n int) time.Duration {
	d := r.opts.MinBackoff
	for i := 1; i < n && d < r.opts.Interval; i++ {
		d *= 2
	}
	return min(d, r.opts.Interval)
}

// Reconcile runs one pass. In enforce mode it sets the backend up if
// needed (at the first pass and after a failed one) and applies the difference between the desired and the listed
// entries; in observe mode it only tears the backend down, once.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.enfMu.Lock()
	defer r.enfMu.Unlock()
	mode := r.gate.Mode()
	if mode != config.ModeEnforce {
		return r.observe(ctx)
	}
	if r.backend != backendSetUp {
		if err := r.enf.Setup(ctx); err != nil {
			return r.fail(mode, fmt.Errorf("set up the %s backend: %w", r.opts.Backend, err))
		}
		r.backend = backendSetUp
	}
	want, skipped := r.desired(r.opts.Now())
	have, err := r.enf.List(ctx)
	if err != nil {
		r.backend = backendUnknown // e.g. the table was changed by hand
		return r.fail(mode, fmt.Errorf("list the applied entries: %w", err))
	}
	add, remove := Diff(want, have)
	add, remove, deferred := settle(add, remove, have, r.opts.Now())
	r.deferred = deferred > 0
	if deferred > 0 {
		r.log.Debug("additions overlapping entries about to expire are deferred", "deferred", deferred)
	}
	if len(add) > 0 || len(remove) > 0 {
		if err := r.enf.Apply(ctx, add, remove); err != nil {
			r.backend = backendUnknown
			return r.fail(mode, fmt.Errorf("apply %d additions and %d removals: %w", len(add), len(remove), err))
		}
		r.log.Info("enforcement reconciled", "added", len(add), "removed", len(remove), "entries", len(want))
	}
	r.succeed(mode, len(want), skipped)
	return nil
}

// observe withdraws the entries once after the switch to observe mode (or
// at the first pass, for entries an earlier run left behind). Callers hold
// enfMu.
func (r *Reconciler) observe(ctx context.Context) error {
	if r.backend != backendTornDown {
		if err := r.enf.Teardown(ctx); err != nil {
			return r.fail(config.ModeObserve, fmt.Errorf("tear down the %s backend: %w", r.opts.Backend, err))
		}
		if r.backend == backendSetUp {
			r.log.Warn("observe mode: every applied block was withdrawn")
		}
		r.backend = backendTornDown
	}
	clear(r.skipped)
	r.deferred = false
	r.succeed(config.ModeObserve, 0, nil)
	return nil
}

// candidate is a decided block that may become an entry.
type candidate struct {
	entry Entry
	score float64
	// forced is set for the operator's force-blocks, which the cap keeps
	// first whatever their score.
	forced bool
}

// desired returns the entries to apply at now: the blocks of the gate
// with at least MinTimeout left that the allow-list does not refuse, the
// operator's force-blocks and then the highest scores first up to
// MaxEntries, without those inside a wider one, ordered by prefix. It logs newly skipped blocks and
// counts the skipped ones by reason. Callers hold enfMu.
func (r *Reconciler) desired(now time.Time) ([]Entry, map[string]int) {
	allow := r.opts.Allowlist()
	byPrefix := map[netip.Prefix]candidate{}
	skipped := map[netip.Prefix]string{}
	for _, d := range r.gate.Blocks() {
		if d.State != decision.StateBlock || d.ExpiresAt.Sub(now) < MinTimeout {
			continue
		}
		p, err := sovereignty.PrefixOf(d.Indicator)
		if err != nil {
			r.log.Warn("block decision without an address range, not applied", "indicator", d.Indicator.Key(), "error", err)
			continue
		}
		if entry, refused := refusedBy(allow, p, &d); refused {
			if _, known := r.skipped[p]; !known {
				r.log.Warn("block decision refused by the allow-list right before apply", "prefix", p.String(),
					"indicator", d.Indicator.Key(), "allowlist_entry", entry.Prefix.String(), "source", entry.Source)
			}
			skipped[p] = SkipAllowlist
			continue
		}
		c := candidate{entry: Entry{Prefix: p, Expires: d.ExpiresAt}, score: d.Score,
			forced: d.Sovereignty.Rule == sovereignty.RuleForceBlock}
		if old, dup := byPrefix[p]; dup { // e.g. ipv4:192.0.2.1 and cidr:192.0.2.1/32
			c.entry.Expires = later(old.entry.Expires, c.entry.Expires)
			c.score = max(old.score, c.score)
			c.forced = c.forced || old.forced
		}
		byPrefix[p] = c
	}
	for p := range byPrefix { // applied through another indicator
		delete(skipped, p)
	}
	cands := make([]candidate, 0, len(byPrefix))
	for _, c := range byPrefix {
		cands = append(cands, c)
	}
	cands = mergeCovered(cands)
	slices.SortFunc(cands, func(a, b candidate) int {
		return cmp.Or(compareForced(a.forced, b.forced), cmp.Compare(b.score, a.score), comparePrefix(a.entry.Prefix, b.entry.Prefix))
	})
	if len(cands) > r.opts.MaxEntries {
		var fresh []string
		for _, c := range cands[r.opts.MaxEntries:] {
			if _, known := r.skipped[c.entry.Prefix]; !known && len(fresh) < maxLoggedPrefixes {
				fresh = append(fresh, c.entry.Prefix.String())
			}
			skipped[c.entry.Prefix] = SkipMaxEntries
		}
		if len(fresh) > 0 {
			r.log.Warn("enforce.max_entries reached; the lowest-score blocks are not applied",
				"max_entries", r.opts.MaxEntries, "skipped", len(cands)-r.opts.MaxEntries, "newly_skipped_sample", fresh)
		}
		cands = cands[:r.opts.MaxEntries]
	}
	r.skipped = skipped
	want := make([]Entry, len(cands))
	for i, c := range cands {
		want[i] = c.entry
	}
	sortEntries(want)
	counts := map[string]int{}
	for _, reason := range skipped {
		counts[reason]++
	}
	return want, counts
}

// mergeCovered leaves out the candidates inside a wider one, since an
// nftables interval set cannot hold overlapping ranges; the wider one
// takes on their priority (the highest score, force-block if any), so the
// cap weighs it like the blocks it stands for. A covered block that
// outlives the wider one returns with the first pass after the wider one
// ended.
func mergeCovered(cands []candidate) []candidate {
	slices.SortFunc(cands, func(a, b candidate) int { return comparePrefix(a.entry.Prefix, b.entry.Prefix) })
	out := cands[:0]
	for _, c := range cands {
		// Sorted by prefix, the kept ones are disjoint and a covering one
		// comes right before what it covers.
		if n := len(out); n > 0 && out[n-1].entry.Prefix.Contains(c.entry.Prefix.Addr()) {
			out[n-1].score = max(out[n-1].score, c.score)
			out[n-1].forced = out[n-1].forced || c.forced
			continue
		}
		out = append(out, c)
	}
	return out
}

// settle adjusts the difference to what the backend holds at now. An
// applied entry with less than ExpiryTolerance left is never removed: it
// may expire before the removal reaches the backend, which then fails the
// whole change. Renewing such an entry is a plain addition, which updates
// its expiry. An addition overlapping another entry that stays applied,
// e.g. a /25 under a /24 about to expire, is deferred: interval sets
// reject overlapping ranges. It returns the number of deferred additions.
func settle(add, remove, have []Entry, now time.Time) (keptAdd, keptRemove []Entry, deferred int) {
	removed := make(map[netip.Prefix]bool, len(remove))
	for _, e := range remove {
		if e.Expires.Sub(now) >= ExpiryTolerance {
			removed[e.Prefix] = true
			keptRemove = append(keptRemove, e)
		}
	}
	var stay []netip.Prefix
	staying := map[netip.Prefix]bool{}
	for _, e := range have {
		if !removed[e.Prefix] {
			stay = append(stay, e.Prefix)
			staying[e.Prefix] = true
		}
	}
	slices.SortFunc(stay, comparePrefix)
	for _, e := range add {
		// Applied prefixes are disjoint: one staying under e's own prefix
		// overlaps no other.
		if !staying[e.Prefix] && overlaps(e.Prefix, stay, staying) {
			deferred++
			continue
		}
		keptAdd = append(keptAdd, e)
	}
	return keptAdd, keptRemove, deferred
}

// overlaps reports whether p shares an address with one of the disjoint,
// sorted prefixes in sorted, which set holds too.
func overlaps(p netip.Prefix, sorted []netip.Prefix, set map[netip.Prefix]bool) bool {
	for bits := p.Bits(); bits >= 0; bits-- { // p or a prefix containing it
		if set[netip.PrefixFrom(p.Addr(), bits).Masked()] {
			return true
		}
	}
	// A prefix inside p is the first one at or after p's address.
	i, _ := slices.BinarySearchFunc(sorted, p, comparePrefix)
	return i < len(sorted) && p.Contains(sorted[i].Addr())
}

// compareForced orders force-blocks first.
func compareForced(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	default:
		return 1
	}
}

// refusedBy returns the allow-list entry that forbids blocking p: any
// protected entry (built-in, own and bootstrap addresses), and an operator
// entry unless the block is the operator's own force-block, which takes
// precedence over allowlist.cidrs and files (ADR 0013).
func refusedBy(allow *sovereignty.Allowlist, p netip.Prefix, d *decision.Decision) (sovereignty.Entry, bool) {
	if e, ok := allow.MatchProtected(p); ok {
		return e, true
	}
	if d.Sovereignty.Rule == sovereignty.RuleForceBlock {
		return sovereignty.Entry{}, false
	}
	return allow.MatchOperator(p)
}

// Diff returns the entries to add and to remove to turn have into want. An
// entry whose expiry is off by more than ExpiryTolerance is in both, so it
// is replaced with the decided expiry. Both results are ordered by prefix.
func Diff(want, have []Entry) (add, remove []Entry) {
	applied := make(map[netip.Prefix]Entry, len(have))
	for _, e := range have {
		applied[e.Prefix] = e
	}
	for _, w := range want {
		h, ok := applied[w.Prefix]
		delete(applied, w.Prefix)
		switch {
		case !ok:
			add = append(add, w)
		case w.Expires.Sub(h.Expires).Abs() > ExpiryTolerance:
			add = append(add, w)
			remove = append(remove, h)
		}
	}
	for _, h := range applied {
		remove = append(remove, h)
	}
	sortEntries(add)
	sortEntries(remove)
	return add, remove
}

// fail records a failed pass in mode and returns err.
func (r *Reconciler) fail(mode config.Mode, err error) error {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Mode, r.status.Err = mode, err
	return err
}

// succeed records a successful pass.
func (r *Reconciler) succeed(mode config.Mode, applied int, skipped map[string]int) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status = Status{Mode: mode, Applied: applied, Skipped: skipped}
	setMetrics(mode, applied, skipped)
}

// setFailure records the consecutive failures and the retry delay.
func (r *Reconciler) setFailure(failures int, retryIn time.Duration) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Failures = failures
	if failures == 0 {
		r.status.Err, r.status.RetryIn = nil, 0
		return
	}
	r.status.RetryIn = retryIn
	applyFailuresTotal.Inc()
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
