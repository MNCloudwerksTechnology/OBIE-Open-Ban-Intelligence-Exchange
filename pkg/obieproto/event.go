package obieproto

import "time"

// Spec is the protocol version identifier carried in every event.
const Spec = "obie/0.1"

// Event types defined by obie/0.1.
const (
	// TypeVerdict announces that the publisher considers an indicator hostile.
	TypeVerdict = "indicator.verdict"
	// TypeRevoke withdraws an earlier verdict of the same publisher.
	TypeRevoke = "indicator.revoke"
)

// Indicator kinds defined by obie/0.1.
const (
	// KindIPv4 is a single IPv4 address; its scope is always "/32".
	KindIPv4 = "ipv4"
	// KindIPv6 is a single IPv6 address; its scope is always "/128".
	KindIPv6 = "ipv6"
	// KindCIDR is an IPv4 network of /16 to /31 or an IPv6 network of /32 to
	// /127; its scope is the prefix length.
	KindCIDR = "cidr"
)

// Suggested actions of a verdict.
const (
	ActionBan   = "ban"
	ActionWatch = "watch"
)

// Protocol limits.
const (
	// MaxEventSize is the maximum size of a serialized event in bytes.
	MaxEventSize = 4096
	// MinTTLSeconds is the shortest allowed verdict lifetime.
	MinTTLSeconds = 60
	// MaxTTLSeconds is the longest allowed verdict lifetime (30 days).
	MaxTTLSeconds = 2592000
	// MaxClockSkew is how far issued_at may lie in the future.
	MaxClockSkew = 5 * time.Minute
	// MaxEvidenceEvents is the largest evidence.events, 2^53-1: the largest
	// integer the canonical (signed) form represents exactly.
	MaxEvidenceEvents = 1<<53 - 1
)

// Event is an obie/0.1 envelope. Which optional parts are required depends on
// Type; see [Event.Validate].
type Event struct {
	// ID is the event's UUIDv7 in canonical lower-case form.
	ID string `json:"id"`
	// Spec is always [Spec].
	Spec string `json:"spec"`
	// Type is [TypeVerdict] or [TypeRevoke].
	Type string `json:"type"`
	// IssuedAt is when the publisher created the event.
	IssuedAt Timestamp `json:"issued_at"`
	// Indicator is the subject of the event.
	Indicator Indicator `json:"indicator"`
	// Protocol is the attacked service, e.g. "ssh" (verdicts only).
	Protocol string `json:"protocol,omitempty"`
	// Evidence summarizes what the publisher observed (verdicts only).
	Evidence *Evidence `json:"evidence,omitempty"`
	// Verdict is the publisher's recommendation (verdicts only).
	Verdict *Verdict `json:"verdict,omitempty"`
	// MITRE lists MITRE ATT&CK technique IDs, e.g. "T1110" (verdicts only).
	MITRE []string `json:"mitre,omitempty"`
	// Revokes is the ID of the withdrawn verdict (revocations only).
	Revokes string `json:"revokes,omitempty"`
	// Reason explains a revocation, e.g. "false_positive" (revocations only).
	Reason string `json:"reason,omitempty"`
	// Publisher identifies and authenticates the issuing node.
	Publisher Publisher `json:"publisher"`
}

// Indicator identifies the attacker.
type Indicator struct {
	// Kind is "ipv4", "ipv6" or "cidr".
	Kind string `json:"kind"`
	// Value is the canonical address or network.
	Value string `json:"value"`
	// Scope is the prefix length, e.g. "/32"; derived from Kind and Value.
	Scope string `json:"scope"`
}

// Evidence summarizes the observation behind a verdict.
type Evidence struct {
	// Events is the number of malicious events observed (at least 1).
	Events int64 `json:"events"`
	// Reason classifies the behavior, e.g. "password_bruteforce".
	Reason string `json:"reason"`
	// LogHash is "sha256:<64 hex>" of the underlying log excerpt, or empty.
	LogHash string `json:"log_hash,omitempty"`
	// Honeypot is true if the activity hit a honeypot.
	Honeypot bool `json:"honeypot"`
}

// Verdict is the publisher's recommendation for an indicator.
type Verdict struct {
	// SuggestedAction is [ActionBan] or [ActionWatch].
	SuggestedAction string `json:"suggested_action"`
	// Confidence is the publisher's confidence in [0, 1].
	Confidence float64 `json:"confidence"`
	// TTLSeconds is the verdict lifetime, in [MinTTLSeconds, MaxTTLSeconds].
	TTLSeconds int64 `json:"ttl_seconds"`
}

// Publisher identifies the issuing node.
type Publisher struct {
	// PeerID is the node's libp2p peer ID.
	PeerID string `json:"peer_id"`
	// ASN is the publisher's autonomous system number, if disclosed.
	ASN *uint32 `json:"asn,omitempty"`
	// Signature is "ed25519:<base64url>" or empty for an unsigned event.
	Signature string `json:"signature"`
}

// ExpiresAt returns when the event stops being relevant. A verdict expires
// after its TTL. A revocation carries no TTL; it stays relevant for
// MaxTTLSeconds, the longest any revoked verdict can live.
func (e *Event) ExpiresAt() time.Time {
	ttl := int64(MaxTTLSeconds)
	if e.Type == TypeVerdict && e.Verdict != nil {
		ttl = e.Verdict.TTLSeconds
	}
	return e.IssuedAt.Add(time.Duration(ttl) * time.Second)
}

// Expired reports whether the event is no longer relevant at now.
func (e *Event) Expired(now time.Time) bool {
	return !now.Before(e.ExpiresAt())
}

// Key returns a stable storage key for the event's indicator, e.g.
// "ipv4:203.0.113.7" or "cidr:198.51.100.0/24". It is only meaningful for
// normalized indicators, which every validated event has.
func (e *Event) Key() string {
	return e.Indicator.Key()
}

// Key returns a stable storage key for the indicator; see [Event.Key].
func (i Indicator) Key() string {
	return i.Kind + ":" + i.Value
}
