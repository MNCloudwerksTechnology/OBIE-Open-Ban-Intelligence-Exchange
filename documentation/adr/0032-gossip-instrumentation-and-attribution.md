# ADR 0032: Gossip instrumentation and the attribution of every block

- **Status:** Accepted
- **Date:** 2026-09-30
- **Work package:** [#1764](https://openproject.niew.dev/work_packages/1764)
  (epic [#1761](https://openproject.niew.dev/work_packages/1761)); amends
  [ADR 0015](0015-metrics-and-audit-log.md); extends
  [ADR 0009](0009-gossip-of-events.md),
  [ADR 0021](0021-console-peers.md) and
  [ADR 0023](0023-console-verdicts.md)

## Context

The routing and trust work packages of epic #1761 state numeric targets:
delivery ratio, propagation latency, duplicate factor, mesh size and
churn, the share of peers below the score thresholds, and who caused a
block. A node measures none of them yet:

- **No view into GossipSub.** No `RawTracer`, `EventTracer` or peer score
  inspector is configured. GossipSub's seen-cache drops a copy of a known
  message before OBIE's validator runs, so
  `obie_events_received_total{outcome="duplicate"}` counts only the copies
  that the store recognises.
- **Whole seconds.** `obie_propagation_delay_seconds` subtracts
  `issued_at`, which obie/0.1 fixes at whole seconds (Table 1 of the spec:
  `YYYY-MM-DDTHH:MM:SSZ`). The delay of a verdict relayed within a second
  cannot be measured.
- **Counts, not identities.** The audit log's `obie.publishers` counts the
  contributing publishers. It does not name them, so a block cannot be
  traced back to the verdicts that caused it.
- **A day of history.** Ended verdicts are kept for 24 hours after their
  expiry (ADR 0023). A ban cannot be attributed a day later.
- **No hop counts.** The simulation harness of #1765 must measure the
  hops and paths an event takes, which no node records.

## Decision

### Tracer metrics

- `internal/gossip` passes a `pubsub.RawTracer` (`WithRawTracer`) that
  feeds Prometheus. Every label value comes from a closed set, and no
  metric carries a peer ID or an address (ADR 0015):

  | Metric | Type | What it counts |
  |--------|------|----------------|
  | `obie_gossip_deliveries_total` | counter | messages from peers that GossipSub accepted and delivered: the first valid copy of each event |
  | `obie_gossip_duplicates_total` | counter | copies of a message already seen, dropped by GossipSub before validation |
  | `obie_gossip_rejects_total{reason}` | counter | messages GossipSub dropped, by reason (below) |
  | `obie_gossip_ignores_total` | counter | messages OBIE's validator ignored (`ValidationIgnore`) |
  | `obie_gossip_grafts_total` | counter | peers added to this node's mesh of the topic |
  | `obie_gossip_prunes_total` | counter | peers removed from the mesh by a PRUNE |
  | `obie_gossip_ihave_total{direction}` | counter | message IDs announced in IHAVE, `sent` or `received` |
  | `obie_gossip_iwant_total{direction}` | counter | message IDs requested in IWANT, `sent` or `received` |
  | `obie_gossip_mesh_peers` | gauge | peers in this node's mesh of the topic now |

  The reasons are `validation_failed` (OBIE's validator rejected it;
  `obie_events_received_total` says why), `queue_full` and `throttled`
  (validation capacity), `signature` and `author` (a GossipSub signature
  or an author, which the StrictNoSign policy of ADR 0009 forbids; kept
  apart from the event signature, whose failures are `validation_failed`),
  `blacklisted`, `self_origin` and `other`. Ignored messages are not
  rejects: GossipSub reports both through `RejectMessage`, and the tracer
  separates them.
- The node's own publications are not deliveries: they are in
  `obie_events_published_total`. A disconnecting peer leaves the mesh
  without a PRUNE, so the tracer keeps the mesh's members itself and
  removes a peer when its stream closes; `obie_gossip_mesh_peers` is their
  number. `obie_events_received_total` keeps its meaning: the copies that
  reached the validator. All copies received are
  `deliveries + duplicates + rejects + ignores`, except those of a
  graylisted peer, whose RPCs GossipSub drops whole before looking at a
  message.
- **Semantics for several nodes in one process** (tests, the in-process
  simulation): every node adds to the same counters, and the gauges add
  each node's share and withdraw it when the node stops, like the
  counters' differences in ADR 0015.

### Peer scores

- The GossipSub peer score (ADR 0009) is read every 10 seconds through
  `WithPeerScoreInspect`, with its components: time in mesh, first and
  invalid message deliveries, IP colocation factor, behaviour penalty,
  application score. It covers the connected peers and those that
  disconnected within `RetainScore` (1 hour), whose score GossipSub keeps.
- **Aggregated in `/metrics`:** `obie_gossip_peer_score` is a histogram
  with one observation per scored peer and inspection (buckets at the
  thresholds: −200, −100, −50, −10, −1, 0, 1, 5, 10);
  `obie_gossip_peers_below_threshold{threshold}` counts the peers whose
  score is below the `gossip` (−50), `publish` (−100) and `graylist`
  (−200) thresholds at the last inspection; `obie_gossip_scored_peers`
  counts the peers scored. The share below a threshold is the quotient.
- **Per peer:** `GET /v1/peers` gains `gossip_score` (the score, the
  thresholds it is below, its components and when it was read), and the
  console's peers view gains a *Gossip score* column, sorted lowest first,
  and the peer page the components (extends ADR 0021). The score of a
  peer without a reading yet (the first comes up to 10 seconds after it
  connects) is absent, not zero. `obiectl peers`
  prints it with `--json`; its table stays as it is, because the checked
  walkthroughs compare it.

### Sub-second propagation delay

- obie/0.1 fixes `issued_at` at whole seconds, and the signed form of an
  event includes it. Sub-second precision in `issued_at` is a protocol
  change; it is a candidate for obie/0.2. **This is the limitation this
  ADR records.**
- The event `id` is a UUIDv7, and "Publishers SHOULD create `id` from the
  event's creation time" (section 4.2): its first 48 bits carry the Unix
  time in milliseconds, and the `id` is signed too. The node takes an
  event's creation time from its `id` when that time lies within the
  second `issued_at` names. Otherwise, e.g. a publisher that builds IDs
  some other way, it takes `issued_at`. `obie_propagation_delay_seconds`
  is receipt minus that time, clamped at 0. The receipt time is when the
  validator checks the message, after the validation queue. The buckets
  now start at 5 ms (0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5,
  10, 30, 60, 120, 300, 600, 1800, 3600 seconds).
- A publisher can date its `id` within that second as it likes, as it
  can date `issued_at` today. The delay remains a measurement, not a
  check; nothing depends on it.

### Contributing publishers in the audit log

- Every `block-added`, `block-updated` and `block-removed` record gains
  `obie.contributors`: a list with one object per verdict that counts in
  the block. Each object holds `peer_id`, `weight`, `confidence` and
  `verdict_id`. A record of an added or updated block lists the
  contributors of the new decision. A record of a removed block lists
  those of the block that ended, because the verdicts that remain do not
  explain what was blocked. A block with none, e.g. a force-block on an
  address without verdicts, has an empty list. `obie.publishers` stays a
  count, because SIEM mappings of a numeric field break when its type
  changes.
- The decision engine's block change stream (`decision.Change`) carries
  the contributors. To name them when a block ends, the engine keeps, for
  every block, each contributing verdict's publisher (interned), verdict
  ID (16 bytes) and the weight and confidence it counted with: 40 bytes
  per contributor and 24 per block, about 52 MB for the 495,299 blocks of
  the resource measurement at two contributors each (+2 % of its
  2.5 GiB). A change of contributing verdict IDs is now also a
  `block-updated`, even if score and expiry stay.
- **Audit log size.** Each contributor adds about 150 bytes to a block
  record: a block of two publishers grows from about 0.45 KiB to about
  0.75 KiB of audit log (218 MiB to about 370 MiB for the 495,299 blocks
  of the resource measurement), which logrotate sizing should allow for.

### Ended verdicts kept for 30 days

- `store.ended_retention` (default `30d`, at least `1h`, at most `365d`,
  applied on restart) replaces the constant 24 hours of ADR 0023: ended
  verdicts, and the record of a verdict past its expiry, are kept that
  long. Entries written before keep their 24 hours.
- **Storage cost.** An ended verdict takes about 1 KB on disk (a
  650–800-byte value and a 100-byte key, measured). The caps of ADR 0023
  do not change: other publishers' ended verdicts are bounded by
  `store.max_indicators` / 10 per state, at most about 200 MB at the
  defaults, as before. A longer retention reaches that cap sooner: above
  about 3,300 ended verdicts a day per state instead of 100,000. As
  before, a full cap keeps the verdicts it holds and refuses newer ones
  until older ones are forgotten, so the verdicts view then shows the
  ones that ended first, not the latest 30 days; the audit log's
  `obie.contributors` are not affected. Dropping the oldest instead, or
  sizing the cap from the retention, is left to a later change. The
  node's own ended verdicts are never capped: a node that reports 1,000
  verdicts a day keeps about 30,000 of them, about 30 MB, instead of 1 MB.

### Per-event trace

- `mesh.trace_path` (default empty: off; applied on restart) names a file
  to which the node appends one JSON line per copy of an event it
  receives and per event it publishes: `node` (its own peer ID), `event`
  (the message ID, i.e. the event ID), `from` (the forwarding peer; the
  node itself for its own), `at` (RFC 3339 with nanoseconds) and
  `outcome` (`published`, a validator outcome of ADR 0009, `duplicate`
  for a copy the seen-cache dropped, or a reject reason from above for a
  message GossipSub dropped before validation). The file is opened like
  the audit log (append, mode 0640) and is never rotated or read by the
  node. It grows by about 230 bytes per copy, so it is meant for
  simulations and short diagnostics. It holds peer IDs, not addresses.
- **Writing it.** GossipSub's event loop traces the copies it drops, so
  tracing must never wait for the disk: lines are collected in memory,
  and one goroutine writes them, whole lines only, every second, once
  64 KiB wait, and when the mesh stops. Beyond 16 MiB waiting, lines are
  dropped with a warning. A failed write loses its lines and is logged
  once; the next write starts on a line of its own, and so does the
  first after a restart if a crash cut the last line short, which the
  reader skips. A `published` line is written when the node publishes,
  also for an event it holds while no peer is on the topic (ADR 0026):
  the delays the join computes then include the wait.
- `internal/eventtrace` writes and reads the lines and joins the files of
  several nodes: for every event, its origin, every node's first
  acceptance with the forwarding peer, the hop count and path back to the
  origin, the delay, and the copies each node received. The simulation
  harness of #1765 uses it.

## Alternatives considered

- **`EventTracer` with GossipSub's JSON or protobuf tracers:** they write
  every RPC, including control messages, and their records name the
  message IDs and peers as bytes. A harness would parse a format that
  follows go-libp2p-pubsub's releases. Rejected for a small format of
  our own.
- **A histogram rebuilt from the last snapshot** (a constant histogram
  whose count is the number of peers): its counters fall when peers go,
  which breaks `rate()`. Rejected for observations plus gauges.
- **Sub-second `issued_at`:** it needs obie/0.2 (signed form, schema,
  test vectors); every obie/0.1 node rejects such events. Rejected for
  now; see above.
- **`obie.publishers` as a list:** a type change of an indexed field.
  Rejected for a new field.
- **Reading the ended verdicts of a removed block from the store:** the
  sweep and the engine race, and the other publishers' share can be full.
  Rejected for the engine keeping its blocks' contributors.

## Consequences

- ADR 0015's metric list gains the `obie_gossip_*` metrics, and its audit
  fields gain `obie.contributors`. `obie_propagation_delay_seconds` has
  millisecond precision for events whose `id` carries their creation
  time, which is true of every event `obied` publishes.
- ADR 0023's 24 hours become `store.ended_retention`.
- The console, `/v1/peers`, the configuration reference, the monitoring
  guide and the example configuration describe the new metrics, fields
  and keys.
- #1765's harness can compute delivery ratio, latency percentiles,
  duplicate factor and hop counts from the metrics and the trace files.
