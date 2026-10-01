package obieproto

import (
	"errors"
	"fmt"
)

// Sentinel errors. Every error returned by [Decode], [Event.Validate],
// [CanonicalBytes], [Verify] and [Receive] matches exactly one of them via
// [errors.Is].
var (
	// ErrMalformed reports input that is not a single well-formed JSON object
	// of the expected shape.
	ErrMalformed = errors.New("obieproto: malformed event")
	// ErrTooLarge reports an event whose serialized form exceeds MaxEventSize.
	ErrTooLarge = errors.New("obieproto: event too large")
	// ErrUnknownField reports a JSON key that obie/0.1 does not define, or a
	// key that appears more than once.
	ErrUnknownField = errors.New("obieproto: unknown or duplicate field")
	// ErrUnsupportedSpec reports a spec version other than obie/0.1.
	ErrUnsupportedSpec = errors.New("obieproto: unsupported spec")
	// ErrUnsupportedType reports an event type that obie/0.1 does not define.
	ErrUnsupportedType = errors.New("obieproto: unsupported event type")
	// ErrUnsupportedIndicator reports an indicator kind that obie/0.1 does not
	// define (for example fqdn, ja3 or sha256 from the whitepaper).
	ErrUnsupportedIndicator = errors.New("obieproto: unsupported indicator kind")
	// ErrNonPublicIndicator reports an indicator inside a loopback,
	// link-local, multicast, unspecified, private, documentation or otherwise
	// special-purpose range. A node must never publish internal addresses.
	ErrNonPublicIndicator = errors.New("obieproto: indicator is not a public address")
	// ErrInvalidField reports a field value that violates an obie/0.1 rule.
	ErrInvalidField = errors.New("obieproto: invalid field")
	// ErrInvalidSignature reports a publisher.signature that is missing,
	// malformed, or does not match the event and the publisher's key.
	ErrInvalidSignature = errors.New("obieproto: invalid signature")
	// ErrPublisherMismatch reports a publisher.peer_id that is not the peer
	// ID of the signing key: when signing, the key does not belong to the
	// peer ID; when verifying, the peer ID embeds no Ed25519 key.
	ErrPublisherMismatch = errors.New("obieproto: publisher does not match the signing key")
	// ErrClockSkew reports an issued_at more than MaxClockSkew ahead of the
	// receiver's clock.
	ErrClockSkew = errors.New("obieproto: issued_at too far in the future")
	// ErrExpired reports a received event that is no longer relevant: a
	// verdict past its TTL or a revocation older than MaxTTLSeconds.
	ErrExpired = errors.New("obieproto: event expired")
)

// FieldError describes which field of an event failed validation. It
// unwraps to one of the sentinel errors.
type FieldError struct {
	// Field is the JSON path of the offending field, e.g. "verdict.confidence".
	Field string
	// Err is the sentinel error the violation belongs to.
	Err error
	// Detail is a human-readable explanation.
	Detail string
}

func (e *FieldError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%v: %s", e.Err, e.Detail)
	}
	return fmt.Sprintf("%v: %s: %s", e.Err, e.Field, e.Detail)
}

func (e *FieldError) Unwrap() error { return e.Err }

func invalid(field, format string, args ...any) error {
	return &FieldError{Field: field, Err: ErrInvalidField, Detail: fmt.Sprintf(format, args...)}
}
