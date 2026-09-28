# obie/0.1: the OBIE event protocol

- **Version:** obie/0.1
- **Status:** Stable for the v0.1.0 release
- **Date:** 2026-09-27
- **Schema:** [`obie-0.1.schema.json`](obie-0.1.schema.json) (JSON Schema 2020-12)
- **Test vectors:** [`test-vectors/`](test-vectors/README.md)
- **Reference implementation:** Go package
  [`pkg/obieproto`](../../pkg/obieproto/doc.go)

## 1. Introduction

OBIE (Open Ban Intelligence Exchange) nodes share signed statements about
hostile network addresses over a libp2p GossipSub mesh. This document
specifies version obie/0.1 of the event format and of the rules for
publishing, relaying and interpreting events. Its goal is that a third party
can build an interoperable node from this document, the JSON Schema and the
test vectors alone.

obie/0.1 covers:

- two event types: a **verdict** about an indicator and the **revocation** of
  an earlier verdict;
- three indicator kinds: a single IPv4 address, a single IPv6 address and a
  CIDR range, all in public address space;
- deterministic signing with Ed25519 over the RFC 8785 canonical form, with
  the publisher identified by its libp2p peer ID;
- transport as one JSON event per GossipSub message on a single topic.

Out of scope for obie/0.1, and planned for later versions: observations as a
shared event type, appeals, non-IP indicators (FQDN, URL, JA3/JA4, hashes,
ASNs), organisational identity via domain challenge, key rotation
statements, CBOR/COSE encodings and per-protocol topics. Some of these
appear in the whitepaper in the [README](../../README.md); where the
whitepaper and this document differ, this document is authoritative for
obie/0.1.

How a node decides to act on verdicts (trust weights, quorum, enforcement)
is local policy and not part of the protocol.

## 2. Conventions

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY" and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
([RFC 2119], [RFC 8174]) when, and only when, they appear in all capitals.

