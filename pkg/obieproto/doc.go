// Package obieproto defines the obie/0.1 event model shared between OBIE
// nodes and third-party implementations, together with its strict validation.
//
// An [Event] is either an indicator verdict ([TypeVerdict]) or the revocation
// of an earlier verdict ([TypeRevoke]). v0.1 indicators are single IPv4 or
// IPv6 addresses and CIDR ranges; internal and special-purpose address ranges
// are never valid indicators.
//
// Receivers use [Decode], which rejects oversized input, unknown or
// duplicate JSON keys and every rule violation. Publishers build an [Event],
// call [Event.Normalize] to put the indicator into canonical form and then
// [Event.Validate]. Validation never rewrites a received event: values that
// are not already canonical are rejected rather than silently fixed.
//
// Validation checks the format of publisher.signature only; verifying it is
// the job of the signing layer.
package obieproto

//go:generate go test -run ^TestVectors$ -update
