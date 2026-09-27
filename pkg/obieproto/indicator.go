package obieproto

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Minimum CIDR prefix lengths; broader ranges are rejected.
const (
	MinIPv4Prefix = 16
	MinIPv6Prefix = 32
)

// globalUnicastIPv6 is the only IPv6 space an indicator may come from. An
// allow-list is used because IPv6 has many encodings of internal addresses
// outside it (IPv4-compatible, IPv4-mapped/translated, NAT64 of private
// space, ULA, link-local, multicast, unallocated space).
var globalUnicastIPv6 = netip.MustParsePrefix("2000::/3")

// nonPublicRanges are special-purpose ranges that a node must never publish:
// for IPv4 unspecified/"this network", private (RFC 1918), shared address
// space, loopback, link-local, multicast and reserved space; for IPv6 the
// special-purpose blocks inside [globalUnicastIPv6].
var nonPublicRanges = mustPrefixes(
	"0.0.0.0/8",      // this network, includes unspecified
	"10.0.0.0/8",     // RFC 1918
	"100.64.0.0/10",  // shared address space (CGNAT)
	"127.0.0.0/8",    // loopback
	"169.254.0.0/16", // link-local
	"172.16.0.0/12",  // RFC 1918
	"192.0.0.0/24",   // IETF protocol assignments
	"192.88.99.0/24", // deprecated 6to4 relay anycast
	"192.168.0.0/16", // RFC 1918
	"198.18.0.0/15",  // benchmarking
	"224.0.0.0/4",    // multicast
	"240.0.0.0/4",    // reserved, includes limited broadcast
	"2001::/23",      // IETF protocol assignments (Teredo, benchmarking, ...)
	"2002::/16",      // 6to4, may wrap private IPv4 addresses
)

// documentationRanges are rejected unless [AllowDocumentationRanges] is set.
var documentationRanges = mustPrefixes(
	"192.0.2.0/24",    // TEST-NET-1
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"2001:db8::/32",   // RFC 3849
	"3fff::/20",       // RFC 9637
)

func mustPrefixes(ss ...string) []netip.Prefix {
	ps := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		ps[i] = netip.MustParsePrefix(s)
	}
	return ps
}

// Normalize rewrites the indicator into canonical form: lower-case kind,
// canonical address text (RFC 5952 for IPv6), the network address for CIDR
// ranges and the derived scope. It does not check whether the address is
// public; [Event.Validate] does.
func (i *Indicator) Normalize() error {
	kind := strings.ToLower(strings.TrimSpace(i.Kind))
	prefix, err := parseIndicator(kind, strings.TrimSpace(i.Value))
	if err != nil {
		return err
	}
	i.Kind = kind
	i.Value = formatIndicator(kind, prefix)
	i.Scope = scopeOf(prefix)
	return nil
}

// Normalize normalizes the event's indicator; see [Indicator.Normalize].
func (e *Event) Normalize() error {
	return e.Indicator.Normalize()
}

// validate checks that the indicator is canonical, its scope matches and it
// lies entirely in public address space.
func (i Indicator) validate(o options) error {
	kind := strings.ToLower(i.Kind)
	prefix, err := parseIndicator(kind, i.Value)
	if err != nil {
		return err
	}
	if i.Kind != kind {
		return invalid("indicator.kind", "%q is not canonical, want %q", i.Kind, kind)
	}
	if canonical := formatIndicator(kind, prefix); i.Value != canonical {
		return invalid("indicator.value", "%q is not canonical, want %q", i.Value, canonical)
	}
	if scope := scopeOf(prefix); i.Scope != scope {
		return invalid("indicator.scope", "%q does not match value, want %q", i.Scope, scope)
	}
	return checkPublic(prefix, o)
}

// parseIndicator parses value according to kind and returns it as a prefix
// (full-length for single addresses).
func parseIndicator(kind, value string) (netip.Prefix, error) {
	switch kind {
	case KindIPv4, KindIPv6:
		addr, err := netip.ParseAddr(value)
		if err != nil || addr.Zone() != "" || addr.Is4() != (kind == KindIPv4) {
			return netip.Prefix{}, invalid("indicator.value", "%q is not an %s address", value, kind)
		}
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	case KindCIDR:
		return parseCIDR(value)
	case "":
		return netip.Prefix{}, invalid("indicator.kind", "missing")
	default:
		return netip.Prefix{}, &FieldError{Field: "indicator.kind", Err: ErrUnsupportedIndicator, Detail: fmt.Sprintf("%q", kind)}
	}
}

func parseCIDR(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Prefix{}, invalid("indicator.value", "%q is not a CIDR range", value)
	}
	minBits := MinIPv6Prefix
	if prefix.Addr().Is4() {
		minBits = MinIPv4Prefix
	}
	bits, maxBits := prefix.Bits(), prefix.Addr().BitLen()
	switch {
	case bits < minBits:
		return netip.Prefix{}, invalid("indicator.value", "%q is broader than /%d", value, minBits)
	case bits == maxBits:
		return netip.Prefix{}, invalid("indicator.value", "%q is a single address, use kind ipv4 or ipv6", value)
	}
	return prefix.Masked(), nil
}

func formatIndicator(kind string, prefix netip.Prefix) string {
	if kind == KindCIDR {
		return prefix.Masked().String()
	}
	return prefix.Addr().String()
}

func scopeOf(prefix netip.Prefix) string {
	return "/" + strconv.Itoa(prefix.Bits())
}

// checkPublic rejects a prefix that overlaps any non-public range. A CIDR
// range that merely contains such a range is rejected too, because acting on
// it would affect internal addresses.
func checkPublic(prefix netip.Prefix, o options) error {
	inGlobal := prefix.Bits() >= globalUnicastIPv6.Bits() && globalUnicastIPv6.Contains(prefix.Addr())
	if prefix.Addr().Is6() && !inGlobal {
		return nonPublic(prefix, globalUnicastIPv6, "lies outside global unicast space")
	}
	for _, r := range nonPublicRanges {
		if prefix.Overlaps(r) {
			return nonPublic(prefix, r, "overlaps special-purpose range")
		}
	}
	if o.allowDocumentation {
		return nil
	}
	for _, r := range documentationRanges {
		if prefix.Overlaps(r) {
			return nonPublic(prefix, r, "overlaps special-purpose range")
		}
	}
	return nil
}

func nonPublic(prefix, r netip.Prefix, reason string) error {
	return &FieldError{
		Field:  "indicator.value",
		Err:    ErrNonPublicIndicator,
		Detail: fmt.Sprintf("%s %s %s", prefix, reason, r),
	}
}
