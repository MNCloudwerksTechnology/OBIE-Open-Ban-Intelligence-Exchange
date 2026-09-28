package nft

import (
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
)

var (
	now     = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	forever = now.Add(100 * 365 * 24 * time.Hour)
)

func addr(s string) []byte { return netip.MustParseAddr(s).AsSlice() }

func TestToElements(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		left   time.Duration
		want   []element
	}{
		{"v4 address", "192.0.2.1/32", time.Hour, []element{
			{key: addr("192.0.2.1"), timeout: time.Hour},
			{key: addr("192.0.2.2"), end: true},
		}},
		{"v4 cidr", "198.51.100.0/24", 90 * time.Second, []element{
			{key: addr("198.51.100.0"), timeout: 90 * time.Second},
			{key: addr("198.51.101.0"), end: true},
		}},
		{"unmasked cidr", "198.51.100.7/24", time.Minute, []element{
			{key: addr("198.51.100.0"), timeout: time.Minute},
			{key: addr("198.51.101.0"), end: true},
		}},
		{"v6 address", "2001:db8::1/128", time.Hour, []element{
			{key: addr("2001:db8::1"), timeout: time.Hour},
			{key: addr("2001:db8::2"), end: true},
		}},
		{"v6 cidr", "2001:db8::/32", time.Hour, []element{
			{key: addr("2001:db8::"), timeout: time.Hour},
			{key: addr("2001:db9::"), end: true},
		}},
		{"top of v4 has no end", "255.255.255.255/32", time.Hour, []element{
			{key: addr("255.255.255.255"), timeout: time.Hour},
		}},
		{"everything v4", "0.0.0.0/0", time.Hour, []element{
			{key: addr("0.0.0.0"), timeout: time.Hour},
		}},
		{"top of v6 has no end", "ffff::/16", time.Hour, []element{
			{key: addr("ffff::"), timeout: time.Hour},
		}},
		{"timeout rounded up to ms", "192.0.2.1/32", 1500*time.Millisecond + time.Microsecond, []element{
			{key: addr("192.0.2.1"), timeout: 1501 * time.Millisecond},
			{key: addr("192.0.2.2"), end: true},
		}},
		{"sub-ms timeout is not zero", "192.0.2.1/32", time.Nanosecond, []element{
			{key: addr("192.0.2.1"), timeout: time.Millisecond},
			{key: addr("192.0.2.2"), end: true},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := enforce.Entry{Prefix: netip.MustParsePrefix(tt.prefix), Expires: now.Add(tt.left)}
			got, err := toElements(e, now)
			if err != nil {
				t.Fatal(err)
			}
			tt.want[0].comment = e.Prefix.Masked().String()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("toElements = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestToElementsRejectsExpired(t *testing.T) {
	for _, left := range []time.Duration{0, -time.Second} {
		e := enforce.Entry{Prefix: netip.MustParsePrefix("192.0.2.1/32"), Expires: now.Add(left)}
		if _, err := toElements(e, now); !errors.Is(err, errExpired) {
			t.Errorf("left %v: err = %v, want errExpired", left, err)
		}
	}
	if _, err := toElements(enforce.Entry{Expires: now.Add(time.Hour)}, now); err == nil {
		t.Error("invalid prefix accepted")
	}
}

func TestKeys(t *testing.T) {
	got := keys(netip.MustParsePrefix("203.0.113.0/25"))
	want := []element{{key: addr("203.0.113.0")}, {key: addr("203.0.113.128"), end: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %+v, want %+v", got, want)
	}
}

// TestRoundTrip maps entries to elements, lists them as the kernel would
// (in reverse order, with the lifetime left) and maps them back.
func TestRoundTrip(t *testing.T) {
	entries := []enforce.Entry{
		{Prefix: netip.MustParsePrefix("0.0.0.0/8"), Expires: now.Add(time.Hour)},
		{Prefix: netip.MustParsePrefix("192.0.2.1/32"), Expires: now.Add(2 * time.Hour)},
		{Prefix: netip.MustParsePrefix("192.0.2.2/32"), Expires: now.Add(3 * time.Hour)}, // adjacent
		{Prefix: netip.MustParsePrefix("192.0.2.4/30"), Expires: now.Add(time.Minute)},   // adjacent after a gap of one
		{Prefix: netip.MustParsePrefix("255.255.255.255/32"), Expires: now.Add(time.Hour)},
	}
	var elems []element
	for _, e := range entries {
		els, err := toElements(e, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, el := range els {
			el.expires = el.timeout
			elems = append([]element{el}, elems...)
		}
	}
	got, err := fromElements(elems, now, time.Second, forever)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Errorf("round trip = %+v, want %+v", got, entries)
	}
}

func TestFromElements(t *testing.T) {
	later := func(d time.Duration) time.Time { return now.Add(d) }
	tests := []struct {
		name    string
		elems   []element
		want    []enforce.Entry
		wantErr bool
	}{
		{"remaining lifetime", []element{
			{key: addr("192.0.2.2"), end: true},
			{key: addr("192.0.2.1"), timeout: time.Hour, expires: 10 * time.Minute},
		}, []enforce.Entry{{Prefix: netip.MustParsePrefix("192.0.2.1/32"), Expires: later(10 * time.Minute)}}, false},
		{"comment beats a stale end", []element{
			{key: addr("192.0.2.0"), comment: "192.0.2.0/24", timeout: time.Hour, expires: time.Hour},
			{key: addr("192.0.2.6"), end: true}, // of an expired 192.0.2.5/32
			{key: addr("192.0.3.0"), end: true},
		}, []enforce.Entry{{Prefix: netip.MustParsePrefix("192.0.2.0/24"), Expires: later(time.Hour)}}, false},
		{"comment of another address is ignored", []element{
			{key: addr("192.0.2.0"), comment: "192.0.2.1/32", timeout: time.Hour, expires: time.Hour},
			{key: addr("192.0.3.0"), end: true},
		}, []enforce.Entry{{Prefix: netip.MustParsePrefix("192.0.2.0/24"), Expires: later(time.Hour)}}, false},
		{"about to expire is left out", []element{
			{key: addr("192.0.2.1"), timeout: time.Hour, expires: 999 * time.Millisecond},
			{key: addr("192.0.2.2"), end: true},
		}, []enforce.Entry{}, false},
		{"without timeout never expires", []element{
			{key: addr("2001:db8::")},
			{key: addr("2001:db8::1:0"), end: true},
		}, []enforce.Entry{{Prefix: netip.MustParsePrefix("2001:db8::/112"), Expires: forever}}, false},
		{"not a prefix", []element{
			{key: addr("192.0.2.1"), timeout: time.Hour, expires: time.Hour},
			{key: addr("192.0.2.4"), end: true},
		}, nil, true},
		{"unaligned open end", []element{
			{key: addr("255.255.255.253"), timeout: time.Hour, expires: time.Hour},
		}, nil, true},
		{"bad key", []element{{key: []byte{1, 2, 3}}}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fromElements(tt.elems, now, time.Second, forever)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromElements = %+v, want %+v", got, tt.want)
			}
		})
	}
}
