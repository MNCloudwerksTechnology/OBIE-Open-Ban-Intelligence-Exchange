# ADR 0015: Prometheus metrics and the decision audit log

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1663](https://openproject.niew.dev/work_packages/1663)

## Context

ADR 0001 promises Prometheus `/metrics` and a JSON decision audit log.
Operators want to see what their node does in the tools they already run —
Prometheus/Grafana for numbers, Loki, Elasticsearch or Splunk for events —
without writing parsers. Until now `/metrics` only carried a few
per-package metrics (`obie_store_events_total`, `obie_enforce_*`), and
`audit.path` was accepted but unused.

## Decision

- **Where metrics live.** Each metric is defined in the package that
  updates it and registered on the Prometheus default registry in `init`
  (the convention `store` and `enforce` already followed); `internal/ops`
  keeps serving the default registry. Gauges derived from state are set
  where the state changes (mesh connection events, the gate's mode, every
  evaluation batch of the decision engine, every reconciliation pass), not
  computed on scrape, so a scrape never touches the store.
- **Names.** Namespace `obie`, no subsystem: `obie_build_info{version}`,
  `obie_node_mode{mode}`, `obie_peers_connected`, `obie_peers_configured`,
  `obie_events_received_total{outcome}`, `obie_events_published_total{type}`,
  `obie_store_active_indicators`, `obie_store_active_verdicts`,
  `obie_decisions{state}`, `obie_enforcer_entries{family}`,
  `obie_enforcer_apply_total{result}`, `obie_enforcer_apply_duration_seconds`,
  `obie_enforcer_skipped_total{reason}`, `obie_propagation_delay_seconds`,
  `obie_admin_requests_total{endpoint,code}`. The pre-release
  `obie_enforce_*` metrics of ADR 0014 are replaced by the `obie_enforcer_*`
  and `obie_node_mode` ones; `obie_store_events_total` stays.
  *Amended by [ADR 0032](0032-gossip-instrumentation-and-attribution.md):
  the `obie_gossip_*` metrics count what GossipSub does (deliveries,
  duplicates, rejects by reason, ignores, grafts, prunes, IHAVE and IWANT
  by direction, mesh size) and the peer scores (a histogram, the peers
  below each threshold and the peers scored), with no peer or IP label.*
- **No high-cardinality labels.** Every label value comes from a closed set
  (outcomes, event types, states, families, results, reasons, modes). The
  admin endpoint label is the `ServeMux` pattern that served the request
  (e.g. `GET /v1/decisions/{indicator...}`), never the path, which may hold
  an address; requests that reach no endpoint are `unmatched`. No IPs, no
  peer IDs.
- **Semantics.** `enforcer_apply_total` counts reconciliation passes (in
  either mode) by result; `enforcer_apply_duration_seconds` times the backend
  calls that apply a change; `enforcer_skipped_total` counts a block when it
  becomes skipped, not on every pass. `propagation_delay_seconds` is receipt
  time minus `issued_at` of accepted events (whole-second precision, clamped
  at 0 for clocks running ahead). *Amended by
  [ADR 0032](0032-gossip-instrumentation-and-attribution.md): minus the
  creation time the event's UUIDv7 `id` carries, to the millisecond, when
  it lies within `issued_at`'s second.* `decisions{state}` counts the
  decisions the engine keeps: every indicator with active verdicts, plus
  force-blocks.
- **Audit log** (`internal/audit`, subsystem `audit`, registered after
  `store` and before `decision` when `audit.path` is set): one JSON object
  per line, appended to a file opened with `O_APPEND` (mode 0640; the
  directory must exist). Field names follow the Elastic Common Schema as
  nested objects: `@timestamp` (UTC, milliseconds), `event.kind`,
  `event.module` (`obie`), `event.dataset` (`obie.audit`), `event.action`,
  `event.outcome` (always `success`: only changes that took effect are
  recorded), `event.reason`, `source.ip` (single addresses only; ranges are
  in `obie.indicator`), `rule.name`, and `obie.*` (`indicator`, `mode`,
  `state`, `score`, `threshold`, `publishers`, `cause`, `expires_at`,
  `event_id`, `revokes`, `note`). Actions: `block-added`, `block-updated`,
  `block-removed` (the engine's block stream), `allowed-by-allowlist`
  (the new transition stream, below), `override-set`, `override-removed`,
  `local-report`, `revocation` (admin API). Blocks rebuilt at startup are
  no change and are not recorded.
  *Extended by [ADR 0025](0025-console-activity-timeline.md):
  `peer-connected`, `peer-disconnected`, `config-reloaded` and
  `mode-changed` record peers, reloads and mode changes; `obie.indicator` is
  left out of records about no address; the last 10,000 records are also
  kept in memory, with or without `audit.path`, for the console's
  timeline.*
  *Amended by [ADR 0032](0032-gossip-instrumentation-and-attribution.md):
  `block-added`, `block-updated` and `block-removed` records list the
  verdicts that count in the block in `obie.contributors` (`peer_id`,
  `weight`, `confidence`, `verdict_id`); a removal lists those of the
  block that ended. `obie.publishers` stays a count.*
- **Transition stream.** `decision.Engine.SubscribeTransitions` streams every
  change of a decision's state (block/none/allowed) with the previous state;
  the block change stream of ADR 0011 is unchanged.
- **Rotation.** SIGHUP reopens the audit log before the configuration is
  reloaded (ADR 0013), so logrotate's `postrotate` sends SIGHUP; if the file
  cannot be reopened, the node logs it and writes on to the old one. A
  failed write is logged and does not block the decision.

## Consequences

- Dashboards and alerts query stable, documented names; the example
  dashboard (`documentation/operations/grafana-dashboard.json`) is tested
  against a live scrape.
- Metrics are process-wide; tests that run several nodes in one process
  observe differences, not absolute values.
- The audit log is not a second source of truth: changes while the node was
  down (e.g. expiries) are not recorded, and the startup state is only in
  `obiectl decisions`.
- SIGHUP both reloads the configuration and reopens the audit log: a
  rotation also applies configuration edits already saved on disk.
- Records are written synchronously from the engine's subscriber callbacks;
  a hanging filesystem under `audit.path` stalls decisions, so the audit log
  belongs on local disk.
