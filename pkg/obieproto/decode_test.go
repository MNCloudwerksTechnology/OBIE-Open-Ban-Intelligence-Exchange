package obieproto

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// readmeExample is the verdict from README §3.2.1 with its placeholders
// filled in. Its indicator is a documentation address.
//
//nolint:gosec // G101 false positive: the signature is a zero-byte placeholder.
const readmeExample = `{
  "id": "01923e4a-7b2c-7def-8a12-3456789abcde",
  "spec": "obie/0.1",
  "type": "indicator.verdict",
  "issued_at": "2026-01-05T01:50:00Z",
  "indicator": {
    "kind": "ipv6",
    "value": "2001:db8:6c::dead:beef",
    "scope": "/128"
  },
  "protocol": "ssh",
  "evidence": {
    "events": 47,
    "reason": "password_bruteforce",
    "log_hash": "sha256:1b4a0000000000000000000000000000000000000000000000000000000000ff",
    "honeypot": true
  },
  "verdict": {
    "suggested_action": "ban",
    "confidence": 0.92,
    "ttl_seconds": 604800
  },
  "mitre": ["T1110"],
  "publisher": {
    "peer_id": "12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN",
    "asn": 64512,
    "signature": "ed25519:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
  }
}`

const revokeJSON = `{"id":"01923e4a-7b2c-7def-9a12-3456789abcdf","spec":"obie/0.1","type":"indicator.revoke",` +
	`"issued_at":"2026-01-05T01:50:00Z","indicator":{"kind":"ipv4","value":"85.10.20.30","scope":"/32"},` +
	`"revokes":"01923e4a-7b2c-7def-8a12-3456789abcde","reason":"false_positive",` +
	`"publisher":{"peer_id":"12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN","signature":""}}`

func mustMarshal(t testing.TB, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return data
}

// withField returns the JSON of validVerdict with the key at path set to raw
// (or removed when raw is ""). path is dot-separated, e.g. "verdict.confidence".
func withField(t *testing.T, path, raw string) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(mustMarshal(t, validVerdict()), &m); err != nil {
		t.Fatal(err)
	}
	keys := strings.Split(path, ".")
	obj := m
	for _, k := range keys[:len(keys)-1] {
		obj = obj[k].(map[string]any)
	}
	last := keys[len(keys)-1]
	if raw == "" {
		delete(obj, last)
	} else {
		obj[last] = json.RawMessage(raw)
	}
	return mustMarshal(t, m)
}

