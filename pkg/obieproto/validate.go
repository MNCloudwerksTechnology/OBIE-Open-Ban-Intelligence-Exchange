package obieproto

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var (
	uuidV7Pattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	protocolPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	reasonPattern   = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	logHashPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	mitrePattern    = regexp.MustCompile(`^T[0-9]{4}(\.[0-9]{3})?$`)
	// peerIDPattern accepts a libp2p peer ID in its legacy base58btc text
	// form ("12D3KooW..." for Ed25519 keys), the only form obie/0.1 uses. The
	// CIDv1 form ("bafz...") is deliberately not accepted.
	peerIDPattern = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]{32,128}$`)
)

// signaturePrefix introduces an Ed25519 signature in publisher.signature.
const signaturePrefix = "ed25519:"

// Validate checks the event against every obie/0.1 rule. It never modifies
// the event; call [Event.Normalize] first when building an event. Errors
// match one of the package's sentinel errors via [errors.Is].
func (e *Event) Validate(opts ...Option) error {
	o := newOptions(opts)
	if e.Spec != Spec {
		return &FieldError{Field: "spec", Err: ErrUnsupportedSpec, Detail: fmt.Sprintf("%q", e.Spec)}
	}
	if e.Type != TypeVerdict && e.Type != TypeRevoke {
		return &FieldError{Field: "type", Err: ErrUnsupportedType, Detail: fmt.Sprintf("%q", e.Type)}
	}
	if !uuidV7Pattern.MatchString(e.ID) {
		return invalid("id", "%q is not a lower-case UUIDv7", e.ID)
	}
	if err := e.validateIssuedAt(o); err != nil {
		return err
	}
	if err := e.Indicator.validate(o); err != nil {
		return err
	}
	validateType := e.validateVerdict
	if e.Type == TypeRevoke {
		validateType = e.validateRevoke
	}
	if err := validateType(); err != nil {
		return err
	}
	if err := e.Publisher.validate(); err != nil {
		return err
	}
	return e.validateSize()
}

func (e *Event) validateIssuedAt(o options) error {
	t := e.IssuedAt.UTC()
	if t.IsZero() || t.Nanosecond() != 0 || t.Year() > 9999 {
		return invalid("issued_at", "must be a UTC time with second precision")
	}
	if limit := o.now().Add(MaxClockSkew); t.After(limit) {
		return invalid("issued_at", "%s is more than %s in the future", t.Format(TimestampLayout), MaxClockSkew)
	}
	return nil
}

func (e *Event) validateVerdict() error {
	if err := forbid(TypeRevoke, presence{"revokes", e.Revokes != ""}, presence{"reason", e.Reason != ""}); err != nil {
		return err
	}
	if !protocolPattern.MatchString(e.Protocol) {
		return invalid("protocol", "%q must match %s", e.Protocol, protocolPattern)
	}
	if e.Evidence == nil {
		return invalid("evidence", "missing")
	}
	if err := e.Evidence.validate(); err != nil {
		return err
	}
	if e.Verdict == nil {
		return invalid("verdict", "missing")
	}
	if err := e.Verdict.validate(); err != nil {
		return err
	}
	return validateMITRE(e.MITRE)
}

func (e *Event) validateRevoke() error {
	err := forbid(TypeVerdict,
		presence{"protocol", e.Protocol != ""},
		presence{"evidence", e.Evidence != nil},
		presence{"verdict", e.Verdict != nil},
		presence{"mitre", e.MITRE != nil})
	if err != nil {
		return err
	}
	if !uuidV7Pattern.MatchString(e.Revokes) {
		return invalid("revokes", "%q is not a lower-case UUIDv7", e.Revokes)
	}
	if e.Revokes == e.ID {
		return invalid("revokes", "an event cannot revoke itself")
	}
	if !reasonPattern.MatchString(e.Reason) {
		return invalid("reason", "%q must match %s", e.Reason, reasonPattern)
	}
	return nil
}

// presence records whether an optional field is set.
type presence struct {
	field string
	set   bool
}

// forbid rejects the first set field; those fields are only allowed in events
// of type allowedIn.
func forbid(allowedIn string, fields ...presence) error {
	for _, f := range fields {
		if f.set {
			return invalid(f.field, "only allowed in %s", allowedIn)
		}
	}
	return nil
}

func (ev *Evidence) validate() error {
	if ev.Events < 1 {
		return invalid("evidence.events", "%d must be at least 1", ev.Events)
	}
	if !reasonPattern.MatchString(ev.Reason) {
		return invalid("evidence.reason", "%q must match %s", ev.Reason, reasonPattern)
	}
	if ev.LogHash != "" && !logHashPattern.MatchString(ev.LogHash) {
		return invalid("evidence.log_hash", "%q must be sha256:<64 lower-case hex> or empty", ev.LogHash)
	}
	return nil
}

func (v *Verdict) validate() error {
	if v.SuggestedAction != ActionBan && v.SuggestedAction != ActionWatch {
		return invalid("verdict.suggested_action", "%q must be %q or %q", v.SuggestedAction, ActionBan, ActionWatch)
	}
	// Written as a negated range so that NaN is rejected too.
	if !(v.Confidence >= 0 && v.Confidence <= 1) {
		return invalid("verdict.confidence", "%v must be in [0, 1]", v.Confidence)
	}
	if v.TTLSeconds < MinTTLSeconds || v.TTLSeconds > MaxTTLSeconds {
		return invalid("verdict.ttl_seconds", "%d must be in [%d, %d]", v.TTLSeconds, MinTTLSeconds, MaxTTLSeconds)
	}
	return nil
}

func validateMITRE(ids []string) error {
	if ids != nil && len(ids) == 0 {
		return invalid("mitre", "must be omitted rather than empty")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !mitrePattern.MatchString(id) {
			return invalid("mitre", "%q is not a MITRE ATT&CK technique ID", id)
		}
		if seen[id] {
			return invalid("mitre", "%q is listed twice", id)
		}
		seen[id] = true
	}
	return nil
}

func (p *Publisher) validate() error {
	if !peerIDPattern.MatchString(p.PeerID) {
		return invalid("publisher.peer_id", "%q is not a base58 libp2p peer ID", p.PeerID)
	}
	if p.ASN != nil && *p.ASN == 0 {
		return invalid("publisher.asn", "0 is reserved")
	}
	if p.Signature == "" {
		return nil
	}
	if _, err := decodeSignature(p.Signature); err != nil {
		return invalid("publisher.signature", "%v", err)
	}
	return nil
}

func (e *Event) validateSize() error {
	data, err := json.Marshal(e)
	if err != nil {
		return &FieldError{Err: ErrMalformed, Detail: err.Error()}
	}
	if len(data) > MaxEventSize {
		return &FieldError{Err: ErrTooLarge, Detail: fmt.Sprintf("%d bytes exceeds %d", len(data), MaxEventSize)}
	}
	return nil
}
