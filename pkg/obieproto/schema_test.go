package obieproto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaFile is the published JSON Schema of obie/0.1 events.
const schemaFile = "../../documentation/spec/obie-0.1.schema.json"

// compileSchema compiles schemaFile as a JSON Schema 2020-12 document; the
// compiler checks it against the draft's meta-schema.
func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.(map[string]any)["$schema"]; got != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("$schema = %v, want JSON Schema 2020-12", got)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("obie-0.1.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("obie-0.1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

// schemaAccepts reports whether data, a single JSON document, is valid
// against sch.
func schemaAccepts(t *testing.T, sch *jsonschema.Schema, data []byte) (bool, error) {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("sample is not a JSON document: %v", err)
	}
	err = sch.Validate(v)
	return err == nil, err
}

// schemaSample is an event that the schema and Decode must judge alike.
type schemaSample struct {
	name  string
	data  []byte
	valid bool
}

func schemaSamples(t *testing.T) []schemaSample {
	valid := string(mustMarshal(t, validVerdict()))
	revoke := func(mutate func(e *Event)) []byte {
		e := validRevoke()
		mutate(e)
		return mustMarshal(t, e)
	}
	verdict := func(mutate func(e *Event)) []byte {
		e := validVerdict()
		mutate(e)
		return mustMarshal(t, e)
	}
	indicator := func(kind, value, scope string) []byte {
		return verdict(func(e *Event) { e.Indicator = Indicator{Kind: kind, Value: value, Scope: scope} })
	}
	return []schemaSample{
		// Valid events.
		{"verdict", []byte(valid), true},
		{"revocation", mustMarshal(t, validRevoke()), true},
		{"unsigned verdict", verdict(func(e *Event) { e.Publisher.Signature = "" }), true},
		{"minimal verdict", verdict(func(e *Event) {
			e.Evidence.LogHash, e.MITRE, e.Publisher.ASN = "", nil, nil
		}), true},
		{"ipv6", indicator(KindIPv6, "2a01:4f8:c17:b8f::2", "/128"), true},
		{"ipv6 ending in ::", indicator(KindIPv6, "2a01:4f8::", "/128"), true},
		{"ipv4 cidr", indicator(KindCIDR, "45.83.64.0/22", "/22"), true},
		{"ipv6 cidr", indicator(KindCIDR, "2a01:4f8::/32", "/32"), true},
		{"confidence 0", withField(t, "verdict.confidence", `0`), true},
		{"confidence 1", withField(t, "verdict.confidence", `1`), true},
		{"shortest ttl", withField(t, "verdict.ttl_seconds", `60`), true},
		{"longest ttl", withField(t, "verdict.ttl_seconds", `2592000`), true},
		{"largest events", withField(t, "evidence.events", `9007199254740991`), true},
		{"largest asn", withField(t, "publisher.asn", `4294967295`), true},

		// Envelope.
		{"not an object", []byte(`[]`), false},
		{"unknown spec", withField(t, "spec", `"obie/0.2"`), false},
		{"missing spec", withField(t, "spec", ""), false},
		{"unknown type", withField(t, "type", `"indicator.appeal"`), false},
		{"missing type", withField(t, "type", ""), false},
		{"missing id", withField(t, "id", ""), false},
		{"uuid v4 id", withField(t, "id", `"01923e4a-7b2c-4def-8a12-3456789abcde"`), false},
		{"upper-case id", withField(t, "id", `"01923E4A-7B2C-7DEF-8A12-3456789ABCDE"`), false},
		{"issued_at with offset", withField(t, "issued_at", `"2026-01-05T02:50:00+01:00"`), false},
		{"issued_at with fraction", withField(t, "issued_at", `"2026-01-05T01:50:00.5Z"`), false},
		{"issued_at as number", withField(t, "issued_at", `1767577800`), false},
		{"unknown top-level key", withField(t, "raw_log", `"Failed password for root"`), false},
		{"unknown nested key", withField(t, "evidence.source_ip", `"10.0.0.1"`), false},
		{"key wrong case", []byte(strings.Replace(valid, `"protocol"`, `"Protocol"`, 1)), false},
		{"null value", withField(t, "publisher.asn", `null`), false},

		// Indicator.
		{"unsupported kind", indicator("fqdn", "evil.example", "/0"), false},
		{"upper-case kind", indicator("IPV4", "85.10.20.30", "/32"), false},
		{"ipv4 with leading zero", indicator(KindIPv4, "85.10.020.30", "/32"), false},
		{"ipv4 out of range", indicator(KindIPv4, "85.10.256.30", "/32"), false},
		{"ipv6 as ipv4", indicator(KindIPv4, "2a01:4f8::1", "/32"), false},
		{"ipv6 upper case", indicator(KindIPv6, "2A01:4F8::1", "/128"), false},
		{"ipv6 leading zero", indicator(KindIPv6, "2a01:04f8::1", "/128"), false},
		{"ipv6 outside 2000::/3", indicator(KindIPv6, "fd00::1", "/128"), false},
		{"ipv4 wrong scope", indicator(KindIPv4, "85.10.20.30", "/24"), false},
		{"ipv6 wrong scope", indicator(KindIPv6, "2a01:4f8::1", "/64"), false},
		{"cidr too broad", indicator(KindCIDR, "85.10.0.0/15", "/15"), false},
		{"cidr single address", indicator(KindCIDR, "85.10.20.30/32", "/32"), false},
		{"ipv6 cidr too broad", indicator(KindCIDR, "2a00::/31", "/31"), false},
		{"missing scope", withField(t, "indicator.scope", ""), false},

		// Verdict fields.
		{"verdict without protocol", withField(t, "protocol", ""), false},
		{"protocol upper case", withField(t, "protocol", `"SSH"`), false},
		{"empty protocol", withField(t, "protocol", `""`), false},
		{"verdict without evidence", withField(t, "evidence", ""), false},
		{"verdict without verdict", withField(t, "verdict", ""), false},
		{"zero events", withField(t, "evidence.events", `0`), false},
		{"too many events", withField(t, "evidence.events", `9007199254740992`), false},
		{"fractional events", withField(t, "evidence.events", `47.5`), false},
		{"reason with space", withField(t, "evidence.reason", `"password bruteforce"`), false},
		{"non-ascii reason", withField(t, "evidence.reason", `"grüße"`), false},
		{"empty log_hash", withField(t, "evidence.log_hash", `""`), false},
		{"md5 log_hash", withField(t, "evidence.log_hash", `"md5:d41d8cd98f00b204e9800998ecf8427e"`), false},
		{"honeypot as string", withField(t, "evidence.honeypot", `"true"`), false},
		{"unknown action", withField(t, "verdict.suggested_action", `"drop"`), false},
		{"negative confidence", withField(t, "verdict.confidence", `-0.1`), false},
		{"confidence above 1", withField(t, "verdict.confidence", `1.5`), false},
		{"confidence as string", withField(t, "verdict.confidence", `"0.9"`), false},
		{"ttl too short", withField(t, "verdict.ttl_seconds", `59`), false},
		{"ttl too long", withField(t, "verdict.ttl_seconds", `2592001`), false},
		{"empty mitre", withField(t, "mitre", `[]`), false},
		{"duplicate mitre", withField(t, "mitre", `["T1110","T1110"]`), false},
		{"bad mitre", withField(t, "mitre", `["T111"]`), false},
		{"verdict with revokes", withField(t, "revokes", `"`+testOtherID+`"`), false},
		{"verdict with reason", withField(t, "reason", `"false_positive"`), false},

		// Revocation fields.
		{"revocation without revokes", revoke(func(e *Event) { e.Revokes = "" }), false},
		{"revocation without reason", revoke(func(e *Event) { e.Reason = "" }), false},
		{"revocation with protocol", revoke(func(e *Event) { e.Protocol = "ssh" }), false},
		{"revocation with verdict", revoke(func(e *Event) { e.Verdict = validVerdict().Verdict }), false},
		{"revocation with evidence", revoke(func(e *Event) { e.Evidence = validVerdict().Evidence }), false},
		{"revocation with mitre", revoke(func(e *Event) { e.MITRE = []string{"T1110"} }), false},
		{"revokes not a uuid v7", revoke(func(e *Event) { e.Revokes = "not-a-uuid" }), false},

		// Publisher.
		{"missing publisher", withField(t, "publisher", ""), false},
		{"peer id with 0", withField(t, "publisher.peer_id", `"12D3KooW0000000000000000000000000000000000000000"`), false},
		{"asn 0", withField(t, "publisher.asn", `0`), false},
		{"asn above 32 bits", withField(t, "publisher.asn", `4294967296`), false},
		{"missing signature", withField(t, "publisher.signature", ""), false},
		{"signature without prefix", withField(t, "publisher.signature", `"`+strings.Repeat("A", 86)+`"`), false},
		{"signature too short", withField(t, "publisher.signature", `"ed25519:`+strings.Repeat("A", 85)+`"`), false},
		{"padded signature", withField(t, "publisher.signature", `"ed25519:`+strings.Repeat("A", 86)+`=="`), false},
		{"signature with non-zero padding bits", withField(t, "publisher.signature", `"ed25519:`+strings.Repeat("A", 85)+`B"`), false},
		{"standard base64 signature", withField(t, "publisher.signature", `"ed25519:+`+strings.Repeat("A", 85)+`"`), false},
	}
}

