package obieproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
)

// field describes one JSON key of the obie/0.1 schema.
type field struct {
	// object is the schema of a nested object, nil for scalars and arrays.
	object schema
	// optional keys may be omitted. When present they must not be null or
	// the empty string, so that a decoded event re-encodes to the same keys.
	// Required keys must be present; null is never allowed.
	optional bool
}

// schema maps each allowed key of a JSON object to its description.
type schema map[string]field

var eventSchema = schema{
	"id":        {},
	"spec":      {},
	"type":      {},
	"issued_at": {},
	"indicator": {object: schema{"kind": {}, "value": {}, "scope": {}}},
	"protocol":  {optional: true},
	"evidence": {optional: true, object: schema{
		"events":   {},
		"reason":   {},
		"log_hash": {optional: true},
		"honeypot": {},
	}},
	"verdict": {optional: true, object: schema{
		"suggested_action": {},
		"confidence":       {},
		"ttl_seconds":      {},
	}},
	"mitre":   {optional: true},
	"revokes": {optional: true},
	"reason":  {optional: true},
	"publisher": {object: schema{
		"peer_id":   {},
		"asn":       {optional: true},
		"signature": {},
	}},
}

// Decode parses and validates a received obie/0.1 event. It is strict:
// input larger than [MaxEventSize], trailing data, unknown or duplicate keys
// (matched case-sensitively), missing required keys, null values and empty
// optional strings (an empty log_hash is expressed by omitting the key) are
// rejected, and the decoded event must pass [Event.Validate] with opts.
//
// The spec and type are checked before the rest of the structure, so an
// event of a newer spec or an unknown type reports [ErrUnsupportedSpec] or
// [ErrUnsupportedType] rather than [ErrUnknownField]. A spec key that is
// missing, not a string or not spelled exactly "spec" also reports
// [ErrUnsupportedSpec]: the input cannot be identified as obie/0.1.
//
// Decode checks the signature's format only. Receivers must verify the
// signature before acting on an event.
func Decode(data []byte, opts ...Option) (*Event, error) {
	if len(data) > MaxEventSize {
		return nil, &FieldError{Err: ErrTooLarge, Detail: fmt.Sprintf("%d bytes exceeds %d", len(data), MaxEventSize)}
	}
	if err := checkSpecAndType(data); err != nil {
		return nil, err
	}
	if err := checkObject(data, "", eventSchema); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var e Event
	if err := dec.Decode(&e); err != nil {
		return nil, &FieldError{Err: ErrMalformed, Detail: err.Error()}
	}
	if e.Verdict != nil && e.Verdict.Confidence == 1 {
		if err := checkConfidenceLiteral(data); err != nil {
			return nil, err
		}
	}
	if err := e.Validate(opts...); err != nil {
		return nil, err
	}
	return &e, nil
}

// confidencePrec is the precision, in bits, at which a confidence literal is
// compared with 0 and 1. A literal of at most MaxEventSize characters that
// lies outside [0, 1] differs from the bound by more than 10^-4200, far more
// than a rounding error at this precision.
const confidencePrec = 1 << 15

// checkConfidenceLiteral rejects a verdict.confidence whose literal lies
// outside [0, 1] although its binary64 value does not, e.g.
// 1.0000000000000001, which rounds to 1. The range applies to the value as
// written, as a JSON Schema validator comparing decimals exactly sees it.
// Rounding is monotonic, so only a literal that rounds to 1 can exceed 1; a
// negative literal rounds to a negative value or -0, which Validate rejects.
// Callers therefore only check literals whose binary64 value is 1, which
// also keeps literals with huge exponents away from the slow big.Float
// parser. data has already been decoded successfully.
func checkConfidenceLiteral(data []byte) error {
	var v struct {
		Verdict *struct {
			Confidence json.Number `json:"confidence"`
		} `json:"verdict"`
	}
	if err := json.Unmarshal(data, &v); err != nil || v.Verdict == nil {
		return nil
	}
	literal := string(v.Verdict.Confidence)
	f, _, err := big.ParseFloat(literal, 10, confidencePrec, big.ToNearestEven)
	if err != nil {
		return invalid("verdict.confidence", "%s is not a number", literal)
	}
	if f.Sign() < 0 || f.Cmp(big.NewFloat(1)) > 0 {
		return invalid("verdict.confidence", "%s must be in [0, 1]", literal)
	}
	return nil
}

// checkSpecAndType looks only at the top-level spec and type keys.
func checkSpecAndType(data []byte) error {
	var head map[string]json.RawMessage
	if err := json.Unmarshal(data, &head); err != nil || head == nil {
		return &FieldError{Err: ErrMalformed, Detail: "not a single JSON object"}
	}
	var spec, typ string
	if err := json.Unmarshal(head["spec"], &spec); err != nil || spec != Spec {
		return &FieldError{Field: "spec", Err: ErrUnsupportedSpec, Detail: string(head["spec"])}
	}
	if err := json.Unmarshal(head["type"], &typ); err != nil || (typ != TypeVerdict && typ != TypeRevoke) {
		return &FieldError{Field: "type", Err: ErrUnsupportedType, Detail: string(head["type"])}
	}
	return nil
}

// checkObject verifies that data is exactly one JSON object whose keys are
// all defined by s, each at most once, and recurses into nested objects.
func checkObject(data []byte, path string, s schema) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return malformed(path, "must be a JSON object")
	}
	seen := make(map[string]bool, len(s))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return malformed(path, err.Error())
		}
		key, _ := tok.(string)
		keyPath := joinPath(path, key)
		f, known := s[key]
		if !known || seen[key] {
			return &FieldError{Field: keyPath, Err: ErrUnknownField, Detail: "not defined by obie/0.1 or repeated"}
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return malformed(keyPath, err.Error())
		}
		if err := checkValue(value, keyPath, f); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return malformed(path, err.Error())
	}
	if err := checkRequired(path, s, seen); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return malformed(path, "trailing data after the event")
	}
	return nil
}

func checkValue(value json.RawMessage, path string, f field) error {
	switch {
	case bytes.Equal(value, []byte("null")):
		return malformed(path, "null is not allowed")
	case f.optional && bytes.Equal(value, []byte(`""`)):
		return invalid(path, "must be omitted rather than empty")
	case f.object != nil:
		return checkObject(value, path, f.object)
	}
	return nil
}

// checkRequired reports the alphabetically first missing required key, so
// the error is deterministic.
func checkRequired(path string, s schema, seen map[string]bool) error {
	var missing []string
	for key, f := range s {
		if !f.optional && !seen[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	return invalid(joinPath(path, missing[0]), "missing")
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func malformed(path, detail string) error {
	return &FieldError{Field: path, Err: ErrMalformed, Detail: detail}
}