func TestDecodeValid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		opts []Option
		want *Event
	}{
		{name: "verdict round trip", data: mustMarshal(t, validVerdict()), want: validVerdict()},
		{name: "revoke", data: []byte(revokeJSON), want: func() *Event {
			e := validRevoke()
			e.Publisher.Signature = ""
			return e
		}()},
		{name: "readme example with documentation option", data: []byte(readmeExample), opts: []Option{AllowDocumentationRanges()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := append([]Option{fixedClock()}, tt.opts...)
			got, err := Decode(tt.data, opts...)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if tt.want != nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decode() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDecodeReadmeExampleRejectsDocumentationRange(t *testing.T) {
	_, err := Decode([]byte(readmeExample), fixedClock())
	if !errors.Is(err, ErrNonPublicIndicator) {
		t.Errorf("Decode() error = %v, want %v", err, ErrNonPublicIndicator)
	}
}

func TestDecodeRoundTripIsByteIdentical(t *testing.T) {
	in := mustMarshal(t, validVerdict())
	e, err := Decode(in, fixedClock())
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if out := mustMarshal(t, e); string(out) != string(in) {
		t.Errorf("round trip changed the event:\n in: %s\nout: %s", in, out)
	}
}

func TestDecodeInvalid(t *testing.T) {
	valid := string(mustMarshal(t, validVerdict()))
	tests := []struct {
		name    string
		data    []byte
		wantErr error
	}{
		// Structure.
		{name: "empty input", data: nil, wantErr: ErrMalformed},
		{name: "not json", data: []byte("hello"), wantErr: ErrMalformed},
		{name: "array", data: []byte("[]"), wantErr: ErrMalformed},
		{name: "null", data: []byte("null"), wantErr: ErrMalformed},
		{name: "truncated", data: []byte(valid[:len(valid)-1]), wantErr: ErrMalformed},
		{name: "trailing object", data: []byte(valid + "{}"), wantErr: ErrMalformed},
		{name: "trailing garbage", data: []byte(valid + "x"), wantErr: ErrMalformed},
		{name: "too large", data: []byte(valid[:len(valid)-1] + strings.Repeat(" ", MaxEventSize) + "}"), wantErr: ErrTooLarge},

		// Spec and type are checked first (forward compatibility).
		{name: "missing spec", data: withField(t, "spec", ""), wantErr: ErrUnsupportedSpec},
		{name: "newer spec with unknown fields", data: []byte(`{"spec":"obie/0.2","type":"indicator.verdict","extra":1}`), wantErr: ErrUnsupportedSpec},
		{name: "spec not a string", data: withField(t, "spec", `1`), wantErr: ErrUnsupportedSpec},
		{name: "unknown type with unknown fields", data: []byte(`{"spec":"obie/0.1","type":"indicator.appeal","appeal":{}}`), wantErr: ErrUnsupportedType},
		{name: "observation type", data: withField(t, "type", `"indicator.observation"`), wantErr: ErrUnsupportedType},
		{name: "missing type", data: withField(t, "type", ""), wantErr: ErrUnsupportedType},
		{name: "type wrong case", data: withField(t, "type", `"Indicator.Verdict"`), wantErr: ErrUnsupportedType},

		// Unknown and duplicate keys.
		{name: "unknown top-level key", data: withField(t, "extra", `1`), wantErr: ErrUnknownField},
		{name: "unknown nested key", data: withField(t, "verdict.extra", `1`), wantErr: ErrUnknownField},
		{name: "unknown publisher key", data: withField(t, "publisher.org", `"acme"`), wantErr: ErrUnknownField},
		{name: "key wrong case", data: []byte(strings.Replace(valid, `"protocol"`, `"Protocol"`, 1)), wantErr: ErrUnknownField},
		{name: "nested key wrong case", data: []byte(strings.Replace(valid, `"confidence"`, `"CONFIDENCE"`, 1)), wantErr: ErrUnknownField},
		{name: "duplicate key", data: []byte(strings.Replace(valid, `"protocol":"ssh"`, `"protocol":"ssh","protocol":"rdp"`, 1)), wantErr: ErrUnknownField},
		{name: "duplicate nested key", data: []byte(strings.Replace(valid, `"confidence":0.92`, `"confidence":0.1,"confidence":0.92`, 1)), wantErr: ErrUnknownField},

		// Missing, null and empty values.
		{name: "missing id", data: withField(t, "id", ""), wantErr: ErrInvalidField},
		{name: "missing indicator", data: withField(t, "indicator", ""), wantErr: ErrInvalidField},
		{name: "missing scope", data: withField(t, "indicator.scope", ""), wantErr: ErrInvalidField},
		{name: "missing confidence", data: withField(t, "verdict.confidence", ""), wantErr: ErrInvalidField},
		{name: "missing honeypot", data: withField(t, "evidence.honeypot", ""), wantErr: ErrInvalidField},
		{name: "missing signature key", data: withField(t, "publisher.signature", ""), wantErr: ErrInvalidField},
		{name: "missing evidence", data: withField(t, "evidence", ""), wantErr: ErrInvalidField},
		{name: "null id", data: withField(t, "id", `null`), wantErr: ErrMalformed},
		{name: "null evidence", data: withField(t, "evidence", `null`), wantErr: ErrMalformed},
		{name: "null asn", data: withField(t, "publisher.asn", `null`), wantErr: ErrMalformed},
		{name: "null mitre", data: withField(t, "mitre", `null`), wantErr: ErrMalformed},
		{name: "empty log_hash", data: withField(t, "evidence.log_hash", `""`), wantErr: ErrInvalidField},
		{name: "empty revokes in verdict", data: withField(t, "revokes", `""`), wantErr: ErrInvalidField},
		{name: "empty mitre", data: withField(t, "mitre", `[]`), wantErr: ErrInvalidField},

		// Wrong JSON types.
		{name: "indicator not an object", data: withField(t, "indicator", `"85.10.20.30"`), wantErr: ErrMalformed},
		{name: "confidence as string", data: withField(t, "verdict.confidence", `"0.9"`), wantErr: ErrMalformed},
		{name: "events as float", data: withField(t, "evidence.events", `47.5`), wantErr: ErrMalformed},
		{name: "honeypot as string", data: withField(t, "evidence.honeypot", `"true"`), wantErr: ErrMalformed},
		{name: "negative asn", data: withField(t, "publisher.asn", `-1`), wantErr: ErrMalformed},
		{name: "asn above 32 bits", data: withField(t, "publisher.asn", `4294967296`), wantErr: ErrMalformed},
		{name: "mitre not a list", data: withField(t, "mitre", `"T1110"`), wantErr: ErrMalformed},
		{name: "issued_at with offset", data: withField(t, "issued_at", `"2026-01-05T02:50:00+01:00"`), wantErr: ErrMalformed},
		{name: "issued_at as number", data: withField(t, "issued_at", `1767577800`), wantErr: ErrMalformed},

		// Validation runs after decoding.
		{name: "issued_at in the future", data: withField(t, "issued_at", `"2026-01-05T02:00:00Z"`), wantErr: ErrInvalidField},
		{name: "unsupported indicator kind", data: withField(t, "indicator", `{"kind":"fqdn","value":"evil.example","scope":"/0"}`), wantErr: ErrUnsupportedIndicator},
		{name: "private indicator", data: withField(t, "indicator", `{"kind":"ipv4","value":"10.0.0.1","scope":"/32"}`), wantErr: ErrNonPublicIndicator},
		{name: "non-canonical indicator", data: withField(t, "indicator", `{"kind":"ipv6","value":"2A01:4F8::1","scope":"/128"}`), wantErr: ErrInvalidField},
		{name: "confidence out of range", data: withField(t, "verdict.confidence", `1.5`), wantErr: ErrInvalidField},
		{name: "revoke with verdict fields", data: []byte(strings.Replace(revokeJSON, `"reason":"false_positive",`, `"reason":"false_positive","protocol":"ssh",`, 1)), wantErr: ErrInvalidField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(tt.data, fixedClock())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Decode() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Errorf("Decode() returned an event alongside error %v", err)
			}
		})
	}
}

// FuzzDecode checks that decoding and validating arbitrary input never
// panics, and that every accepted event re-encodes to input that decodes to
// the same event.
func FuzzDecode(f *testing.F) {
	f.Add(mustMarshal(f, validVerdict()))
	f.Add(mustMarshal(f, validRevoke()))
	f.Add([]byte(readmeExample))
	f.Add([]byte(revokeJSON))
	f.Add([]byte(`{"spec":"obie/0.1","type":"indicator.verdict"}`))
	f.Add([]byte(`{"spec":"obie/0.2"}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		e, err := Decode(data, fixedClock(), AllowDocumentationRanges())
		if err != nil {
			var fe *FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("Decode() error %T is not a *FieldError: %v", err, err)
			}
			return
		}
		again, err := Decode(mustMarshal(t, e), fixedClock(), AllowDocumentationRanges())
		if err != nil {
			t.Fatalf("re-encoded event rejected: %v", err)
		}
		if !reflect.DeepEqual(e, again) {
			t.Fatalf("re-encoded event differs:\n%+v\n%+v", e, again)
		}
	})
}