Every normative statement that uses an absolute requirement carries a tag
such as [ENV-1], in bold where it is defined.
[Appendix A](#appendix-a-requirements-and-tests) maps each tag to the tests
of the reference implementation that check it.

Terms:

- **Node:** an implementation of this protocol, identified by one Ed25519
  key pair.
- **Publisher:** the node that created and signed an event.
- **Receiver:** a node that gets an event from the mesh.
- **Relay:** a receiver that forwards messages to other peers (every
  GossipSub subscriber does).
- **Indicator:** the network address or range an event is about.
- **In effect:** a verdict that a receiver takes into account for its local
  decisions (section 5).
- **Drop:** discard a message: do not act on it, do not store it and do not
  forward it.

Byte sizes are in octets. Times are UTC. "Seconds" are SI seconds without
leap seconds, as in Unix time.

## 3. Overview

A publisher builds an event, normalises its indicator (section 6),
validates it (section 4), signs it (section 7) and publishes the serialized
event as one message on the topic `obie/0.1/verdicts` (section 9). Every
receiver runs the checks of section 10 before it acts on the event or
forwards it, and derives from the accepted events which verdicts are in
effect (section 5).

## 4. Event envelope

### 4.1 Encoding

**[ENC-1]** An event MUST be a single JSON object ([RFC 8259]) encoded in
UTF-8, with nothing before or after it other than JSON whitespace, and its
serialized form MUST NOT exceed 4096 bytes (`MaxEventSize`). Receivers MUST
reject larger input before parsing it.

**[ENC-2]** An event MUST contain only the members defined in section 4.2,
at every level of nesting. Member names are case-sensitive. A receiver MUST
reject an event with an unknown member, a member whose name differs in
case, or a member that appears more than once in the same object.

**[ENC-3]** A member MUST NOT have the value `null`. An optional member
that has no value MUST be omitted: an empty string or empty array is not an
allowed value for any optional member.

**[ENC-4]** Members of type integer MUST be written as JSON integers without
a fraction or exponent (`47`, not `47.0` or `4.7e1`) and MUST lie within
±(2^53−1), the range that RFC 8785 serializes exactly.

Numbers are interpreted as IEEE 754 binary64 values, as I-JSON ([RFC 7493])
requires, except that range constraints apply to the number as written: a
`confidence` of `1.0000000000000001` is out of range although it rounds
to 1. Publishers that format numbers as RFC 8785 does never write such
literals.

The order of members and insignificant whitespace are free; they do not
affect the signature (section 7).

### 4.2 Members

Table 1 lists every member. Column "V" is for verdicts, "R" for revocations:
**req** = required, **opt** = optional, **—** = not allowed.

| Member                        | V   | R   | Type    | Constraint                                                                                                  |
|-------------------------------|-----|-----|---------|-------------------------------------------------------------------------------------------------------------|
| `id`                          | req | req | string  | UUIDv7 ([RFC 9562]) in lower-case canonical form, `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$` |
| `spec`                        | req | req | string  | exactly `obie/0.1`                                                                                          |
| `type`                        | req | req | string  | `indicator.verdict` or `indicator.revoke`                                                                   |
| `issued_at`                   | req | req | string  | creation time, `YYYY-MM-DDTHH:MM:SSZ` (RFC 3339, UTC, whole seconds), a valid calendar date after `0001-01-01T00:00:00Z` |
| `indicator`                   | req | req | object  | section 6                                                                                                   |
| `indicator.kind`              | req | req | string  | `ipv4`, `ipv6` or `cidr`                                                                                    |
| `indicator.value`             | req | req | string  | canonical address or network (section 6.2)                                                                  |
| `indicator.scope`             | req | req | string  | `/` + prefix length, derived from `kind` and `value` (section 6.2)                                          |
| `protocol`                    | req | —   | string  | attacked service, `^[a-z0-9_-]{1,32}$`, e.g. `ssh`, `http`, `smtp`                                          |
| `evidence`                    | req | —   | object  | what the publisher observed                                                                                 |
| `evidence.events`             | req |     | integer | number of malicious events observed, 1 to 2^53−1                                                            |
| `evidence.reason`             | req |     | string  | behaviour class, `^[a-z0-9_]{1,64}$`, e.g. `password_bruteforce`                                            |
| `evidence.log_hash`           | opt |     | string  | `sha256:` + 64 lower-case hex digits: SHA-256 of the log excerpt the publisher keeps (section 13)            |
| `evidence.honeypot`           | req |     | boolean | whether the activity hit a honeypot                                                                         |
| `verdict`                     | req | —   | object  | the publisher's recommendation                                                                              |
| `verdict.suggested_action`    | req |     | string  | `ban` or `watch`                                                                                            |
| `verdict.confidence`          | req |     | number  | 0 to 1 inclusive; `-0` is not allowed                                                                       |
| `verdict.ttl_seconds`         | req |     | integer | lifetime of the verdict, 60 to 2592000 (30 days)                                                            |
| `mitre`                       | opt | —   | array   | 1 or more distinct MITRE ATT&CK technique IDs, each `^T[0-9]{4}(\.[0-9]{3})?$`                             |
| `revokes`                     | —   | req | string  | `id` of the withdrawn verdict (UUIDv7 as for `id`), different from this event's `id`                        |
| `reason`                      | —   | req | string  | why the verdict is withdrawn, `^[a-z0-9_]{1,64}$`, e.g. `false_positive`                                    |
| `publisher`                   | req | req | object  | section 8                                                                                                   |
| `publisher.peer_id`           | req | req | string  | libp2p peer ID of the signing key (section 8)                                                               |
| `publisher.asn`               | opt | opt | integer | the publisher's autonomous system number, 1 to 4294967295, self-declared                                    |
| `publisher.signature`         | req | req | string  | `ed25519:` + 86 characters of unpadded base64url (section 7)                                                |

**[ENV-1]** An event MUST contain every member that Table 1 marks as
required for its type and MUST NOT contain a member that Table 1 marks as
not allowed for its type.

**[ENV-2]** Every member value MUST have the type and satisfy the
constraint given in Table 1.

**[ENV-3]** A revocation MUST NOT name its own `id` in `revokes`.

**[ENV-4]** `issued_at` MUST NOT lie more than 300 seconds (`MaxClockSkew`)
after the receiver's clock at the time of validation. There is no lower
bound: old events are handled by expiry (section 5.3).

Publishers SHOULD create `id` from the event's creation time as UUIDv7
specifies. Receivers treat an event whose `id` they have seen before as a
duplicate, so a publisher that reuses an `id` loses the later event.

`publisher.signature` is the empty string only while a publisher builds an
event; the reference implementation's decoder accepts such events so that
tools can check an unsigned draft, but no receiver accepts one from the mesh
(section 10).

## 5. Event types and semantics

### 5.1 Verdict (`indicator.verdict`)

A verdict states that the publisher considers the indicator hostile, with a
suggested action (`ban`: block it; `watch`: observe it), a confidence and a
lifetime. It summarises the evidence without disclosing it (section 13).
Verdicts are advisory: each receiver decides by its own policy whether and
how to act on them.

A verdict **supersedes** another verdict when both have the same
`publisher.peer_id` and the same indicator (`kind` and `value`), and it has
the later `issued_at` or, for equal `issued_at`, the greater `id` in
byte-wise comparison of the lower-case string.

**[SEM-1]** A receiver MUST treat at most one verdict per publisher and
indicator as in effect: the latest one, that is, the one no other received
verdict supersedes. A verdict that does not supersede the one the receiver
holds MUST be ignored, even if it arrives later.

**[SEM-2]** Verdicts of different publishers, and verdicts about different
indicators, MUST NOT supersede each other. In particular a verdict about a
CIDR range and one about an address inside it are independent.

A later verdict replaces an earlier one in every respect: a publisher lowers
its confidence, changes the action or extends the lifetime by issuing a new
verdict. To end a verdict early, a publisher SHOULD revoke it (section 5.2)
rather than rely on a newer verdict with a short lifetime: once that newer
verdict has expired, a receiver that has forgotten it cannot tell that an
older, still unexpired verdict was superseded.

Receivers SHOULD therefore remember the `issued_at` and `id` of the latest
verdict per publisher and indicator until `issued_at` + 2592000 seconds,
the longest lifetime of any verdict it can have superseded, even after the
latest verdict itself has expired.

### 5.2 Revocation (`indicator.revoke`)

A revocation withdraws an earlier verdict of the same publisher, for example
after a false positive.

**[SEM-3]** A revocation withdraws a verdict if and only if its `revokes`
equals the verdict's `id`, its `publisher.peer_id` equals the verdict's, and
its indicator equals the verdict's. A withdrawn verdict MUST NOT be in
effect. A revocation that matches no verdict in this way MUST NOT change
which verdicts are in effect; in particular nobody but the publisher of a
verdict can revoke it.

Revoking a verdict does not restore the verdict it superseded: after the
revocation the publisher has no verdict in effect on that indicator until it
issues a new one. Publishers SHOULD NOT date a revocation earlier than the
verdict it revokes; section 5.3 relies on revocations outliving their
verdicts.

GossipSub does not preserve order, so a revocation can arrive before its
verdict. Receivers SHOULD remember a revocation whose verdict they have not
seen until the revocation expires (section 5.3) and apply it if the verdict
arrives.

### 5.3 Expiry

A verdict **expires** at `issued_at + verdict.ttl_seconds`. A revocation
expires at `issued_at` + 2592000 seconds, the longest lifetime a verdict it
can revoke may have.

**[SEM-4]** A verdict MUST NOT be in effect at or after its expiry time.

Receivers MAY forget expired events, subject to the recommendations of
sections 5.1 and 5.2 on what to remember. Section 10 drops events that have
already expired on arrival.

## 6. Indicators

### 6.1 Kinds

| `kind` | Value                                     | `scope`             |
|--------|-------------------------------------------|---------------------|
| `ipv4` | one IPv4 address                          | `/32`               |
| `ipv6` | one IPv6 address                          | `/128`              |
| `cidr` | an IPv4 network of prefix length 16 to 31, or an IPv6 network of prefix length 32 to 127 | `/` + prefix length |

**[IND-1]** `indicator.kind` MUST be one of `ipv4`, `ipv6` and `cidr`.
Receivers MUST reject any other kind, including the kinds the whitepaper
mentions for later versions (`fqdn`, `ja3`, `sha256`, …).

A single address is always expressed with kind `ipv4` or `ipv6`, never as a
`/32` or `/128` CIDR range; ranges broader than /16 (IPv4) or /32 (IPv6)
cannot be expressed, which limits the damage of a wrong verdict.

### 6.2 Canonical form

**[IND-2]** `indicator.value` and `indicator.scope` MUST be in canonical
form:

- `ipv4`: dotted decimal without leading zeros, e.g. `85.10.20.30`.
- `ipv6`: the text form of [RFC 5952]: lower-case hexadecimal, no leading
  zeros in a group, the longest run of two or more zero groups (the first
  one if tied) replaced by `::`, no zone identifier, e.g.
  `2a01:4f8:c17:b8f::2`.
- `cidr`: the network address in the form above for its family, `/`, and
  the prefix length in decimal; all host bits are zero, e.g.
  `45.83.64.0/22`, `2a01:4f8::/32`.
- `scope`: `/32` for `ipv4`, `/128` for `ipv6`, and `/` followed by the
  prefix length of `value` for `cidr`.

**[IND-3]** Receivers MUST reject an indicator that is not in canonical form
rather than normalise it: the signature covers the value as sent.

**Normalisation** is the publisher-side procedure that produces the
canonical form from operator or log input: trim surrounding whitespace,
lower-case `kind`, parse `value` as an address (`ipv4`, `ipv6`) or network
(`cidr`) of the given kind, format it canonically, clear the host bits of a
network, and set `scope`. A value that does not parse as the given kind,
an IPv4-mapped IPv6 address given as `ipv4`, a CIDR range broader than the
limits of section 6.1, and a CIDR range covering a single address are
errors, not something to normalise.

**[IND-4]** Publishers MUST normalise the indicator before signing, and
normalising a canonical indicator MUST leave it unchanged.

The storage key of an indicator is `kind + ":" + value`, e.g.
`ipv4:85.10.20.30` or `cidr:45.83.64.0/22`; "same indicator" in section 5
means equal keys.

### 6.3 Public address space only

**[IND-5]** An indicator MUST lie entirely in public address space. For a
CIDR range, every address of the range counts: a range that overlaps any of
the following ranges MUST be rejected, as MUST an address inside one.

- IPv4: `0.0.0.0/8` (this network), `10.0.0.0/8`, `172.16.0.0/12`,
  `192.168.0.0/16` (RFC 1918), `100.64.0.0/10` (shared address space),
  `127.0.0.0/8` (loopback), `169.254.0.0/16` (link-local), `192.0.0.0/24`
  (IETF protocol assignments), `192.88.99.0/24` (6to4 relay anycast),
  `198.18.0.0/15` (benchmarking), `224.0.0.0/4` (multicast), `240.0.0.0/4`
  (reserved, including the limited broadcast address).
- IPv6: everything outside `2000::/3` (global unicast), which excludes
  unspecified, loopback, IPv4-mapped, IPv4-compatible and IPv4-translated
  addresses, NAT64 prefixes, discard-only, unique local, link-local,
  site-local, multicast and unallocated space; and inside it `2001::/23`
  (IETF protocol assignments, including Teredo and benchmarking) and
  `2002::/16` (6to4, which can embed private IPv4 addresses).
- Documentation ranges: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`,
  `2001:db8::/32`, `3fff::/20`.

The reference implementation offers an option to accept documentation
ranges in tests and examples; `Receive`, the check for messages from the
mesh, ignores it. A separate option, `ReceiveDocumentationRanges`, lets
`Receive` accept them for multi-node tests; production nodes never set it.

## 7. Canonicalisation and signing

**[SIG-1]** The signed bytes of an event MUST be the JSON Canonicalization
Scheme ([RFC 8785]) serialization of the event with the member
`publisher.signature` removed. All other members, including the rest of
`publisher`, are covered.

**[SIG-2]** An implementation MUST refuse to canonicalise, and therefore
to sign or verify, input that is not I-JSON ([RFC 7493]): invalid UTF-8,
escaped lone surrogates, duplicate member names, non-finite numbers or
integers beyond ±(2^53−1). Repairing such input (for example replacing
invalid UTF-8 with U+FFFD) would let different events share one signature.

RFC 8785 fixes member order (by UTF-16 code units of the names), string
escaping (only `"`, `\` and control characters) and number formatting
(ECMAScript `Number.prototype.toString`, e.g. a confidence of one third is
`0.3333333333333333`, and `1.0` is `1`). No obie/0.1 member admits
characters outside ASCII, but canonicalisation is defined for all of
Unicode; test vector `05-unicode-string` exercises it.

**[SIG-3]** The signature MUST be an Ed25519 signature ([RFC 8032], pure
Ed25519, no context or pre-hash) of the signed bytes with the private key
of `publisher.peer_id`, and `publisher.signature` MUST be `ed25519:`
followed by the 64-byte signature in base64url without padding
([RFC 4648] section 5): 86 characters whose unused trailing bits are zero.
Receivers MUST reject any other encoding and any signature that does not
verify.

**[SIG-4]** Verifiers MUST reject a public key that is not the canonical
encoding of a curve point or that has small order (its product with the
cofactor 8 is the identity). With such keys plain RFC 8032 verification
accepts forged signatures: for the identity point, the signature
(R = identity, S = 0) verifies every message. Test vector `08-weak-key`
shows this.

Ed25519 signatures are deterministic: signing the same event with the same
key always yields the same signature, which the test vectors rely on.

Signature verification does not replace field validation and vice versa;
section 10 requires both.

## 8. Identity and publisher binding

A node has one Ed25519 key pair. Its libp2p peer ID is derived from the
public key, and the same key signs the node's events.

**[ID-1]** `publisher.peer_id` MUST be the libp2p peer ID of the signing
key in the legacy base58btc text form: the base58btc encoding (Bitcoin
alphabet, no multibase prefix) of the bytes `0x00 0x24 0x08 0x01 0x12 0x20`
followed by the 32-byte public key. That is an identity multihash of the
protobuf `PublicKey{Type: Ed25519, Data: key}`; such peer IDs start with
`12D3KooW`. Receivers MUST reject any other encoding, including the CIDv1
form of the same peer ID and peer IDs of other key types.

**[ID-2]** Receivers MUST take the verification key from
`publisher.peer_id` and from nowhere else. An event signed by any key other
than the one embedded in `publisher.peer_id` fails verification.

**[ID-3]** A GossipSub message MUST NOT carry author information: no
`from`, `seqno`, `signature` or `key` field (the GossipSub `StrictNoSign`
message signature policy). Receivers MUST drop a message that carries any
of them. The event signature is the only authentication of the publisher:
any peer can forward, and re-send, a publisher's signed event, but no peer
can alter or forge one.

`publisher.asn` is self-declared and unverified in obie/0.1. Receivers
SHOULD NOT base trust decisions on it without verifying it by other means.

The key stays the same across restarts of a node, which keeps its
identity. obie/0.1 has no key rotation; a node with a new key is a new
publisher (key rotation statements are planned).

## 9. Transport

Nodes form a libp2p mesh and exchange events with GossipSub (v1.1).

**[TRN-1]** Events MUST be published on the GossipSub topic
`obie/0.1/verdicts`. Verdicts and revocations share this topic.

**[TRN-2]** Each GossipSub message MUST carry exactly one serialized event
as its data, without framing, batching, compression or any other envelope,
and within the size limit of [ENC-1].

**[TRN-3]** The GossipSub message ID of a message MUST be the `id` member
of its event, so that every node deduplicates, announces (`IHAVE`) and
requests (`IWANT`) an event under the same ID. A message whose data is not
a JSON object with a string `id` of at most 4096 bytes is invalid anyway;
its message ID is implementation-defined.

Recommendations for the libp2p layer:

- Nodes SHOULD register a topic validator that runs the checks of section
  10 and reports failures as `Reject`, so that GossipSub peer scoring
  penalises peers that forward invalid messages. Failures that depend on
  the local clock (an expired event, an `issued_at` too far in the future)
  SHOULD be reported as `Ignore` instead: an honest peer with a slightly
  different clock may have forwarded the message in good faith. An event
  that expired more than 300 seconds (`MaxClockSkew`) before it was
  received MAY be reported as `Reject`: no clock within the tolerance of
  [ENV-4] would have forwarded it.
- Because the message ID is chosen by the publisher, a peer that learns an
  event's `id` can send a forged message with that ID ahead of the genuine
  one; the receiver rejects the forgery and, as GossipSub remembers the ID
  of every message it has seen, drops the genuine message while it
  remembers the ID, from whichever peer it arrives. Nodes SHOULD therefore
  enable GossipSub peer scoring so that such peers are quickly pruned.
- Nodes SHOULD listen on TCP with the Noise security protocol and the yamux
  multiplexer, and MAY additionally offer QUIC. Peer discovery in obie/0.1
  uses statically configured bootstrap peers; DHT discovery is planned.

## 10. Validation and drop rules

A node processes every message it receives on the topic with the following
checks. The reference implementation performs them in this order in
`obieproto.Receive`: cheap checks first, except that the checks against the
local clock come after the signature, so that a clock failure (which
section 9 reports as `Ignore`) is only ever reported for an authentic
event. The order is otherwise free.

1. Size: at most 4096 bytes ([ENC-1]).
2. Version and type: `spec` is `obie/0.1` and `type` is known ([VER-1],
   [VER-2]).
3. Structure: one JSON object with only defined, non-duplicate, non-null
   members of the right JSON type ([ENC-1] to [ENC-4], [ENV-1]).
4. Field rules: Table 1, canonical and public indicator ([ENV-2], [ENV-3],
   [IND-1] to [IND-5]).
5. Signature: [SIG-1] to [SIG-4], [ID-1], [ID-2].
6. Clock skew: `issued_at` is not too far in the future ([ENV-4]).
7. Expiry: the event has not expired (section 5.3).

**[RCV-1]** A node MUST run all of these checks on a message before it acts
on the event or forwards the message, and MUST drop a message that fails
any of them.

**[RCV-2]** A node MUST drop an event that has already expired when it is
received.

**[RCV-3]** Receivers MUST NOT modify a received event to make it pass
validation: non-canonical values are rejected, not repaired.

Receivers SHOULD ignore an event whose `id` they have already accepted
(a duplicate) without forwarding it again; GossipSub's own duplicate
suppression normally prevents this.

Dropped messages SHOULD be counted by reason (the error classes of Appendix
C) so that operators can see signature failures and malformed traffic.

## 11. Rate limits

obie/0.1 does not carry rate limits in events. The following expectations
let publishers stay within what receivers will tolerate:

- A publisher SHOULD NOT publish more than 10 events per second averaged
  over one minute, nor more than 50 events in a burst.
- A publisher SHOULD NOT republish a verdict that is still in effect
  unchanged; it SHOULD issue a new verdict only when the action, the
  confidence or the lifetime changes, or when less than half of the
  lifetime remains.
- A publisher SHOULD aggregate many hostile addresses of one network into a
  CIDR verdict rather than publishing one verdict per address.
- Receivers SHOULD limit the events they accept per publisher (a token
  bucket with the rates above is RECOMMENDED) and MAY also limit the events
  they accept per forwarding peer; they MAY drop events beyond the limits.
  Such drops SHOULD be reported to GossipSub as `Ignore`, not `Reject`,
  because the forwarding peer is not necessarily at fault. A per-peer
  limit SHOULD be well above the per-publisher limit: a relay forwards the
  events of every publisher, and a flooding publisher's admitted events
  alone reach the per-publisher limit. The reference implementation admits
  an event only if both limits do, and counts it against both; by default
  the per-publisher bucket holds 50 events refilled at 10 per second, the
  per-peer bucket 250 events refilled at 50 per second.

## 12. Versioning and forward compatibility

The `spec` member identifies the protocol version. Any change to the
members, their constraints or the signed form yields a new version with a
new `spec` value and a new topic `obie/<version>/verdicts`; there are no
extension points inside obie/0.1.

**[VER-1]** A receiver MUST reject an event whose `spec` is not exactly
`obie/0.1`, and MUST classify it as an unsupported version even if it also
has members or types obie/0.1 does not know.

**[VER-2]** A receiver MUST reject an event whose `type` is not one of the
obie/0.1 types (for example `indicator.observed` or `indicator.appeal`,
planned for later versions), classifying it as an unsupported type even if
it also has unknown members.

Because unknown members are rejected ([ENC-2]), an obie/0.1 node never
silently ignores information a newer publisher considered important.
Nodes that support several versions subscribe to each version's topic and
publish every event on the topic of its version.

## 13. Privacy

OBIE shares indicators, not data about users or the publisher's systems
(manifesto principle 5, "Privacy by Default").

**[PRV-1]** An event MUST NOT carry raw logs, payloads, user names,
credentials or other free text. Every string member has a restricted format
(Table 1); the only descriptive ones are short tokens (`protocol`,
`evidence.reason`, `reason`, MITRE IDs), and receivers reject any member not
in Table 1 ([ENC-2]).

**[PRV-2]** A node MUST NOT publish an event about an internal address:
indicators in private, loopback, link-local, shared, documentation or other
special-purpose ranges (section 6.3) are invalid, so receivers reject them
and a publisher's own validation refuses them.

Further guidance:

- `evidence.log_hash` lets the publisher later prove which log excerpt a
  verdict was based on without disclosing it. A hash of short or
  predictable content can be reversed by guessing; publishers SHOULD hash
  an excerpt that contains unpredictable data (complete log lines with
  timestamps and source ports) or omit `log_hash`. The excerpt SHOULD stay
  on the publisher's node.
- `publisher.asn` discloses the publisher's network and is optional.
- `publisher.peer_id` is a stable pseudonym: all events of a node are
  linkable to each other. libp2p also discloses a node's transport
  addresses to the peers it connects to.

## 14. Security considerations

- **Forgery and tampering.** Every event is signed, and the signature covers
  every member except itself; any change to a signed event is detected
  (test vector `06-tampered`). Verifiers reject weak public keys ([SIG-4]),
  without which forgeries without a private key would be possible.
- **Canonicalisation ambiguity.** Strict I-JSON input ([SIG-2]), rejection
  of duplicate and unknown members ([ENC-2]) and of non-canonical values
  ([IND-3]) ensure that one event has exactly one signed form, so two
  implementations cannot disagree about what a signature covers.
- **Replay.** Anyone can re-send a publisher's original GossipSub message
  while its event is unexpired. A receiver that still knows the event treats
  it as a duplicate (same `id`); one that does not must not let it undo a
  later decision of the publisher. An expired event is dropped ([RCV-2]); a
  revoked verdict stays withdrawn because a revocation that is not dated
  before its verdict outlives it (sections 5.2, 5.3); and a superseded
  verdict stays superseded as long as the receiver remembers the latest
  verdict as section 5.1 recommends. Publishers end verdicts early by
  revocation for this reason.
- **Clock skew.** Events dated more than five minutes ahead are rejected
  ([ENV-4]), so a publisher cannot make a verdict supersede later ones by
  dating it in the future. Nodes SHOULD keep their clocks synchronised
  (NTP).
- **Poisoning and Sybil attacks.** Anyone can create a key and publish
  well-formed events. The protocol authenticates publishers but does not
  establish trust in them: receivers SHOULD act only on verdicts of
  publishers they trust, weighted by local policy, and SHOULD require
  corroboration by several independent publishers before enforcing.
  Local allow-lists SHOULD always take precedence over received verdicts.
- **Revocation abuse.** Only a verdict's own publisher can revoke it
  ([SEM-3]); a revocation by anyone else has no effect.
- **Over-broad indicators.** CIDR ranges are limited to /16 (IPv4) and /32
  (IPv6), and indicators in special-purpose ranges are rejected ([IND-5]),
  so a hostile or mistaken verdict cannot target internal networks or
  large parts of the address space.
- **Denial of service.** The size limit ([ENC-1]), checks that run before
  the comparatively expensive signature verification (about 64 µs per
  event on one core), GossipSub peer scoring and the rate limits of section
  11 bound the cost of hostile traffic.
- **Key compromise.** A stolen key lets an attacker publish and revoke in
  the name of its publisher. obie/0.1 has no key rotation or revocation of
  keys; operators remove trust in the compromised publisher locally and the
  node starts over with a new key. Nodes SHOULD store the private key so
  that no other user of the system can read it.

## 15. JSON Schema

[`obie-0.1.schema.json`](obie-0.1.schema.json) is a JSON Schema (draft
2020-12) of the event structure: members, types, patterns, ranges and the
members allowed per event type. Like the reference decoder it accepts an
empty `publisher.signature` (an unsigned draft, section 4.2). It is
necessary but not sufficient: an event the schema rejects is invalid, but
an event it accepts can still violate the rules below.

### 15.1 Rules beyond the schema

JSON Schema cannot express these rules; implementations check them in code:

1. The serialized size limit of 4096 bytes ([ENC-1]).
2. Duplicate member names ([ENC-2]); JSON parsers typically keep only one.
3. Integers written with a fraction or exponent, such as `47.0` ([ENC-4]);
   JSON Schema treats them as integers.
4. Negative zero as confidence ([ENV-2]).
5. Calendar validity of `issued_at`, e.g. `2026-02-30T00:00:00Z`, and the
   excluded value `0001-01-01T00:00:00Z` ([ENV-2]).
6. The clock-skew limit of `issued_at` ([ENV-4]).
7. `revokes` differing from `id` ([ENV-3]).
8. The complete canonical form of IPv6 addresses (RFC 5952: shortest form,
   a single `::`) and zero host bits of CIDR ranges ([IND-2]); the schema
   checks the character set, lower case, the absence of leading zeros and
   that IPv6 addresses lie in `2000::/3`.
9. `scope` matching the prefix length of a CIDR `value` ([IND-2]).
10. Public address space ([IND-5]) beyond `2000::/3`.
11. The signature itself ([SIG-1] to [SIG-4]) and the peer ID encoding
    beyond its alphabet ([ID-1]).

The reference implementation's tests validate every test vector and a list
of valid and invalid samples against the schema and require the decoder to
agree on each, and check that each rule above is enforced by the decoder
although the schema accepts the sample (`TestSchemaAgreesWithDecode`,
`TestSchemaAcceptsVectors`, `TestSchemaLimits`).

## 16. Test vectors

[`test-vectors/`](test-vectors/README.md) contains signing test vectors:
events with the key seed, the canonical bytes in hex, the signature and the
expected results (`protocol_valid`: the event passes the rules of sections
4 and 6 at its `issued_at`; `valid`: the signature verifies). An
implementation SHOULD reproduce the canonical bytes and signatures of all
valid vectors and reject the negative ones. The seeds are the RFC 8032
test keys; they are public and for tests only.

## 17. References

- [RFC 2119]: Key words for use in RFCs to Indicate Requirement Levels.
- [RFC 8174]: Ambiguity of Uppercase vs Lowercase in RFC 2119 Key Words.
- [RFC 8259]: The JavaScript Object Notation (JSON) Data Interchange Format.
- [RFC 7493]: The I-JSON Message Format.
- [RFC 8785]: JSON Canonicalization Scheme (JCS).
- [RFC 8032]: Edwards-Curve Digital Signature Algorithm (EdDSA).
- [RFC 4648]: The Base16, Base32, and Base64 Data Encodings.
- [RFC 5952]: A Recommendation for IPv6 Address Text Representation.
- [RFC 9562]: Universally Unique IDentifiers (UUIDs).
- [RFC 3339]: Date and Time on the Internet: Timestamps.
- libp2p peer IDs: <https://github.com/libp2p/specs/blob/master/peer-ids/peer-ids.md>
- GossipSub v1.1: <https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md>
- IANA special-purpose address registries:
  <https://www.iana.org/assignments/iana-ipv4-special-registry/>,
  <https://www.iana.org/assignments/iana-ipv6-special-registry/>

[RFC 2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC 8174]: https://www.rfc-editor.org/rfc/rfc8174
[RFC 8259]: https://www.rfc-editor.org/rfc/rfc8259
[RFC 7493]: https://www.rfc-editor.org/rfc/rfc7493
[RFC 8785]: https://www.rfc-editor.org/rfc/rfc8785
[RFC 8032]: https://www.rfc-editor.org/rfc/rfc8032
[RFC 4648]: https://www.rfc-editor.org/rfc/rfc4648
[RFC 5952]: https://www.rfc-editor.org/rfc/rfc5952
[RFC 9562]: https://www.rfc-editor.org/rfc/rfc9562
[RFC 3339]: https://www.rfc-editor.org/rfc/rfc3339

## Appendix A: Requirements and tests

Every tagged requirement and the tests of the reference implementation
(`pkg/obieproto`) that check it. `TestSpecRequirementsAreTested` fails if a
normative statement has no tag, a tag is missing here, or a test named here
does not exist. For [SEM-1] to [SEM-4] the tests check the rules as the
reference implementation states them (`Event.Supersedes`,
`Event.Withdraws`, `Event.Expired`); the node's event store applies them.
For [TRN-1] the test pins the topic constant that the mesh layer uses;
[ID-3] and [TRN-3] are tested in the node's gossip layer
(`internal/gossip`).

| Requirement | Tests |
|-------------|-------|
| ENC-1  | `TestDecodeInvalid`, `TestValidate`, `TestSchemaLimits` |
| ENC-2  | `TestDecodeInvalid`, `TestSchemaAgreesWithDecode`, `TestSchemaLimits` |
| ENC-3  | `TestDecodeInvalid`, `TestValidate`, `TestSchemaAgreesWithDecode` |
| ENC-4  | `TestDecodeInvalid`, `TestValidate`, `TestSchemaLimits`, `TestEncodeRejects` |
| ENV-1  | `TestValidate`, `TestDecodeInvalid`, `TestSchemaAgreesWithDecode` |
| ENV-2  | `TestValidate`, `TestDecodeInvalid`, `TestTimestampUnmarshal`, `TestSchemaAgreesWithDecode`, `TestSchemaLimits` |
| ENV-3  | `TestValidate`, `TestSchemaLimits` |
| ENV-4  | `TestValidate`, `TestReceive` |
| SEM-1  | `TestSupersedes` |
| SEM-2  | `TestSupersedes` |
| SEM-3  | `TestWithdraws` |
| SEM-4  | `TestExpired`, `TestExpiresAt` |
| IND-1  | `TestIndicatorNormalize`, `TestIndicatorValidate`, `TestDecodeInvalid`, `TestSchemaAgreesWithDecode` |
| IND-2  | `TestIndicatorValidate`, `TestIndicatorNormalize`, `TestSchemaAgreesWithDecode`, `TestSchemaLimits` |
| IND-3  | `TestIndicatorValidate`, `TestValidateDoesNotModify`, `TestDecodeInvalid` |
| IND-4  | `TestIndicatorNormalize`, `TestKeyIsStableAcrossNormalization` |
| IND-5  | `TestIndicatorValidate`, `TestValidate`, `TestDecodeReadmeExampleRejectsDocumentationRange` |
| SIG-1  | `TestCanonicalBytes`, `TestVectorFiles`, `TestVerifyDetectsTampering` |
| SIG-2  | `TestTransformRejects`, `TestEncodeRejects`, `TestCanonicalBytes` |
| SIG-3  | `TestSignVerifyRoundTrip`, `TestVerifyRejects`, `TestValidate`, `TestVectorFiles` |
| SIG-4  | `TestWeakPublicKeysAreRejected`, `TestVerifyRejectsWeakKeyForgery`, `TestVectorFiles` |
| ID-1   | `TestPeerIDRoundTrip`, `TestPublicKeyFromPeerIDRejects`, `TestVerifyRejects` |
| ID-2   | `TestVerifyRejects`, `TestSignRejects`, `TestVectorFiles` |
| ID-3   | `TestReceive`, `TestGossipDropsAuthoredMessages` |
| TRN-1  | `TestTopic` |
| TRN-2  | `TestDecodeInvalid`, `TestReceive` |
| TRN-3  | `TestMessageID` |
| RCV-1  | `TestReceive`, `TestReceiveVectors` |
| RCV-2  | `TestReceive` |
| RCV-3  | `TestValidateDoesNotModify`, `TestIndicatorValidate`, `TestDecodeRoundTripIsByteIdentical` |
| VER-1  | `TestValidate`, `TestDecodeInvalid` |
| VER-2  | `TestValidate`, `TestDecodeInvalid` |
| PRV-1  | `TestDecodeInvalid`, `TestSchemaAgreesWithDecode`, `TestValidate` |
| PRV-2  | `TestIndicatorValidate`, `TestValidate` |

## Appendix B: Examples

A verdict (test vector `01-verdict-ipv4`):

```json
{
  "id": "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a01",
  "spec": "obie/0.1",
  "type": "indicator.verdict",
  "issued_at": "2026-09-27T12:00:00Z",
  "indicator": { "kind": "ipv4", "value": "85.10.20.30", "scope": "/32" },
  "protocol": "ssh",
  "evidence": {
    "events": 47,
    "reason": "password_bruteforce",
    "log_hash": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
    "honeypot": true
  },
  "verdict": { "suggested_action": "ban", "confidence": 0.92, "ttl_seconds": 604800 },
  "mitre": ["T1110", "T1110.001"],
  "publisher": {
    "peer_id": "12D3KooWQK1wnefoLrcVHbbnf5tLzbopUd3K3bFAoJpA7YJgL5pV",
    "asn": 24940,
    "signature": "ed25519:rGOk8AdaUEqyBsgZ_fHBPpiX84vnj_xY12-5BS-Mk9WoNnBeawvI98RaeS3jB5YIvx7H_hWoXObQIZPmdj9jDw"
  }
}
```

Its revocation by the same publisher (test vector `04-revoke`):

```json
{
  "id": "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a04",
  "spec": "obie/0.1",
  "type": "indicator.revoke",
  "issued_at": "2026-09-27T13:00:00Z",
  "indicator": { "kind": "ipv4", "value": "85.10.20.30", "scope": "/32" },
  "revokes": "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a01",
  "reason": "false_positive",
  "publisher": {
    "peer_id": "12D3KooWQK1wnefoLrcVHbbnf5tLzbopUd3K3bFAoJpA7YJgL5pV",
    "signature": "ed25519:kb0r3YZsVhuZ5xHEn74aPMUgWjGqDbSaFaOpBzYJlSSO_9PqAA-HkjKYEIw94QX_V-pB3Ubfu9MbxoJKhLkBCQ"
  }
}
```

## Appendix C: Error classes (informative)

The reference implementation classifies every rejection into one class, the
sentinel errors of `pkg/obieproto`. Other implementations need not use these
names, but distinguishing the classes helps operators and metrics.

| Class                   | Meaning                                                                 |
|-------------------------|-------------------------------------------------------------------------|
| `malformed`             | not a single JSON object of the expected shape, `null`, wrong JSON type |
| `too_large`             | larger than 4096 bytes                                                  |
| `unknown_field`         | unknown, differently cased or duplicate member                          |
| `unsupported_spec`      | `spec` is not `obie/0.1`                                                |
| `unsupported_type`      | `type` is not an obie/0.1 type                                          |
| `unsupported_indicator` | `indicator.kind` is not an obie/0.1 kind                                |
| `non_public_indicator`  | indicator in a special-purpose range                                    |
| `invalid_field`         | any other violation of Table 1 or section 6                             |
| `publisher_mismatch`    | peer ID without a usable Ed25519 key                                    |
| `clock_skew`            | `issued_at` more than 300 seconds ahead of the receiver's clock         |
| `expired`               | the event expired before it was received                                |
| `invalid_signature`     | signature missing, malformed or not matching                            |
