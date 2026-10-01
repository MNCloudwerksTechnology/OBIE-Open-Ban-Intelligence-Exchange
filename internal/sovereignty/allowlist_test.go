package sovereignty

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// TestBuiltinRanges checks every built-in range with an address inside,
// at its edges and just outside.
func TestBuiltinRanges(t *testing.T) {
	a := NewAllowlist(Builtin()...)
	tests := []struct {
		label   string
		inside  []string
		outside []string
	}{
		{"unspecified / this network", []string{"0.0.0.0", "0.255.255.255"}, []string{"1.0.0.0"}},
		{"unspecified", []string{"::"}, nil},
		{"loopback", []string{"127.0.0.1", "127.255.255.255", "::1"}, []string{"128.0.0.0", "::2"}},
		{"private (RFC 1918)", []string{"10.0.0.0", "10.255.255.255", "172.16.0.1", "172.31.255.255", "192.168.0.1", "192.168.255.255"},
			[]string{"9.255.255.255", "11.0.0.0", "172.15.255.255", "172.32.0.0", "192.167.255.255", "192.169.0.0"}},
		{"shared address space (CGNAT)", []string{"100.64.0.0", "100.127.255.255"}, []string{"100.63.255.255", "100.128.0.0"}},
		{"link-local", []string{"169.254.0.1", "169.254.255.255", "fe80::1", "febf:ffff::1"}, []string{"169.253.255.255", "169.255.0.0", "fec0::1"}},
		{"unique local (ULA)", []string{"fc00::1", "fd12:3456::1", "fdff:ffff::1"}, []string{"fbff:ffff::1", "fe00::1"}},
		{"multicast", []string{"224.0.0.1", "239.255.255.255", "ff02::1", "ff0e::1"}, []string{"223.255.255.255", "feff::1"}},
		{"limited broadcast", []string{"255.255.255.255"}, []string{"255.255.255.254"}},
		{"IPv4-mapped", []string{"::ffff:1.2.3.4"}, nil},
		{"documentation (TEST-NET-1)", []string{"192.0.2.0", "192.0.2.255"}, []string{"192.0.1.255", "192.0.3.0"}},
		{"documentation (TEST-NET-2)", []string{"198.51.100.7"}, []string{"198.51.101.0"}},
		{"documentation (TEST-NET-3)", []string{"203.0.113.7"}, []string{"203.0.114.0"}},
		{"documentation", []string{"2001:db8::1", "2001:db8:ffff::1", "3fff::1", "3fff:fff::1"}, []string{"2001:db9::1", "3fff:1000::1"}},
	}
	for _, tt := range tests {
		for _, s := range tt.inside {
			e, ok := a.MatchProtected(netip.MustParsePrefix(addrPrefix(s)))
			if !ok || e.Source != SourceBuiltin {
				t.Errorf("%s: not allow-listed", s)
				continue
			}
			if !strings.HasPrefix(e.Label, strings.Split(tt.label, " (")[0]) {
				t.Errorf("%s: matched %q, want %q", s, e.Label, tt.label)
			}
		}
		for _, s := range tt.outside {
			if e, ok := a.Match(netip.MustParsePrefix(addrPrefix(s))); ok {
				t.Errorf("%s: allow-listed by %v", s, e)
			}
		}
	}
	// Public addresses are never on the built-in list.
	for _, s := range []string{"1.1.1.1", "8.8.8.8", "185.1.2.3", "2a00:1450::1", "2606:4700::1111"} {
		if e, ok := a.Match(netip.MustParsePrefix(addrPrefix(s))); ok {
			t.Errorf("%s: allow-listed by %v", s, e)
		}
	}
	if n := len(Builtin()); n != 20 {
		t.Errorf("Builtin() has %d entries; update this test when adding ranges", n)
	}
}

func addrPrefix(s string) string {
	addr := netip.MustParseAddr(s)
	return netip.PrefixFrom(addr, addr.BitLen()).String()
}

