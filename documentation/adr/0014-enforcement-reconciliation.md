# ADR 0014: Enforcement by reconciliation through a pluggable enforcer

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1661](https://openproject.niew.dev/work_packages/1661)

## Context

ADR 0011 streams block changes, ADR 0013 routes them through the mode gate.
Forwarding single changes to a firewall drifts: a change lost while the
backend fails, a table flushed by hand or entries left behind by a crashed
node are never corrected. What the decision engine decides must be exactly
what is applied, continuously and idempotently, and a runaway decision set
or a gap in the allow-list must never lock the operator out.

## Decision

- **Enforcer interface** (`internal/enforce`): `Setup(ctx)` (idempotent,
  keeps existing entries), `List(ctx) ([]Entry, error)` (unexpired entries),
  `Apply(ctx, add, remove []Entry)` (removals first, then additions; an
  entry in both is replaced), `Teardown(ctx)`. `Entry` is
  `{Prefix netip.Prefix, Expires time.Time}`: every entry carries its
  remaining timeout, so the backend (the kernel for nftables) drops it on
  its own even if `obied` dies.
- **Level-triggered reconciliation.** The `Gate` no longer forwards single
  changes; it keeps the current block decisions and the mode and notifies
  the `Reconciler`. The reconciler (subsystem `enforce`, registered right
  after `decision`) runs one pass at start, ~250 ms after a notification
  (changes arriving together are coalesced) and every
  `enforce.reconcile_interval`. A pass computes the desired entries, lists
  the applied ones, and applies the minimal difference; an entry whose
  expiry differs by more than 5 s is replaced.
- **Desired entries** = the gate's `block` decisions of address and CIDR
  indicators with at least 1 s left, deduplicated by prefix, then:
  - *Allow-list defence in depth:* every entry is checked against the
    effective allow-list right before apply. A protected entry (built-in,
    own, bootstrap) always refuses it; an `allowlist.cidrs`/files entry
    refuses it unless it is the operator's own `force_block` (the ADR 0013
    precedence). Refused blocks are logged once and counted.
  - *Cap:* at most `enforce.max_entries`, the operator's force-blocks
    first, then the highest score; the rest is skipped, logged once with a
    sample and counted.
- **Modes.** In `observe` the reconciler never calls `Setup`, `List` or
  `Apply`; it calls `Teardown` once (at the first pass, or after a switch
  from `enforce`), withdrawing whatever an earlier enforce run left. Its
  status detail says `observing`. Switching to `enforce` sets up the
  backend and applies the blocks within the debounce delay.
- **Failures** are retried with exponential backoff (1 s doubling up to
  `enforce.reconcile_interval`); notifications do not shorten the wait, so
  a mode switch during a failure takes effect with the next retry. A pass
  is bounded by 30 s, so a hanging backend call fails too.
  While failing, the subsystem is not ready: `/readyz` answers 503 and
  `obiectl status` shows the error. `obied` stops without tearing the
  backend down: entries expire by themselves and the next start reconciles
  them.
- **Backends.** `dryrun` (the default) keeps entries in memory, expires
  them like the kernel and logs every add and remove as JSON; nothing is
  blocked. `nftables` follows in its own work package; until then
  `obied` refuses to start with it.
- **Visibility.** `GET /v1/enforced` / `obiectl enforced [--json]` lists
  the applied entries from `List`. Prometheus: `obie_enforce_entries`,
  `obie_enforce_skipped_entries{reason}`, `obie_enforce_enforcing`,
  `obie_enforce_failures_total`.

## Consequences

- A hand-flushed table, a crashed node or a failed apply heals within one
  reconcile interval; the backend is the only state that has to survive.
- `enforce → observe` withdraws everything through `Teardown`, not by
  removing entries one by one.
- Backends must be idempotent and must report expiries accurately enough
  (within 5 s) not to be rewritten every pass.
- ADR 0013's "switching replays the current blocks as `added` / withdraws
  them as `removed` (cause `mode`)" is superseded: switching only notifies
  the reconciler, which converges on the new mode.
