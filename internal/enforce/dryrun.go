package enforce

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"time"
)

// DryRun is the default backend: it keeps the entries in memory, expires
// them like the kernel would and logs every add and remove, but blocks
// nothing.
type DryRun struct {
	log *slog.Logger
	now func() time.Time

	mu      sync.Mutex
	entries map[netip.Prefix]time.Time
}

// NewDryRun returns a dry-run backend logging to log.
func NewDryRun(log *slog.Logger) *DryRun {
	return &DryRun{log: log, now: time.Now, entries: map[netip.Prefix]time.Time{}}
}

// Setup logs that nothing will be blocked.
func (d *DryRun) Setup(context.Context) error {
	d.log.Info("dryrun: enforcement backend ready; blocks are logged, nothing is blocked")
	return nil
}

// List returns the unexpired entries, ordered by prefix.
func (d *DryRun) List(context.Context) ([]Entry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expire()
	out := make([]Entry, 0, len(d.entries))
	for p, exp := range d.entries {
		out = append(out, Entry{Prefix: p, Expires: exp})
	}
	sortEntries(out)
	return out, nil
}

// Apply removes, then adds the entries, logging each.
func (d *DryRun) Apply(_ context.Context, add, remove []Entry) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	for _, e := range remove {
		delete(d.entries, e.Prefix)
		d.log.Info("dryrun: remove", "prefix", e.Prefix.String())
	}
	for _, e := range add {
		d.entries[e.Prefix] = e.Expires
		d.log.Info("dryrun: add", "prefix", e.Prefix.String(), "expires_at", e.Expires.UTC(),
			"timeout", e.Expires.Sub(now).Round(time.Second).String())
	}
	return nil
}

// Teardown drops every entry.
func (d *DryRun) Teardown(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log.Info("dryrun: teardown", "entries", len(d.entries))
	clear(d.entries)
	return nil
}

// expire drops the entries whose timeout ran out. Callers hold mu.
func (d *DryRun) expire() {
	now := d.now()
	for p, exp := range d.entries {
		if !now.Before(exp) {
			delete(d.entries, p)
		}
	}
}