// TestOverlap checks that a CIDR overlapping an allow-listed range in any
// way matches, and that adjacent ranges do not.
func TestOverlap(t *testing.T) {
	a := NewAllowlist(
		Entry{Prefix: netip.MustParsePrefix("198.18.4.0/24"), Source: SourceConfig},
		Entry{Prefix: netip.MustParsePrefix("2a01:4f8:1::/48"), Source: SourceConfig},
		Entry{Prefix: netip.MustParsePrefix("185.10.10.10/32"), Source: SourceSelf},
	)
	tests := []struct {
		indicator string
		want      string
	}{
		{"198.18.4.1/32", "198.18.4.0/24"},    // address inside
		{"198.18.4.128/25", "198.18.4.0/24"},  // range inside
		{"198.18.0.0/16", "198.18.4.0/24"},    // range containing the entry
		{"198.18.4.0/24", "198.18.4.0/24"},    // identical
		{"198.18.5.0/24", ""},                 // adjacent after
		{"198.18.3.0/24", ""},                 // adjacent before
		{"198.18.2.0/23", ""},                 // neighboring /23
		{"198.18.4.0/23", "198.18.4.0/24"},    // /23 starting at the entry
		{"185.10.10.0/24", "185.10.10.10/32"}, // range containing an own address
		{"185.10.11.0/24", ""},
		{"2a01:4f8:1:2::/64", "2a01:4f8:1::/48"},
		{"2a01:4f8::/32", "2a01:4f8:1::/48"},
		{"2a01:4f8:2::/48", ""},
		{"::ffff:198.18.4.1/128", ""}, // other family: covered by the built-in IPv4-mapped entry instead
	}
	for _, tt := range tests {
		e, ok := a.Match(netip.MustParsePrefix(tt.indicator))
		got := ""
		if ok {
			got = e.Prefix.String()
		}
		if got != tt.want {
			t.Errorf("Match(%s) = %q, want %q", tt.indicator, got, tt.want)
		}
	}
	// Protected entries are matched first and separately.
	if e, ok := a.MatchProtected(netip.MustParsePrefix("185.10.0.0/16")); !ok || e.Source != SourceSelf {
		t.Errorf("MatchProtected = %v, %v", e, ok)
	}
	if _, ok := a.MatchProtected(netip.MustParsePrefix("198.18.4.1/32")); ok {
		t.Error("an allowlist.cidrs entry matched as protected")
	}
	var none *Allowlist
	if _, ok := none.Match(netip.MustParsePrefix("10.0.0.1/32")); ok {
		t.Error("nil allow-list matched")
	}
}

func TestParseEntry(t *testing.T) {
	tests := []struct{ in, want, err string }{
		{"192.0.2.0/24", "192.0.2.0/24", ""},
		{" 2001:db8::/32 ", "2001:db8::/32", ""},
		{"203.0.113.7", "203.0.113.7/32", ""},
		{"2001:db8::1", "2001:db8::1/128", ""},
		{"::ffff:203.0.113.7", "203.0.113.7/32", ""},
		{"10.1.2.3/8", "", "did you mean 10.0.0.0/8?"},
		{"10.0.0.0/33", "", "invalid CIDR"},
		{"nope", "", "invalid address"},
		{"fe80::1%eth0", "", "invalid address"},
	}
	for _, tt := range tests {
		p, err := ParseEntry(tt.in)
		switch {
		case tt.err != "":
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("ParseEntry(%q) error = %v, want %q", tt.in, err, tt.err)
			}
		case err != nil || p.String() != tt.want:
			t.Errorf("ParseEntry(%q) = %v, %v, want %s", tt.in, p, err, tt.want)
		}
	}
}

func TestOverlapping(t *testing.T) {
	a := NewAllowlist(
		Entry{Prefix: netip.MustParsePrefix("185.0.0.0/16"), Source: SourceConfig},
		Entry{Prefix: netip.MustParsePrefix("185.0.1.0/24"), Source: SourceFile, Label: "allow.txt:1"},
		Entry{Prefix: netip.MustParsePrefix("185.0.1.7/32"), Source: SourceBootstrap},
		Entry{Prefix: netip.MustParsePrefix("185.1.0.0/16"), Source: SourceConfig},
	)
	var got []string
	for _, e := range a.Overlapping(netip.MustParsePrefix("185.0.1.0/24")) {
		got = append(got, string(e.Source)+" "+e.Prefix.String())
	}
	want := []string{"bootstrap 185.0.1.7/32", "config 185.0.0.0/16", "file 185.0.1.0/24"}
	if !slices.Equal(got, want) {
		t.Errorf("Overlapping = %v, want %v", got, want)
	}
	var none *Allowlist
	if none.Overlapping(netip.MustParsePrefix("185.0.1.0/24")) != nil || none.Files() != nil || none.Warnings() != nil {
		t.Error("a nil allow-list holds something")
	}
}