// TestSchemaAgreesWithDecode validates every sample against the published
// schema and asserts that Decode reaches the same verdict.
func TestSchemaAgreesWithDecode(t *testing.T) {
	sch := compileSchema(t)
	for _, s := range schemaSamples(t) {
		t.Run(s.name, func(t *testing.T) {
			schemaOK, schemaErr := schemaAccepts(t, sch, s.data)
			_, decodeErr := Decode(s.data, fixedClock())
			if schemaOK != s.valid || (decodeErr == nil) != s.valid {
				t.Errorf("want valid = %v; schema error = %v; Decode error = %v", s.valid, schemaErr, decodeErr)
			}
		})
	}
}

// TestSchemaAcceptsVectors checks every published test vector: the schema
// accepts its event exactly when the vector is protocol-valid, and Decode
// agrees.
func TestSchemaAcceptsVectors(t *testing.T) {
	sch := compileSchema(t)
	files, err := filepath.Glob(filepath.Join(vectorDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no test vectors: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			v := readVector(t, file)
			var e Event
			if err := json.Unmarshal(v.Event, &e); err != nil {
				t.Fatal(err)
			}
			schemaOK, schemaErr := schemaAccepts(t, sch, v.Event)
			_, decodeErr := Decode(v.Event, WithClock(func() time.Time { return e.IssuedAt.Time }))
			if schemaOK != v.ProtocolValid || (decodeErr == nil) != v.ProtocolValid {
				t.Errorf("want protocol_valid = %v; schema error = %v; Decode error = %v", v.ProtocolValid, schemaErr, decodeErr)
			}
		})
	}
}

