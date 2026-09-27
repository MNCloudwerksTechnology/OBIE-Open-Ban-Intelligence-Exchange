package obieproto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const (
	testID      = "01923e4a-7b2c-7def-8a12-3456789abcde"
	testOtherID = "01923e4a-7b2c-7def-9a12-3456789abcdf"
	testPeerID  = "12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN"
)

var (
	testNow       = time.Date(2026, 1, 5, 1, 50, 0, 0, time.UTC)
	testSignature = "ed25519:" + strings.Repeat("A", 86)
)

// validVerdict returns a verdict event that passes Validate at testNow.
func validVerdict() *Event {
	asn := uint32(64496)
	return &Event{
		ID:        testID,
		Spec:      Spec,
		Type:      TypeVerdict,
		IssuedAt:  NewTimestamp(testNow),
		Indicator: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"},
		Protocol:  "ssh",
		Evidence: &Evidence{
			Events:   47,
			Reason:   "password_bruteforce",
			LogHash:  "sha256:" + strings.Repeat("ab", 32),
			Honeypot: true,
		},
		Verdict:   &Verdict{SuggestedAction: ActionBan, Confidence: 0.92, TTLSeconds: 604800},
		MITRE:     []string{"T1110", "T1110.001"},
		Publisher: Publisher{PeerID: testPeerID, ASN: &asn, Signature: testSignature},
	}
}

// validRevoke returns a revocation event that passes Validate at testNow.
func validRevoke() *Event {
	return &Event{
		ID:        testOtherID,
		Spec:      Spec,
		Type:      TypeRevoke,
		IssuedAt:  NewTimestamp(testNow),
		Indicator: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"},
		Revokes:   testID,
		Reason:    "false_positive",
		Publisher: Publisher{PeerID: testPeerID, Signature: testSignature},
	}
}

func TestExpiresAt(t *testing.T) {
	tests := []struct {
		name  string
		event *Event
		want  time.Time
	}{
		{name: "verdict uses ttl", event: validVerdict(), want: testNow.Add(604800 * time.Second)},
		{name: "revoke uses max ttl", event: validRevoke(), want: testNow.Add(MaxTTLSeconds * time.Second)},
		{
			name:  "verdict without verdict block falls back to max ttl",
			event: func() *Event { e := validVerdict(); e.Verdict = nil; return e }(),
			want:  testNow.Add(MaxTTLSeconds * time.Second),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.ExpiresAt(); !got.Equal(tt.want) {
				t.Errorf("ExpiresAt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpired(t *testing.T) {
	e := validVerdict()
	e.Verdict.TTLSeconds = 60
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "at issue time", now: testNow, want: false},
		{name: "one second before expiry", now: testNow.Add(59 * time.Second), want: false},
		{name: "exactly at expiry", now: testNow.Add(60 * time.Second), want: true},
		{name: "after expiry", now: testNow.Add(time.Hour), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.Expired(tt.now); got != tt.want {
				t.Errorf("Expired(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestKey(t *testing.T) {
	tests := []struct {
		name      string
		indicator Indicator
		want      string
	}{
		{name: "ipv4", indicator: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"}, want: "ipv4:85.10.20.30"},
		{name: "ipv6", indicator: Indicator{Kind: KindIPv6, Value: "2a01:4f8::1", Scope: "/128"}, want: "ipv6:2a01:4f8::1"},
		{name: "cidr", indicator: Indicator{Kind: KindCIDR, Value: "85.10.0.0/16", Scope: "/16"}, want: "cidr:85.10.0.0/16"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validVerdict()
			e.Indicator = tt.indicator
			if got := e.Key(); got != tt.want {
				t.Errorf("Key() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTimestampUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "utc seconds", input: `"2026-01-05T01:50:00Z"`},
		{name: "offset", input: `"2026-01-05T01:50:00+00:00"`, wantErr: true},
		{name: "non-utc offset", input: `"2026-01-05T02:50:00+01:00"`, wantErr: true},
		{name: "fractional seconds", input: `"2026-01-05T01:50:00.5Z"`, wantErr: true},
		{name: "lower-case z", input: `"2026-01-05T01:50:00z"`, wantErr: true},
		{name: "date only", input: `"2026-01-05"`, wantErr: true},
		{name: "invalid date", input: `"2026-02-30T01:50:00Z"`, wantErr: true},
		{name: "number", input: `1767577800`, wantErr: true},
		{name: "null", input: `null`, wantErr: true},
		{name: "empty", input: `""`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ts Timestamp
			err := json.Unmarshal([]byte(tt.input), &ts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal(%s) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			out, err := json.Marshal(ts)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(out) != tt.input {
				t.Errorf("round trip = %s, want %s", out, tt.input)
			}
		})
	}
}

func TestNewTimestamp(t *testing.T) {
	in := time.Date(2026, 1, 5, 3, 50, 0, 999, time.FixedZone("CET+2", 2*3600))
	got := NewTimestamp(in)
	if got.Location() != time.UTC || got.Nanosecond() != 0 || !got.Equal(testNow) {
		t.Errorf("NewTimestamp(%v) = %v, want %v", in, got.Time, testNow)
	}
}
