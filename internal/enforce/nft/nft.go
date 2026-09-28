// Package nft is the nftables enforcement backend. It owns the table
// `inet obie` and never touches any other table: two interval sets with
// per-element timeouts, obie_v4 and obie_v6, and a filter chain `input`
// (optionally also `forward`) at priority -10 that drops their sources.
// It talks netlink directly (github.com/google/nftables) and needs
// CAP_NET_ADMIN only. See ADR 0015.
package nft

import (
	"errors"
	"time"
)

// Names of what the backend creates.
const (
	Table        = "obie"
	SetV4        = "obie_v4"
	SetV6        = "obie_v6"
	ChainInput   = "input"
	ChainForward = "forward"
	// Priority runs the chains before the filter chains at priority 0 of
	// other tables.
	Priority = -10
)

// MaxChunk is the most set elements sent in one netlink message; a
// transaction holds as many messages as needed.
const MaxChunk = 1000

// Options configures the backend.
type Options struct {
	// Forward adds the forward chain (enforce.nftables.forward).
	Forward bool
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// NetNS is a file descriptor of the network namespace to program
	// instead of obied's own; 0 for obied's own. Only multi-node tests on
	// one host set it, to give every node a firewall of its own.
	NetNS int
}

// ErrPermission is returned when the kernel refuses the netlink requests.
var ErrPermission = errors.New("nftables access denied: obied needs CAP_NET_ADMIN " +
	"(run it as root or grant the capability, e.g. AmbientCapabilities=CAP_NET_ADMIN in the systemd unit)")

// ErrDrift is returned by List when the table is missing or was changed
// outside obied; the next Setup restores it.
var ErrDrift = errors.New("table inet " + Table + " is missing or was changed outside obied")

// never is the expiry reported for elements without a timeout, which
// obied never adds: far enough out that the reconciler replaces them.
const never = 100 * 365 * 24 * time.Hour
