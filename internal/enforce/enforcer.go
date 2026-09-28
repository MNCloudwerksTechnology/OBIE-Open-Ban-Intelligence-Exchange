package enforce

import (
	"context"
	"net/netip"
	"slices"
	"time"
)

// Entry is one applied block: every address in Prefix is dropped until
// Expires.
type Entry struct {
	Prefix  netip.Prefix
	Expires time.Time
}

// Enforcer is an enforcement backend, e.g. an nftables set. The Reconciler
// is its only caller and never calls it concurrently.
type Enforcer interface {
	// Setup creates what the backend needs, e.g. its own table; it must be
	// idempotent and keep entries that already exist.
	Setup(ctx context.Context) error
	// List returns the entries currently applied, without expired ones.
	List(ctx context.Context) ([]Entry, error)
	// Apply removes the entries in remove, then adds the ones in add, each
	// with its remaining timeout (Expires - now), so the backend drops
	// them on its own even if obied dies. An entry may be in both: it is
	// replaced with the new expiry. Adding an applied entry again updates
	// its expiry (the nftables backend needs a kernel that updates element
	// timeouts; otherwise the entry expires early and the next pass adds
	// it again).
	Apply(ctx context.Context, add, remove []Entry) error
	// Teardown removes everything Setup created, with every entry.
	Teardown(ctx context.Context) error
}

// comparePrefix orders prefixes: IPv4 before IPv6, then by address and
// length.
func comparePrefix(a, b netip.Prefix) int {
	if c := a.Addr().Compare(b.Addr()); c != 0 {
		return c
	}
	return a.Bits() - b.Bits()
}

// sortEntries orders entries by prefix.
func sortEntries(entries []Entry) {
	slices.SortFunc(entries, func(a, b Entry) int { return comparePrefix(a.Prefix, b.Prefix) })
}