// TestSchemaLimits documents the rules that JSON Schema cannot express. Each
// sample is valid against the schema but rejected by Decode; the
// specification lists the same rules in "Rules beyond the schema".
func TestSchemaLimits(t *testing.T) {
	valid := string(mustMarshal(t, validVerdict()))
	indicator := func(kind, value, scope string) []byte {
		e := validVerdict()
		e.Indicator = Indicator{Kind: kind, Value: value, Scope: scope}
		return mustMarshal(t, e)
	}
	tests := []struct {
		name string
		data []byte
	}{
		{"larger than MaxEventSize", []byte(valid[:len(valid)-1] + strings.Repeat(" ", MaxEventSize) + "}")},
		{"duplicate key", []byte(strings.Replace(valid, `"protocol":"ssh"`, `"protocol":"ssh","protocol":"rdp"`, 1))},
		{"integer written with a fraction", withField(t, "evidence.events", `47.0`)},
		{"integer written with an exponent", withField(t, "verdict.ttl_seconds", `6e2`)},
		{"negative zero confidence", withField(t, "verdict.confidence", `-0`)},
		{"impossible date", withField(t, "issued_at", `"2026-02-30T00:00:00Z"`)},
		{"issued_at in the future", withField(t, "issued_at", `"2026-01-05T02:00:00Z"`)},
		{"private ipv4", indicator(KindIPv4, "10.0.0.1", "/32")},
		{"documentation ipv4", indicator(KindIPv4, "203.0.113.7", "/32")},
		{"special-purpose ipv6", indicator(KindIPv6, "2001:db8::1", "/128")},
		{"cidr in shared address space", indicator(KindCIDR, "100.64.0.0/16", "/16")},
		{"ipv6 with two ::", indicator(KindIPv6, "2a01::1::2", "/128")},
		{"ipv6 not in shortest form", indicator(KindIPv6, "2a01:4f8:0:0:0:0:0:1", "/128")},
		{"cidr with host bits", indicator(KindCIDR, "45.83.64.1/22", "/22")},
		{"scope differs from cidr prefix", indicator(KindCIDR, "45.83.64.0/22", "/23")},
		{"revocation of itself", func() []byte {
			e := validRevoke()
			e.Revokes = e.ID
			return mustMarshal(t, e)
		}()},
	}
	sch := compileSchema(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schemaOK, schemaErr := schemaAccepts(t, sch, tt.data)
			if !schemaOK {
				t.Errorf("schema rejects the sample (%v); move it to schemaSamples", schemaErr)
			}
			if _, err := Decode(tt.data, fixedClock()); err == nil {
				t.Error("Decode() accepts the sample")
			}
		})
	}
}
