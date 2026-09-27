package obieproto

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func fixedClock() Option {
	return WithClock(func() time.Time { return testNow })
}

func TestValidate(t *testing.T) {
	type mutate func(e *Event)
	verdict := func(m mutate) *Event { e := validVerdict(); m(e); return e }
	revoke := func(m mutate) *Event { e := validRevoke(); m(e); return e }
	asn := func(v uint32) *uint32 { return &v }

	tests := []struct {
		name    string
		event   *Event
		opts    []Option
		wantErr error
	}{
		{name: "valid verdict", event: validVerdict()},
		{name: "valid revoke", event: validRevoke()},
		{name: "valid minimal verdict", event: verdict(func(e *Event) {
			e.MITRE = nil
			e.Evidence.LogHash = ""
			e.Evidence.Honeypot = false
			e.Publisher.ASN = nil
			e.Publisher.Signature = ""
		})},
		{name: "valid ipv6 verdict", event: verdict(func(e *Event) {
			e.Indicator = Indicator{Kind: KindIPv6, Value: "2a01:4f8::1", Scope: "/128"}
		})},
		{name: "valid cidr verdict", event: verdict(func(e *Event) {
			e.Indicator = Indicator{Kind: KindCIDR, Value: "85.10.0.0/16", Scope: "/16"}
		})},

		// Envelope.
		{name: "wrong spec", event: verdict(func(e *Event) { e.Spec = "obie/0.2" }), wantErr: ErrUnsupportedSpec},
		{name: "missing spec", event: verdict(func(e *Event) { e.Spec = "" }), wantErr: ErrUnsupportedSpec},
		{name: "unsupported type", event: verdict(func(e *Event) { e.Type = "indicator.observation" }), wantErr: ErrUnsupportedType},
		{name: "unsupported type wins over bad indicator", event: verdict(func(e *Event) {
			e.Type = "indicator.appeal"
			e.Indicator = Indicator{Kind: "fqdn", Value: "evil.example"}
		}), wantErr: ErrUnsupportedType},
		{name: "missing type", event: verdict(func(e *Event) { e.Type = "" }), wantErr: ErrUnsupportedType},
		{name: "id not uuid", event: verdict(func(e *Event) { e.ID = "uuid-v7" }), wantErr: ErrInvalidField},
		{name: "id uuidv4", event: verdict(func(e *Event) { e.ID = "01923e4a-7b2c-4def-8a12-3456789abcde" }), wantErr: ErrInvalidField},
		{name: "id wrong variant", event: verdict(func(e *Event) { e.ID = "01923e4a-7b2c-7def-ca12-3456789abcde" }), wantErr: ErrInvalidField},
		{name: "id upper-case", event: verdict(func(e *Event) { e.ID = strings.ToUpper(testID) }), wantErr: ErrInvalidField},

		// issued_at.
		{name: "issued_at zero", event: verdict(func(e *Event) { e.IssuedAt = Timestamp{} }), wantErr: ErrInvalidField},
		{name: "issued_at sub-second", event: verdict(func(e *Event) {
			e.IssuedAt = Timestamp{testNow.Add(time.Millisecond)}
		}), wantErr: ErrInvalidField},
		{name: "issued_at exactly max skew ahead", event: verdict(func(e *Event) {
			e.IssuedAt = NewTimestamp(testNow.Add(MaxClockSkew))
		})},
		{name: "issued_at beyond max skew", event: verdict(func(e *Event) {
			e.IssuedAt = NewTimestamp(testNow.Add(MaxClockSkew + time.Second))
		}), wantErr: ErrInvalidField},
		{name: "issued_at in the past", event: verdict(func(e *Event) {
			e.IssuedAt = NewTimestamp(testNow.Add(-365 * 24 * time.Hour))
		})},

		// Indicator (details in TestIndicatorValidate).
		{name: "unsupported indicator", event: verdict(func(e *Event) {
			e.Indicator = Indicator{Kind: "fqdn", Value: "evil.example", Scope: ""}
		}), wantErr: ErrUnsupportedIndicator},
		{name: "private indicator", event: verdict(func(e *Event) {
			e.Indicator = Indicator{Kind: KindIPv4, Value: "192.168.1.1", Scope: "/32"}
		}), wantErr: ErrNonPublicIndicator},
		{name: "documentation indicator", event: verdict(func(e *Event) {
			e.Indicator = Indicator{Kind: KindIPv6, Value: "2001:db8:6c::dead:beef", Scope: "/128"}
		}), wantErr: ErrNonPublicIndicator},
		{name: "documentation indicator allowed by option", event: verdict(func(e *Event) {
			e.Indicator = Indicator{Kind: KindIPv6, Value: "2001:db8:6c::dead:beef", Scope: "/128"}
		}), opts: []Option{AllowDocumentationRanges()}},
		{name: "non-canonical indicator", event: verdict(func(e *Event) {
			e.Indicator = Indicator{Kind: KindIPv6, Value: "2A01:4F8::1", Scope: "/128"}
		}), wantErr: ErrInvalidField},

		// protocol.
		{name: "protocol with dash and digits", event: verdict(func(e *Event) { e.Protocol = "http-2_tls" })},
		{name: "protocol 32 chars", event: verdict(func(e *Event) { e.Protocol = strings.Repeat("a", 32) })},
		{name: "protocol 33 chars", event: verdict(func(e *Event) { e.Protocol = strings.Repeat("a", 33) }), wantErr: ErrInvalidField},
		{name: "protocol missing", event: verdict(func(e *Event) { e.Protocol = "" }), wantErr: ErrInvalidField},
		{name: "protocol upper-case", event: verdict(func(e *Event) { e.Protocol = "SSH" }), wantErr: ErrInvalidField},
		{name: "protocol with dot", event: verdict(func(e *Event) { e.Protocol = "obie.v0" }), wantErr: ErrInvalidField},

		// evidence.
		{name: "evidence missing", event: verdict(func(e *Event) { e.Evidence = nil }), wantErr: ErrInvalidField},
		{name: "evidence zero events", event: verdict(func(e *Event) { e.Evidence.Events = 0 }), wantErr: ErrInvalidField},
		{name: "evidence negative events", event: verdict(func(e *Event) { e.Evidence.Events = -1 }), wantErr: ErrInvalidField},
		{name: "evidence max events", event: verdict(func(e *Event) { e.Evidence.Events = MaxEvidenceEvents })},
		{name: "evidence events beyond 2^53-1", event: verdict(func(e *Event) { e.Evidence.Events = MaxEvidenceEvents + 1 }), wantErr: ErrInvalidField},
		{name: "evidence reason 64 chars", event: verdict(func(e *Event) { e.Evidence.Reason = strings.Repeat("a", 64) })},
		{name: "evidence reason 65 chars", event: verdict(func(e *Event) { e.Evidence.Reason = strings.Repeat("a", 65) }), wantErr: ErrInvalidField},
		{name: "evidence reason missing", event: verdict(func(e *Event) { e.Evidence.Reason = "" }), wantErr: ErrInvalidField},
		{name: "evidence reason with dash", event: verdict(func(e *Event) { e.Evidence.Reason = "password-bruteforce" }), wantErr: ErrInvalidField},
		{name: "log_hash short", event: verdict(func(e *Event) { e.Evidence.LogHash = "sha256:1b4a" }), wantErr: ErrInvalidField},
		{name: "log_hash upper-case hex", event: verdict(func(e *Event) {
			e.Evidence.LogHash = "sha256:" + strings.Repeat("AB", 32)
		}), wantErr: ErrInvalidField},
		{name: "log_hash other algorithm", event: verdict(func(e *Event) {
			e.Evidence.LogHash = "sha512:" + strings.Repeat("ab", 32)
		}), wantErr: ErrInvalidField},

		// verdict.
		{name: "verdict missing", event: verdict(func(e *Event) { e.Verdict = nil }), wantErr: ErrInvalidField},
		{name: "action watch", event: verdict(func(e *Event) { e.Verdict.SuggestedAction = ActionWatch })},
		{name: "action unknown", event: verdict(func(e *Event) { e.Verdict.SuggestedAction = "drop" }), wantErr: ErrInvalidField},
		{name: "action upper-case", event: verdict(func(e *Event) { e.Verdict.SuggestedAction = "BAN" }), wantErr: ErrInvalidField},
		{name: "confidence 0", event: verdict(func(e *Event) { e.Verdict.Confidence = 0 })},
		{name: "confidence 1", event: verdict(func(e *Event) { e.Verdict.Confidence = 1 })},
		{name: "confidence negative zero", event: verdict(func(e *Event) { e.Verdict.Confidence = math.Copysign(0, -1) }), wantErr: ErrInvalidField},
		{name: "confidence negative", event: verdict(func(e *Event) { e.Verdict.Confidence = -0.01 }), wantErr: ErrInvalidField},
		{name: "confidence above 1", event: verdict(func(e *Event) { e.Verdict.Confidence = 1.01 }), wantErr: ErrInvalidField},
		{name: "confidence NaN", event: verdict(func(e *Event) { e.Verdict.Confidence = math.NaN() }), wantErr: ErrInvalidField},
		{name: "confidence +Inf", event: verdict(func(e *Event) { e.Verdict.Confidence = math.Inf(1) }), wantErr: ErrInvalidField},
		{name: "ttl min", event: verdict(func(e *Event) { e.Verdict.TTLSeconds = MinTTLSeconds })},
		{name: "ttl max", event: verdict(func(e *Event) { e.Verdict.TTLSeconds = MaxTTLSeconds })},
		{name: "ttl below min", event: verdict(func(e *Event) { e.Verdict.TTLSeconds = MinTTLSeconds - 1 }), wantErr: ErrInvalidField},
		{name: "ttl above max", event: verdict(func(e *Event) { e.Verdict.TTLSeconds = MaxTTLSeconds + 1 }), wantErr: ErrInvalidField},

		// mitre.
		{name: "mitre sub-technique", event: verdict(func(e *Event) { e.MITRE = []string{"T1110.004"} })},
		{name: "mitre empty list", event: verdict(func(e *Event) { e.MITRE = []string{} }), wantErr: ErrInvalidField},
		{name: "mitre lower-case", event: verdict(func(e *Event) { e.MITRE = []string{"t1110"} }), wantErr: ErrInvalidField},
		{name: "mitre three digits", event: verdict(func(e *Event) { e.MITRE = []string{"T111"} }), wantErr: ErrInvalidField},
		{name: "mitre bad sub-technique", event: verdict(func(e *Event) { e.MITRE = []string{"T1110.01"} }), wantErr: ErrInvalidField},
		{name: "mitre tactic id", event: verdict(func(e *Event) { e.MITRE = []string{"TA0006"} }), wantErr: ErrInvalidField},
		{name: "mitre duplicate", event: verdict(func(e *Event) { e.MITRE = []string{"T1110", "T1110"} }), wantErr: ErrInvalidField},

		// Fields of the other event type.
		{name: "verdict with revokes", event: verdict(func(e *Event) { e.Revokes = testOtherID }), wantErr: ErrInvalidField},
		{name: "verdict with reason", event: verdict(func(e *Event) { e.Reason = "false_positive" }), wantErr: ErrInvalidField},
		{name: "revoke with protocol", event: revoke(func(e *Event) { e.Protocol = "ssh" }), wantErr: ErrInvalidField},
		{name: "revoke with evidence", event: revoke(func(e *Event) { e.Evidence = validVerdict().Evidence }), wantErr: ErrInvalidField},
		{name: "revoke with verdict", event: revoke(func(e *Event) { e.Verdict = validVerdict().Verdict }), wantErr: ErrInvalidField},
		{name: "revoke with mitre", event: revoke(func(e *Event) { e.MITRE = []string{"T1110"} }), wantErr: ErrInvalidField},

		// Revocation.
		{name: "revoke missing revokes", event: revoke(func(e *Event) { e.Revokes = "" }), wantErr: ErrInvalidField},
		{name: "revoke revokes not uuidv7", event: revoke(func(e *Event) { e.Revokes = "abc" }), wantErr: ErrInvalidField},
		{name: "revoke revokes itself", event: revoke(func(e *Event) { e.Revokes = e.ID }), wantErr: ErrInvalidField},
		{name: "revoke missing reason", event: revoke(func(e *Event) { e.Reason = "" }), wantErr: ErrInvalidField},
		{name: "revoke reason with space", event: revoke(func(e *Event) { e.Reason = "false positive" }), wantErr: ErrInvalidField},
		{name: "revoke private indicator", event: revoke(func(e *Event) {
			e.Indicator = Indicator{Kind: KindIPv4, Value: "10.0.0.1", Scope: "/32"}
		}), wantErr: ErrNonPublicIndicator},

		// publisher.
		{name: "peer_id missing", event: verdict(func(e *Event) { e.Publisher.PeerID = "" }), wantErr: ErrInvalidField},
		{name: "peer_id not base58", event: verdict(func(e *Event) { e.Publisher.PeerID = "12D3KooW0000000000000000000000000000000000000000000000" }), wantErr: ErrInvalidField},
		{name: "asn zero", event: verdict(func(e *Event) { e.Publisher.ASN = asn(0) }), wantErr: ErrInvalidField},
		{name: "asn 32-bit", event: verdict(func(e *Event) { e.Publisher.ASN = asn(4200000000) })},
		{name: "signature wrong algorithm", event: verdict(func(e *Event) {
			e.Publisher.Signature = "rsa:" + strings.Repeat("A", 86)
		}), wantErr: ErrInvalidField},
		{name: "signature padded", event: verdict(func(e *Event) {
			e.Publisher.Signature = signaturePrefix + strings.Repeat("A", 86) + "=="
		}), wantErr: ErrInvalidField},
		{name: "signature standard base64", event: verdict(func(e *Event) {
			e.Publisher.Signature = signaturePrefix + strings.Repeat("+", 86)
		}), wantErr: ErrInvalidField},
		{name: "signature wrong length", event: verdict(func(e *Event) {
			e.Publisher.Signature = signaturePrefix + strings.Repeat("A", 43)
		}), wantErr: ErrInvalidField},
		{name: "signature non-canonical trailing bits", event: verdict(func(e *Event) {
			e.Publisher.Signature = signaturePrefix + strings.Repeat("A", 85) + "B"
		}), wantErr: ErrInvalidField},

		// Size.
		{name: "serialized size above 4 KiB", event: verdict(func(e *Event) {
			e.MITRE = make([]string, 0, 500)
			for i := range 500 {
				e.MITRE = append(e.MITRE, fmt.Sprintf("T1%03d", i))
			}
		}), wantErr: ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := append([]Option{fixedClock()}, tt.opts...)
			err := tt.event.Validate(opts...)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				var fe *FieldError
				if !errors.As(err, &fe) {
					t.Errorf("Validate() error %T is not a *FieldError", err)
				}
			}
		})
	}
}

func TestValidateDefaultClock(t *testing.T) {
	e := validVerdict()
	e.IssuedAt = NewTimestamp(time.Now().Add(time.Hour))
	if err := e.Validate(); !errors.Is(err, ErrInvalidField) {
		t.Errorf("Validate() with default clock error = %v, want %v", err, ErrInvalidField)
	}
}

func TestValidateDoesNotModify(t *testing.T) {
	e := validVerdict()
	e.Indicator = Indicator{Kind: KindIPv6, Value: "2A01:4F8::1", Scope: "/128"}
	before := e.Indicator
	_ = e.Validate(fixedClock())
	if e.Indicator != before {
		t.Errorf("Validate() modified indicator: %+v -> %+v", before, e.Indicator)
	}
}

func TestWithNilClockKeepsDefault(t *testing.T) {
	e := validVerdict()
	e.IssuedAt = NewTimestamp(time.Now())
	if err := e.Validate(WithClock(nil)); err != nil {
		t.Errorf("Validate(WithClock(nil)) error = %v", err)
	}
}
