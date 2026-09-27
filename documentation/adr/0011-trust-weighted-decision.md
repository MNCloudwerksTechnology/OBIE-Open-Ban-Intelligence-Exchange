# ADR 0011: Trust-weighted decision engine

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1659](https://openproject.niew.dev/work_packages/1659)

## Context

ADR 0001 fixes the decision rule: operator-assigned trust weights,
`score = Σ weight(publisher) × confidence` over the distinct publishers'
latest active verdicts, block iff score ≥ threshold AND distinct publishers
≥ quorum. ADR 0008 gives every consumer the same view of the active verdicts
per indicator and notifies changes. The enforcer (a later work package)
needs a stream of block decisions, and the operator must be able to ask why
an address is or is not blocked.

## Decision

- **Package and lifecycle:** `internal/decision` is a lifecycle subsystem
  named `decision`, registered right after `store` (so it starts once the
  database is open and stops before it closes). `Start` subscribes to the
  store's change notifications *first*, then builds the initial view from
  `ListIndicators`, so no change is lost in between. Notifications only mark
  an indicator dirty; one worker goroutine re-reads dirty indicators and
  re-evaluates them. The store's writer goroutine is never blocked by the
  evaluation, and a burst of changes on one indicator is coalesced.
- **Evaluation is a pure function** of the indicator's active verdicts, the
  policy (from `trust` and `decision`), this node's peer ID and the time.
  Verdicts are summed in publisher order, so a decision is deterministic.
  - weight = `trust.local_weight` for this node's own peer ID, the weight in
    `trust.publishers` for listed peers, `trust.default_weight` otherwise.
  - Only active `ban` verdicts of publishers with weight > 0 *contribute*.
    `watch` verdicts and zero-weight publishers are listed in the
    explanation but never contribute.
  - `block` iff score ≥ `decision.threshold` (compared with a tolerance of
    1e-9, so e.g. 0.6 + 0.6 + 0.6 reaches 1.8 despite float rounding) AND
    contributors ≥ `decision.quorum`.
  - **Local autoblock** (`decision.local_autoblock`, default true): an active
    `ban` verdict of this node itself blocks on its own, provided
    `trust.local_weight` > 0 — setting the local weight to 0 means the
    operator does not trust the node's own detections at all.
  - **Expiry** of a block is the latest expiry among the verdicts that
    justify it (all contributors for a consensus block, the local verdict
    for an autoblock), capped at `decision.max_ttl` from the time of
    evaluation. When a capped block reaches its expiry while its verdicts
    are still active, the engine re-evaluates it (a periodic check, every
    ten seconds), which extends it by at most `max_ttl` again.
- **State kept:** a compact summary (state, score, contributor count,
  expiry, reason) per indicator with at least one active verdict; the full
  per-publisher explanation is recomputed on request from the store. This
  keeps memory proportional to the number of indicators, not verdicts.
- **Change stream:** `Engine.Subscribe` delivers `added` (became `block`),
  `updated` (still `block`, but expiry, score or contributors changed) and
  `removed` (no longer `block`) changes with the new decision and the cause
  (the store's reason, `startup` or `refresh`). A subscriber registered
  after `Start` first receives every current block as `added` with cause
  `snapshot`, atomically with its registration, so it neither misses nor
  duplicates a block. Callbacks run one at a time, must be fast and must not
  call `Subscribe`, `Start` or `Stop`; the enforcer queues them.
- **Failures:** if reading an indicator's verdicts fails, its previous
  decision is kept, the indicator stays pending and is retried every refresh
  interval, and the subsystem reports not ready until a pass succeeds.
- **Sovereignty placeholder:** the allow-list and operator overrides are not
  applied yet (WP #1660). Explanations carry a `sovereignty` section that
  states this, so the API shape does not change when they are.
- **Admin API:** `GET /v1/decisions/{indicator}` explains one indicator
  (an IPv4/IPv6 address, a CIDR range or an indicator key such as
  `ipv4:203.0.113.7`; an indicator without verdicts yields decision `none`);
  `GET /v1/decisions?state=block|none` lists the kept summaries (a block
  past its expiry is no longer listed as a block, even before its refresh).
  An address is explained from the verdicts on that address only; verdicts
  on a CIDR range containing it are explained under the range. As for
  peers, the wire types live in `internal/admin` and `internal/daemon`
  converts, so the admin API contract does not depend on the engine's
  internal types. `obiectl explain <ip>`
  prints the explanation, `obiectl decisions [--state block]` the list.
- **Mode:** the engine decides in both `observe` and `enforce` mode; whether
  a block is enforced is the enforcer's concern.

## Consequences

- One peer never blocks under the defaults (quorum 2, default weight 0).
- Changing trust or decision settings needs a restart, which rebuilds every
  decision at startup.
- An explanation is computed at request time and may, for at most one store
  sweep interval, differ from the kept summary; both converge on the next
  notification.
