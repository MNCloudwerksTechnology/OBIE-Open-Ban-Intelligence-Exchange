// Package obieproto defines the obie/0.1 event model shared between OBIE
// nodes and third-party implementations, together with its strict validation
// and its Ed25519 signatures.
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
// Publishers then call [Sign] with the node's Ed25519 key, whose libp2p peer
// ID ([PeerIDFromPublicKey]) must be publisher.peer_id. Receivers call
// [Verify] after [Decode]; it takes the public key from the peer ID, so no
// key lookup is needed. The signature covers [CanonicalBytes]: the RFC 8785
// (JSON Canonicalization Scheme) form of the event without
// publisher.signature, encoded as "ed25519:" + unpadded base64url. Validation
// checks the signature's format only, and verification does not check the
// field rules: a receiver needs both.
//
// Nodes exchange events on the GossipSub topic [Topic], one event per
// message. [Receive] is the complete check for a received message: it
// decodes, binds the message author to publisher.peer_id, drops expired
// events and verifies the signature. [Supersedes] and [Event.Withdraws]
// state the rules that decide which verdicts are in effect.
//
// documentation/spec/obie-0.1.md is the protocol specification and
// documentation/spec/obie-0.1.schema.json its JSON Schema.
//
// documentation/spec/test-vectors publishes signing test vectors; `go
// generate` regenerates them from this package's tests.
package obieproto

//go:generate go test -run ^TestVectors$ -update
