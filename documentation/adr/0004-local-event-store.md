# ADR 0004: Local event store

- **Status:** Accepted
- **Date:** 2026-09-27
- **Work package:** [#1654](https://openproject.niew.dev/work_packages/1654)

## Context

ADR 0001 fixes BadgerDB v4 in the state directory as the node's storage. The
mesh, the admin API and the decision engine all need the same view of which
verdicts are active per indicator, and that view must survive a restart,
forget expired verdicts without operator action, ignore replayed events and
stay bounded on a small VPS (≤ 1 vCPU, 512 MB RAM, 100k active indicators).

## Decision

- **Package and lifecycle:** `internal/store` owns the database. `store.DB` is
  a lifecycle subsystem named `store`: `Start` opens Badger in
  `<node.state_dir>/db` (created with mode 0700) and starts the background
  loops, `Stop` ends them and closes the database, `Ready` reports ready while
  the database is open and the loops are healthy. It is registered first in
  `internal/daemon`, so it is stopped last. Consumers depend on the
  `store.Store` interface; `store.NewMemory` returns the same implementation
  on Badger's in-memory mode, so tests of other packages need no disk and no
  second implementation of the store's rules.
- **Dependency:** `github.com/dgraph-io/badger/v4` v4.8.0 — the newest release
  that still supports the module's Go 1.23 baseline (v4.9 needs Go 1.24).
- **Keyspaces** (one byte-string prefix each; indicator keys are
  `obieproto.Indicator.Key()`, which never contains `0x00`):
  - `e/<event id>` → the event (JSON). Kept until the event expires (Badger
    TTL); it serves `Get` and is the seen-ID set for deduplication.
  - `s/<event id>` → seen marker of an ignored event (stale verdict, ignored
    revocation), kept until the event would expire, so replays count as
    duplicates without the event being retrievable.
  - `v/<indicator>\x00<publisher>` → the publisher's latest verdict on the
    indicator and whether it was revoked. Kept until the verdict expires.
  - `x/<expiry, 8-byte big-endian Unix seconds><verdict or override key>` →
    expiry index of *active* verdicts and expiring overrides, without TTL. The
    sweep walks it in time order; entries are removed when their verdict is
    superseded, revoked or expired, or their override replaced, deleted or
    expired, so the index only ever holds what is still in effect.
  - `r/<verdict id>\x00<publisher>` → indicator of a revocation whose verdict
    has not been seen yet, so a verdict that arrives after its revocation
    (gossip reordering) is stored as revoked if publisher and indicator match.
    Kept until the revocation expires.
  - `o/<indicator>` → operator override (force-allow / force-block, optional
    expiry and note). Separate keyspace, never touched by events.
- **Rules:** events are expected to be validated (`obieproto.Decode`) before
  `Put`. `Put` ignores events whose ID was already seen, events that are
  already expired, and verdicts not newer (by `issued_at`, ties broken by
  event ID) than the publisher's current verdict on the indicator. A revocation
  deactivates the referenced verdict only if it comes from the verdict's
  publisher and names the same indicator; other revocations are ignored and
  counted. A revocation of a verdict not seen yet cannot be attributed and is
  accepted and held until it expires; once the verdict arrives it applies only
  if publisher and indicator match, and is counted as ignored otherwise. The
  mesh therefore must not treat `accepted` as proof that a revocation is
  legitimate. All writes are serialized by one mutex and applied in a single
  Badger transaction, so each `Put` is atomic.
- **Change notifications:** subscribers registered with `Subscribe` receive a
  `Change` (indicator and reason: verdict, revoke, expiry, override) after the
  write committed, whenever the set of active verdicts or the override of an
  indicator changed. Callbacks run synchronously on the writing goroutine and
  must be fast and must not write to the store.
- **Expiry:** every entry with a lifetime gets a Badger TTL, so expired data
  disappears from reads and compaction reclaims it. A sweep (every minute)
  walks the expiry index up to now, deletes what expired and notifies the
  affected indicators, so an expiry is notified within one sweep interval
  (reads stop returning the verdict at its expiry already). An index entry
  whose value cannot be decoded is logged and dropped rather than stalling
  the sweep. The value-log GC runs every ten minutes until it has
  nothing left to rewrite.
- **Memory budget:** Badger's defaults are sized for servers with gigabytes of
  RAM. The store uses 16 MiB memtables (at most 2), a 32 MiB block cache, a
  16 MiB index cache and 64 MiB value-log files. With 100k active indicators
  the open store holds about 185 MiB of Go heap (`BenchmarkInsertList100k`),
  within the 512 MB budget; the memtables and caches are bounded by these
  settings rather than by the number of indicators.
- **Metrics:** `obie_store_events_total{result}` counts `Put` outcomes
  (accepted, duplicate, stale, expired, foreign_revoke, invalid_revoke) in the
  Prometheus default registry; `DB.Stats` returns the same counts per database.

## Consequences

- The store's semantics exist exactly once; other work packages get realistic
  behavior from `store.NewMemory` in their tests.
- A notification means "re-read this indicator"; it carries no state, so
  consumers never act on a stale copy. Consumers that start after the store
  build their initial view with `ListIndicators`.
- The expiry index and the verdict records are updated in the same
  transaction; the sweep is the only path that removes index entries for
  expired verdicts, so an unclean shutdown can at worst delay a notification
  until the next sweep after restart.
- Overrides are stored only; applying them is the decision work package's job.
