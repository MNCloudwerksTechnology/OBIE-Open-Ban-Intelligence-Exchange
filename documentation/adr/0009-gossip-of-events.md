# ADR 0009: Gossip of events

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1656](https://openproject.niew.dev/work_packages/1656)

## Context

ADR 0007 connects a node to its bootstrap peers; ADR 0008 stores events.
Signed verdicts and revocations must now spread across the mesh within
seconds, and nothing invalid or abusive may be relayed: a forged, altered,
oversized or expired event is dropped at the first hop, and a flooding peer
is throttled without starving the others.

## Decision

- **Dependency:** `github.com/libp2p/go-libp2p-pubsub` v0.17 (GossipSub
  v1.1); `golang.org/x/time/rate` for token buckets (already in the module
  graph through go-libp2p).
- **Package:** `internal/gossip` joins the topic `obie/0.1/verdicts` on a
  given host and store. It is not a lifecycle subsystem of its own: the
  `mesh` subsystem creates it right after its host in `Start` and closes it
  before the host in `Stop`. `Mesh.Publish` exposes it; the mesh now
  requires the store (`mesh.Options.Store`), which the daemon starts first.
- **No pubsub signatures:** messages are published with GossipSub's
  `StrictNoSign` policy and without author (`from`, `seqno`, `key`); messages
  that carry them are rejected. The event signature is the only
  authentication of the publisher. This replaces the spec's former
  recommendation of `StrictSign` and its author check ([ID-3] now requires
  the absence of author information): the author check only restated what
  the event signature proves, cost a second signature per message and tied
  events to the host that first published them.
- **Message ID = event ID** ([TRN-3]), so every node deduplicates and
  requests (`IWANT`) an event under the same ID. Data that is not a JSON
  object with an `id` of at most 4 KiB gets its SHA-256 as ID; it is invalid
  anyway. Consequence: GossipSub remembers an ID as seen before it
  validates the message, whatever the result. A mesh member that receives
  an event can send forged messages with its ID to its other neighbours
  ahead of the genuine one: they reject the forgery and penalise the sender,
  but then drop the genuine event from every peer while they remember the
  ID (2 minutes), and v0.1 has no later sync, so they miss the event. Each
  peer identity can do this about five times before it is graylisted.
  Message IDs bound to the content (event ID plus a hash of the signed
  form) would close this at the cost of a protocol change; they are
  candidates for obie/0.2.
- **Validator order** (topic validator, runs before a message is stored or
  relayed): size ≤ 4 KiB → decode → field rules → signature → clock
  (`issued_at` at most `MaxClockSkew` ahead, not expired) → duplicate
  (`store.Seen`) → rate limits. The first five are `obieproto.Receive`,
  which now checks the clock after the signature, so a forged event is
  always invalid whatever its timestamps. Outcomes and GossipSub results:

  | Outcome             | Result | Cause |
  |---------------------|--------|-------|
  | `too_large`         | Reject | over 4096 bytes |
  | `invalid_schema`    | Reject | any format or field rule |
  | `invalid_signature` | Reject | signature missing, malformed or wrong; peer ID without a usable key |
  | `expired`           | Ignore | expired less than `MaxClockSkew` ago, or dated too far ahead: an honest peer with a skewed clock may forward it |
  | `expired`           | Reject | expired longer ago than any tolerated clock skew |
  | `duplicate`         | Ignore | already stored or seen by the store |
  | `rate_limited`      | Ignore | a token bucket is empty |
  | `accepted`          | Accept | stored and relayed |

  Accepted events are written to the store by the validator; events the
  store ignores (a verdict older than the publisher's current one, a
  revocation it does not apply) are still relayed. A store error is logged
  and does not stop the relay.
- **Rate limits:** two token buckets per event, one keyed by the publisher
  (`mesh.rate_limit.publisher`, default 10 events/s, burst 50) and one by
  the forwarding peer (`mesh.rate_limit.peer`, default 50 events/s, burst
  250). An event takes a token from both or from neither, so a relay is not
  charged for a flooding publisher's excess. The peer limit defaults higher
  than the publisher limit because a peer relays the events of every
  publisher: with equal limits (the work package's single "10/s, burst 50")
  the ~50 events a relay legitimately admits from one flooder use up the next
  hop's whole bucket for that relay, and honest events arriving through it
  were dropped there (reproduced by `TestGossipRateLimitsFlooder`). Buckets
  that have refilled are forgotten, so memory follows the active keys.
  A rate-limited event is remembered by GossipSub like any other and is
  not accepted from another peer while that lasts.
- **Validation queue:** received messages wait in GossipSub's validation
  queue before any bucket sees them, and a full queue drops the messages of
  every peer alike. The queue holds four full peer bursts (1000 messages
  with the defaults) instead of GossipSub's 32: with 32, a flooder's burst
  on a busy node filled it, and honest events arriving meanwhile were lost
  (`TestGossipRateLimitsFlooder` failed on two-CPU CI runners).
- **Peer scoring:** enabled with conservative parameters: invalid messages
  (P4, weight −10, squared, decaying within an hour) drive the score;
  thresholds gossip −50, publish −100, graylist −200, i.e. a peer is
  ignored after about five invalid messages. Mesh-delivery penalties (P3,
  P3b) are off because verdicts are sparse and a quiet peer is not
  misbehaving; rewards for time in the mesh and first deliveries are small
  and capped; IP colocation is penalised above ten peers per address.
- **Local publication:** `Publish(event)` accepts only events whose
  publisher is the node itself, checks them with `obieproto.Receive`,
  stores them, then publishes. The validator accepts the node's own
  messages without counting them. With no peers the event is stored and the
  publication is a no-op on the wire. Own events are flood-published
  (GossipSub v1.1): sent to every topic peer above the publish threshold,
  not only to the mesh peers, so a single peer that drops one does not
  stop it.
- **Metrics hook:** `gossip.Metrics` (`Observe(Outcome)`) receives the
  outcome of every message from a peer; `gossip.Outcomes` lists the
  outcomes. The metrics work package (#1663) wires it to Prometheus; until
  then the daemon passes none.
  *Extended by [ADR 0032](0032-gossip-instrumentation-and-attribution.md):
  a GossipSub `RawTracer` counts what happens before and around the
  validator (`obie_gossip_*`), the peer scores are read every 10 seconds
  and exported, and `mesh.trace_path` writes a line per copy of an event.*

## Consequences

- The spec (sections 8–11, appendices A and C) changes accordingly; the
  new `obieproto.ErrClockSkew` separates a future `issued_at` from other
  field errors, and `obieproto.Receive` no longer takes the message author.
- GossipSub relays only to peers in the topic mesh, which forms in the
  heartbeat (1 s) after peers connect; an event published before that
  reaches the direct peers only. v0.1 has no anti-entropy sync, so such an
  event is not delivered further later.
- Operators whose neighbours relay for a large mesh may need to raise
  `mesh.rate_limit.peer`.
