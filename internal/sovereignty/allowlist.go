// Package sovereignty keeps the operator in charge of the node: the
// allow-list of addresses that are never blocked, and the rule that combines
// it with the operator's force-allow and force-block overrides. See
// ADR 0013.
package sovereignty

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Source says where an allow-list entry comes from.
type Source string

// Allow-list and override sources.
const (
	// SourceBuiltin: a range that is never blocked on any node.
	SourceBuiltin Source = "builtin"
	// SourceSelf: one of this node's own addresses.
	SourceSelf Source = "self"
	// SourceBootstrap: an address of a mesh.bootstrap peer.
	SourceBootstrap Source = "bootstrap"
	// SourceConfig: an entry of allowlist.cidrs.
	SourceConfig Source = "config"
	// SourceFile: a line of one of the allowlist.files.
	SourceFile Source = "file"
	// SourceOverride: an operator override.
	SourceOverride Source = "override"
)

// Protected reports whether entries of the source protect the node's own
// infrastructure, so that not even a force-block override beats them.
func (s Source) Protected() bool {
	return s == SourceBuiltin || s == SourceSelf || s == SourceBootstrap
}

// Entry is one allow-listed range.
type Entry struct {
	Prefix netip.Prefix
	Source Source
	// Label describes the entry: the name of a built-in range, the file and
	// line, or the configured address it was derived from.
	Label string
}

// builtin are the ranges no node ever blocks: its own infrastructure and
// address space that is not reachable on the public internet.
var builtin = []Entry{
	builtinEntry("0.0.0.0/8", "unspecified / this network"),
	builtinEntry("::/128", "unspecified"),
	builtinEntry("127.0.0.0/8", "loopback"),
	builtinEntry("::1/128", "loopback"),
	builtinEntry("10.0.0.0/8", "private (RFC 1918)"),
	builtinEntry("172.16.0.0/12", "private (RFC 1918)"),
	builtinEntry("192.168.0.0/16", "private (RFC 1918)"),
	builtinEntry("100.64.0.0/10", "shared address space (CGNAT)"),
	builtinEntry("169.254.0.0/16", "link-local"),
	builtinEntry("fe80::/10", "link-local"),
	builtinEntry("fc00::/7", "unique local (ULA)"),
	builtinEntry("224.0.0.0/4", "multicast"),
	builtinEntry("ff00::/8", "multicast"),
	builtinEntry("255.255.255.255/32", "limited broadcast"),
	builtinEntry("::ffff:0:0/96", "IPv4-mapped"),
}

// documentation are the built-in documentation ranges; multi-node tests
// leave them out (Env.OmitDocumentationRanges) to use them as indicators.
var documentation = []Entry{
	builtinEntry("192.0.2.0/24", "documentation (TEST-NET-1)"),
	builtinEntry("198.51.100.0/24", "documentation (TEST-NET-2)"),
	builtinEntry("203.0.113.0/24", "documentation (TEST-NET-3)"),
	builtinEntry("2001:db8::/32", "documentation"),
	builtinEntry("3fff::/20", "documentation"),
}

func builtinEntry(cidr, label string) Entry {
	return Entry{Prefix: netip.MustParsePrefix(cidr), Source: SourceBuiltin, Label: label}
}

// Builtin returns the built-in allow-list entries.
func Builtin() []Entry {
	return slices.Concat(builtin, documentation)
}

// Allowlist is an immutable set of allow-listed ranges. The zero value and
// nil allow nothing.
type Allowlist struct {
	protected []Entry
	operator  []Entry
}

// NewAllowlist returns the allow-list of entries; the built-in entries are
// not added implicitly. Host bits of the prefixes are masked.
func NewAllowlist(entries ...Entry) *Allowlist {
	a := &Allowlist{}
	for _, e := range entries {
		e.Prefix = e.Prefix.Masked()
		if e.Source.Protected() {
			a.protected = append(a.protected, e)
		} else {
			a.operator = append(a.operator, e)
		}
	}
	return a
}

// Entries returns every entry, the protected ones first.
func (a *Allowlist) Entries() []Entry {
	if a == nil {
		return nil
	}
	return append(slices.Clone(a.protected), a.operator...)
}

// MatchProtected returns the first built-in, own or bootstrap entry that
// overlaps p.
func (a *Allowlist) MatchProtected(p netip.Prefix) (Entry, bool) {
	if a == nil {
		return Entry{}, false
	}
	return firstOverlap(a.protected, p)
}

// MatchOperator returns the first allowlist.cidrs or allowlist.files entry
// that overlaps p.
func (a *Allowlist) MatchOperator(p netip.Prefix) (Entry, bool) {
	if a == nil {
		return Entry{}, false
	}
	return firstOverlap(a.operator, p)
}

// Match returns the first entry that overlaps p, protected entries first.
func (a *Allowlist) Match(p netip.Prefix) (Entry, bool) {
	if e, ok := a.MatchProtected(p); ok {
		return e, true
	}
	return a.MatchOperator(p)
}

// firstOverlap returns the first entry overlapping p: an address inside the
// entry, a range inside it or a range containing it. Blocking a range that
// contains an allow-listed address would block that address too.
func firstOverlap(entries []Entry, p netip.Prefix) (Entry, bool) {
	for _, e := range entries {
		if e.Prefix.Overlaps(p) {
			return e, true
		}
	}
	return Entry{}, false
}

// describe says what an entry is, for explanations.
func (e Entry) describe() string {
	var what string
	switch e.Source {
	case SourceBuiltin:
		what = "built-in range"
	case SourceSelf:
		what = "this node's address"
	case SourceBootstrap:
		what = "bootstrap peer address"
	case SourceConfig:
		what = "allowlist.cidrs entry"
	case SourceFile:
		what = "allow-list file entry"
	default:
		what = string(e.Source) + " entry"
	}
	s := what + " " + e.Prefix.String()
	if e.Label != "" {
		s += " (" + e.Label + ")"
	}
	return s
}

// ParseEntry parses an IP address or a CIDR range without host bits.
func ParseEntry(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid CIDR %q: want e.g. 192.0.2.0/24 or 2001:db8::/32", s)
		}
		if masked := p.Masked(); masked != p {
			return netip.Prefix{}, fmt.Errorf("CIDR %q has host bits set; did you mean %s?", s, masked)
		}
		return p, nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil || addr.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("invalid address %q: want an IP address or CIDR range", s)
	}
	return netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()), nil
}
