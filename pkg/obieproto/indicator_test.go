package obieproto

import (
	"errors"
	"testing"
)

func TestIndicatorNormalize(t *testing.T) {
	tests := []struct {
		name    string
		in      Indicator
		want    Indicator
		wantErr error
	}{
		{
			name: "ipv4 unchanged",
			in:   Indicator{Kind: "ipv4", Value: "85.10.20.30"},
			want: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"},
		},
		{
			name: "kind lower-cased and whitespace trimmed",
			in:   Indicator{Kind: " IPv4 ", Value: " 85.10.20.30 "},
			want: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"},
		},
		{
			name: "ipv6 canonical form",
			in:   Indicator{Kind: "IPV6", Value: "2A01:04F8:0000:0000:0000:0000:0000:0001"},
			want: Indicator{Kind: KindIPv6, Value: "2a01:4f8::1", Scope: "/128"},
		},
		{
			name: "ipv6 with embedded dotted quad",
			in:   Indicator{Kind: "ipv6", Value: "2a01:4f8::85.10.20.30"},
			want: Indicator{Kind: KindIPv6, Value: "2a01:4f8::550a:141e", Scope: "/128"},
		},
		{
			name: "wrong scope is replaced",
			in:   Indicator{Kind: "ipv4", Value: "85.10.20.30", Scope: "/24"},
			want: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"},
		},
		{
			name: "cidr masked to network address",
			in:   Indicator{Kind: "cidr", Value: "85.10.20.30/16"},
			want: Indicator{Kind: KindCIDR, Value: "85.10.0.0/16", Scope: "/16"},
		},
		{
			name: "ipv6 cidr masked and canonical",
			in:   Indicator{Kind: "CIDR", Value: "2A01:4F8:ABCD::1/48"},
			want: Indicator{Kind: KindCIDR, Value: "2a01:4f8:abcd::/48", Scope: "/48"},
		},
		{name: "unsupported kind fqdn", in: Indicator{Kind: "fqdn", Value: "evil.example"}, wantErr: ErrUnsupportedIndicator},
		{name: "unsupported kind ja3", in: Indicator{Kind: "ja3", Value: "abc"}, wantErr: ErrUnsupportedIndicator},
		{name: "missing kind", in: Indicator{Value: "85.10.20.30"}, wantErr: ErrInvalidField},
		{name: "garbage value", in: Indicator{Kind: "ipv4", Value: "not-an-ip"}, wantErr: ErrInvalidField},
		{name: "ipv4 with leading zeros", in: Indicator{Kind: "ipv4", Value: "085.10.20.30"}, wantErr: ErrInvalidField},
		{name: "ipv6 value for ipv4 kind", in: Indicator{Kind: "ipv4", Value: "2a01:4f8::1"}, wantErr: ErrInvalidField},
		{name: "ipv4-mapped value for ipv4 kind", in: Indicator{Kind: "ipv4", Value: "::ffff:85.10.20.30"}, wantErr: ErrInvalidField},
		{name: "ipv4 value for ipv6 kind", in: Indicator{Kind: "ipv6", Value: "85.10.20.30"}, wantErr: ErrInvalidField},
		{name: "ipv6 with zone", in: Indicator{Kind: "ipv6", Value: "2a01:4f8::1%eth0"}, wantErr: ErrInvalidField},
		{name: "cidr without prefix", in: Indicator{Kind: "cidr", Value: "85.10.0.0"}, wantErr: ErrInvalidField},
		{name: "ipv4 cidr broader than /16", in: Indicator{Kind: "cidr", Value: "85.10.0.0/15"}, wantErr: ErrInvalidField},
		{name: "ipv6 cidr broader than /32", in: Indicator{Kind: "cidr", Value: "2a01::/31"}, wantErr: ErrInvalidField},
		{name: "ipv4 cidr /32", in: Indicator{Kind: "cidr", Value: "85.10.20.30/32"}, wantErr: ErrInvalidField},
		{name: "ipv6 cidr /128", in: Indicator{Kind: "cidr", Value: "2a01:4f8::1/128"}, wantErr: ErrInvalidField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in
			err := got.Normalize()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Normalize() error = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("Normalize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestKeyIsStableAcrossNormalization(t *testing.T) {
	a := validVerdict()
	a.Indicator = Indicator{Kind: "IPv6", Value: "2A01:04F8:0000:0000:0000:0000:0000:0001"}
	b := validVerdict()
	b.Indicator = Indicator{Kind: KindIPv6, Value: "2a01:4f8::1"}
	for _, e := range []*Event{a, b} {
		if err := e.Normalize(); err != nil {
			t.Fatalf("Normalize() error = %v", err)
		}
	}
	if a.Key() != b.Key() {
		t.Errorf("keys differ after normalization: %q vs %q", a.Key(), b.Key())
	}
}

func TestIndicatorValidate(t *testing.T) {
	tests := []struct {
		name     string
		in       Indicator
		allowDoc bool
		wantErr  error
	}{
		{name: "public ipv4", in: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"}},
		{name: "public ipv6", in: Indicator{Kind: KindIPv6, Value: "2a01:4f8::1", Scope: "/128"}},
		{name: "public ipv4 cidr /16", in: Indicator{Kind: KindCIDR, Value: "85.10.0.0/16", Scope: "/16"}},
		{name: "public ipv4 cidr /31", in: Indicator{Kind: KindCIDR, Value: "85.10.20.30/31", Scope: "/31"}},
		{name: "public ipv6 cidr /32", in: Indicator{Kind: KindCIDR, Value: "2a01:4f8::/32", Scope: "/32"}},
		{name: "public ipv6 cidr /127", in: Indicator{Kind: KindCIDR, Value: "2a01:4f8::/127", Scope: "/127"}},

		// Canonical form and scope.
		{name: "upper-case kind", in: Indicator{Kind: "IPv4", Value: "85.10.20.30", Scope: "/32"}, wantErr: ErrInvalidField},
		{name: "non-canonical ipv6", in: Indicator{Kind: KindIPv6, Value: "2A01:4F8::1", Scope: "/128"}, wantErr: ErrInvalidField},
		{name: "uncompressed ipv6", in: Indicator{Kind: KindIPv6, Value: "2a01:4f8:0:0:0:0:0:1", Scope: "/128"}, wantErr: ErrInvalidField},
		{name: "cidr host bits set", in: Indicator{Kind: KindCIDR, Value: "85.10.20.0/16", Scope: "/16"}, wantErr: ErrInvalidField},
		{name: "ipv4 wrong scope", in: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/24"}, wantErr: ErrInvalidField},
		{name: "ipv4 missing scope", in: Indicator{Kind: KindIPv4, Value: "85.10.20.30"}, wantErr: ErrInvalidField},
		{name: "cidr wrong scope", in: Indicator{Kind: KindCIDR, Value: "85.10.0.0/16", Scope: "/24"}, wantErr: ErrInvalidField},
		{name: "unsupported kind", in: Indicator{Kind: "asn", Value: "64496", Scope: "/0"}, wantErr: ErrUnsupportedIndicator},

		// Non-public ranges (AC5).
		{name: "unspecified ipv4", in: Indicator{Kind: KindIPv4, Value: "0.0.0.0", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "this network", in: Indicator{Kind: KindIPv4, Value: "0.1.2.3", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "rfc1918 10/8", in: Indicator{Kind: KindIPv4, Value: "10.1.2.3", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "rfc1918 172.16/12", in: Indicator{Kind: KindIPv4, Value: "172.31.255.254", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "rfc1918 192.168/16", in: Indicator{Kind: KindIPv4, Value: "192.168.1.1", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "cgnat", in: Indicator{Kind: KindIPv4, Value: "100.64.0.1", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "loopback ipv4", in: Indicator{Kind: KindIPv4, Value: "127.0.0.1", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "link-local ipv4", in: Indicator{Kind: KindIPv4, Value: "169.254.1.1", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "benchmarking", in: Indicator{Kind: KindIPv4, Value: "198.18.0.1", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "multicast ipv4", in: Indicator{Kind: KindIPv4, Value: "224.0.0.251", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "broadcast", in: Indicator{Kind: KindIPv4, Value: "255.255.255.255", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "reserved ipv4", in: Indicator{Kind: KindIPv4, Value: "240.0.0.1", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "unspecified ipv6", in: Indicator{Kind: KindIPv6, Value: "::", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "loopback ipv6", in: Indicator{Kind: KindIPv6, Value: "::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "ipv4-mapped ipv6", in: Indicator{Kind: KindIPv6, Value: "::ffff:85.10.20.30", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "ula", in: Indicator{Kind: KindIPv6, Value: "fd00::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "link-local ipv6", in: Indicator{Kind: KindIPv6, Value: "fe80::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "multicast ipv6", in: Indicator{Kind: KindIPv6, Value: "ff02::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "6to4 relay anycast", in: Indicator{Kind: KindIPv4, Value: "192.88.99.1", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "ipv4-compatible private", in: Indicator{Kind: KindIPv6, Value: "::a00:1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "ipv4-compatible loopback", in: Indicator{Kind: KindIPv6, Value: "::7f00:1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "ipv4-translated", in: Indicator{Kind: KindIPv6, Value: "::ffff:0:a00:1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "nat64 of private", in: Indicator{Kind: KindIPv6, Value: "64:ff9b::a00:1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "nat64 of loopback", in: Indicator{Kind: KindIPv6, Value: "64:ff9b::7f00:1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "nat64 local-use", in: Indicator{Kind: KindIPv6, Value: "64:ff9b:1::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "discard-only", in: Indicator{Kind: KindIPv6, Value: "100::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "6to4 wrapping private", in: Indicator{Kind: KindIPv6, Value: "2002:a00:1::", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "teredo", in: Indicator{Kind: KindIPv6, Value: "2001::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "ipv6 benchmarking", in: Indicator{Kind: KindIPv6, Value: "2001:2::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "unallocated ipv6", in: Indicator{Kind: KindIPv6, Value: "4000::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "site-local ipv6", in: Indicator{Kind: KindIPv6, Value: "fec0::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "public ipv6 at top of global unicast", in: Indicator{Kind: KindIPv6, Value: "3ffe:ffff::1", Scope: "/128"}},
		{name: "cidr outside global unicast", in: Indicator{Kind: KindCIDR, Value: "4000::/32", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "cidr containing 6to4", in: Indicator{Kind: KindCIDR, Value: "2002::/32", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "cidr inside rfc1918", in: Indicator{Kind: KindCIDR, Value: "10.20.0.0/16", Scope: "/16"}, wantErr: ErrNonPublicIndicator},
		{name: "cidr containing special range", in: Indicator{Kind: KindCIDR, Value: "192.0.0.0/16", Scope: "/16"}, wantErr: ErrNonPublicIndicator},
		{name: "cidr ula", in: Indicator{Kind: KindCIDR, Value: "fd12:3456::/32", Scope: "/32"}, wantErr: ErrNonPublicIndicator},

		// Documentation ranges: rejected unless explicitly allowed.
		{name: "doc ipv4 rejected", in: Indicator{Kind: KindIPv4, Value: "203.0.113.7", Scope: "/32"}, wantErr: ErrNonPublicIndicator},
		{name: "doc ipv6 rejected", in: Indicator{Kind: KindIPv6, Value: "2001:db8:6c::dead:beef", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "doc ipv6 3fff rejected", in: Indicator{Kind: KindIPv6, Value: "3fff::1", Scope: "/128"}, wantErr: ErrNonPublicIndicator},
		{name: "doc cidr rejected", in: Indicator{Kind: KindCIDR, Value: "198.51.100.0/24", Scope: "/24"}, wantErr: ErrNonPublicIndicator},
		{name: "doc ipv4 allowed", in: Indicator{Kind: KindIPv4, Value: "203.0.113.7", Scope: "/32"}, allowDoc: true},
		{name: "doc ipv6 allowed", in: Indicator{Kind: KindIPv6, Value: "2001:db8:6c::dead:beef", Scope: "/128"}, allowDoc: true},
		{name: "doc option keeps private rejected", in: Indicator{Kind: KindIPv4, Value: "10.1.2.3", Scope: "/32"}, allowDoc: true, wantErr: ErrNonPublicIndicator},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []Option
			if tt.allowDoc {
				opts = append(opts, AllowDocumentationRanges())
			}
			err := tt.in.validate(newOptions(opts))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate(%+v) error = %v, want %v", tt.in, err, tt.wantErr)
			}
		})
	}
}
