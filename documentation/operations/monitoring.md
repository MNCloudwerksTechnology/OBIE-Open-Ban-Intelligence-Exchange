# Monitoring an OBIE node

`obied` exposes Prometheus metrics for Prometheus/Grafana and can write a
decision audit log for Loki, Elasticsearch, Splunk or any other tool that
reads JSON lines. Both are described here; the design is recorded in
[ADR 0015](../adr/0015-metrics-and-audit-log.md).

## Metrics

`obied` serves `/metrics`, `/healthz` and `/readyz` on `metrics.listen`
(default `127.0.0.1:9464`). Scrape it like any other target:

```yaml
scrape_configs:
  - job_name: obie
    static_configs:
      - targets: ["127.0.0.1:9464"]
```

No label ever carries an IP address or a [peer ID](../glossary.md#peer-id).

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `obie_build_info` | gauge | `version` | Always 1; the build version. |
| `obie_node_mode` | gauge | `mode` (`observe`, `enforce`) | 1 for the current `node.mode`. |
| `obie_peers_connected` | gauge | | Connected mesh peers. |
| `obie_peers_configured` | gauge | | Bootstrap peers in `mesh.bootstrap`. |
| `obie_events_received_total` | counter | `outcome` (`accepted`, `duplicate`, `rate_limited`, `expired`, `invalid_signature`, `invalid_schema`, `too_large`) | Events received from peers, by validation outcome. |
| `obie_events_published_total` | counter | `type` (`verdict`, `revoke`) | Events this [node](../glossary.md#node) sent to the mesh; one published while no peer was on the topic counts when it is sent, once a peer joins. |
| `obie_store_active_indicators` | gauge | | [Indicators](../glossary.md#indicator) with at least one active [verdict](../glossary.md#verdict). |
| `obie_store_active_verdicts` | gauge | | Active verdicts (one per publisher and indicator). |
| `obie_decisions` | gauge | `state` (`block`, `none`, `allowed`) | Decisions the engine keeps, by state. |
| `obie_enforcer_entries` | gauge | `family` (`ipv4`, `ipv6`) | Entries the enforcement backend applies (0 in [observe mode](../glossary.md#observe-mode)). |
| `obie_enforcer_apply_total` | counter | `result` (`success`, `error`) | Reconciliation passes. |
| `obie_enforcer_apply_duration_seconds` | histogram | | Duration of the backend calls that apply a change. |
| `obie_enforcer_skipped_total` | counter | `reason` (`allowlist`, `max_entries`) | Decided blocks newly left out of the backend. |
| `obie_propagation_delay_seconds` | histogram | | Receipt time minus the creation time of accepted events: to the millisecond from the event's ID (a UUIDv7) when that lies within the second of its `issued_at`, which the protocol gives in whole seconds; else from `issued_at`. The clocks of both nodes count in. |
| `obie_admin_requests_total` | counter | `endpoint`, `code` | Admin API requests by endpoint pattern (e.g. `POST /v1/reports`; `unmatched` for none) and HTTP status. |
| `obie_store_events_total` | counter | `result` | Events passed to the store, by outcome; `full` counts verdicts refused because the store was full and they would have expired first. |
| `obie_store_verdict_records` | gauge | | Verdicts the store holds (active, [revoked](../glossary.md#revocation) or expired but not yet swept), bounded by `store.max_indicators`. |
| `obie_store_evictions_total` | counter | | Stored verdicts evicted, the one expiring first each, to keep the store within `store.max_indicators`. |
| `obie_gossip_deliveries_total` | counter | | Events from peers that GossipSub accepted and delivered: the first valid copy of each. |
| `obie_gossip_duplicates_total` | counter | | Copies of an event the node had already seen, dropped by GossipSub before the validator runs; `obie_events_received_total{outcome="duplicate"}` counts only those the store recognises. |
| `obie_gossip_rejects_total` | counter | `reason` (`validation_failed`, `queue_full`, `throttled`, `signature`, `author`, `blacklisted`, `self_origin`, `other`) | Messages from peers that GossipSub dropped: `validation_failed` are those the validator rejected (`obie_events_received_total` says why), `queue_full` and `throttled` those validation had no room for. |
| `obie_gossip_ignores_total` | counter | | Messages from peers the validator ignored: duplicates the store recognises, rate-limited and slightly expired events. |
| `obie_gossip_grafts_total` / `obie_gossip_prunes_total` | counter | | Peers added to and removed (by a PRUNE) from the node's mesh of the topic; a disconnect removes a peer without one. |
| `obie_gossip_ihave_total` / `obie_gossip_iwant_total` | counter | `direction` (`sent`, `received`) | Event IDs announced in IHAVE gossip and requested in IWANT. |
| `obie_gossip_mesh_peers` | gauge | | Peers in the node's mesh of the topic, to which it relays every event in full. |
| `obie_gossip_peer_score` | histogram | | GossipSub peer scores, one observation per scored peer every 10 seconds (buckets at -200, -100, -50, -10, -1, 0, 1, 5, 10). |
| `obie_gossip_peers_below_threshold` | gauge | `threshold` (`gossip`, `publish`, `graylist`) | Peers whose score was below -50 (no gossip with them), -100 (none of this node's events) or -200 (their messages ignored) at the last reading. |
| `obie_gossip_scored_peers` | gauge | | Peers with a score at the last reading: the connected ones and those that left within the hour. |
| `obie_store_ended_verdicts` | gauge | `state` (`revoked`, `expired`) | Verdicts the store keeps for `store.ended_retention` (30 days by default) after they ended, for the console's [verdicts view](console.md#the-verdicts-view) and to trace blocks back to them; of other publishers at most a tenth of `store.max_indicators` in each state. |

Useful queries:

```promql
obie_peers_connected == 0                                    # isolated node
rate(obie_events_received_total{outcome=~"invalid_.*"}[5m])  # peers sending garbage
rate(obie_enforcer_apply_total{result="error"}[5m]) > 0      # enforcement failing
histogram_quantile(0.95, sum by (le) (rate(obie_propagation_delay_seconds_bucket[15m])))
rate(obie_store_evictions_total[15m]) > 0                    # store full: a flood, or max_indicators too low
# copies received per event: gossip's duplicate factor
(rate(obie_gossip_deliveries_total[15m]) + rate(obie_gossip_duplicates_total[15m])) / rate(obie_gossip_deliveries_total[15m])
rate(obie_gossip_grafts_total[15m]) + rate(obie_gossip_prunes_total[15m])  # mesh churn
obie_gossip_peers_below_threshold{threshold="graylist"} / obie_gossip_scored_peers  # share of peers ignored
```

Each peer's own score, the score limits it is below (`below`) and the
score's components are in `obiectl peers --json` (`gossip_score`), the admin API's
`GET /v1/peers`, and the web console's
[peers view](console.md#the-peers-view).

### Grafana dashboard

[`grafana-dashboard.json`](grafana-dashboard.json) is a minimal dashboard
with the mode, peers, events by outcome, active blocks, enforcer apply
latency, propagation delay, the gossip mesh and its churn, the copies
received, the duplicate factor and the peer scores. Import it (Dashboards → New → Import) and
pick your Prometheus data source; the `instance` variable selects nodes.

## Audit log

Set `audit.path` to an absolute file path, e.g.
`/var/log/obie/audit.jsonl`; the directory must exist and be writable by
the user `obied` runs as. `obied` then appends one JSON object per line
for every decision change, and for the node's peers, reloads and mode
changes:

| `event.action` | When |
|---|---|
| `block-added` | An indicator became blocked (consensus, local autoblock or force-block). |
| `block-updated` | A block's expiry, score, publishers or rule changed. |
| `block-removed` | An indicator is no longer blocked. |
| `allowed-by-allowlist` | The [allow-list](../glossary.md#allow-list) or a force-allow keeps an indicator with verdicts from being blocked. |
| `override-set` / `override-removed` | The operator set or deleted an [override](../glossary.md#override) (`obiectl allow`, `block`, `unoverride`, or the web console). |
| `local-report` | This node issued a verdict (`obiectl report`, Fail2Ban, or the web console). |
| `revocation` | This node revoked one of its verdicts (`obiectl revoke`, or the web console). |
| `peer-connected` / `peer-disconnected` | The mesh connected to a peer, or lost its last connection to it. |
| `config-reloaded` | A reload (SIGHUP, also logrotate's) took effect; a rejected reload is not recorded. |
| `mode-changed` | A reload switched `node.mode`. |

Records follow the Elastic Common Schema (nested objects):

```json
{"@timestamp":"2026-09-28T12:00:00.123Z","event":{"kind":"event","module":"obie","dataset":"obie.audit","action":"block-added","outcome":"success","reason":"consensus: score 1.8 >= threshold 1.8, 2 >= quorum 2"},"source":{"ip":"203.0.113.7"},"rule":{"name":"consensus"},"obie":{"indicator":"ipv4:203.0.113.7","mode":"enforce","state":"block","score":1.8,"threshold":1.8,"publishers":2,"contributors":[{"peer_id":"12D3KooWPeerA","weight":1,"confidence":0.9,"verdict_id":"0199a1b2-c3d4-7e5f-8a6b-00000000000a"},{"peer_id":"12D3KooWPeerB","weight":0.9,"confidence":1,"verdict_id":"0199a1b2-c3d4-7e5f-8a6b-00000000000b"}],"cause":"verdict","expires_at":"2026-09-29T12:00:00.123Z"}}
```

- `source.ip` is set for single addresses; ranges are only in
  `obie.indicator` (e.g. `cidr:203.0.113.0/24`). Records about no address
  (peers, reloads, mode changes) have neither, and no `rule.name`.
- `rule.name` is `consensus`, `local_autoblock`, `allowlist`,
  `force_allow`, `force_block`, `local_report` or `revocation`.
- `obie.score`, `obie.threshold` and `obie.publishers` (contributing
  publishers) are set for decisions; `obie.mode` is the `node.mode` at the
  time; `obie.cause` says what triggered a decision change (`verdict`,
  `revoke`, `expiry`, `evict`, `override`, `refresh`, `reload`).
- `obie.contributors` names the verdicts that count in a block, on every
  `block-added`, `block-updated` and `block-removed` record: each
  publisher's `peer_id`, the trust `weight` and `confidence` it counted
  with, and the `verdict_id`. A removal names the verdicts of the block
  that ended; a force-block without verdicts has `[]`. The store keeps a
  verdict for `store.ended_retention` (30 days by default) after it
  ended, so `obiectl` and the console's
  [verdicts view](console.md#the-verdicts-view) still show it that long.
  A new verdict of a contributing publisher is a `block-updated` even if
  score and expiry stay the same.
- `obie.peer_id` and `obie.peer_name` (its `trust.publishers` name, if
  any) name the peer of `peer-connected` and `peer-disconnected`;
  `obie.settings` lists the settings a reload changed and applied,
  `obie.restart_settings` those that wait for a restart; `obie.mode` of
  `mode-changed` is the new mode, `obie.previous_mode` the one before.
- `obie.origin` says through which door an operator action came —
  `admin-api` (`obiectl`, Fail2Ban and every other client of the admin
  socket) or `console` (the [web console](console.md#act-from-the-console))
  — and `user.id` and `user.name` name the local user who carried it out:
  the UID from the socket's peer credentials, and its name if the host
  knows it. Only `override-set`, `override-removed`, `local-report` and
  `revocation` have them:

  ```json
  {"@timestamp":"2026-09-28T12:00:00.123Z","event":{"kind":"event","module":"obie","dataset":"obie.audit","action":"override-set","outcome":"success","reason":"operator force_block override set"},"source":{"ip":"192.0.2.99"},"rule":{"name":"force_block"},"user":{"id":"1000","name":"alice"},"obie":{"indicator":"ipv4:192.0.2.99","mode":"enforce","expires_at":"2026-09-28T13:00:00.123Z","note":"scanner","origin":"console"}}
  ```

- Blocks that exist when `obied` starts are not recorded again; use
  `obiectl decisions` for the current state.
- The [web console's activity timeline](console.md#the-activity-timeline)
  reads this file back, so it shows the same records as your SIEM. `obied`
  opens the file for reading too; if it may only write it, or it is no
  regular file, the timeline shows the last records it keeps in memory
  instead.

The full set of fields per action is in
[the golden test file](../../internal/audit/testdata/audit.golden.jsonl).

### Rotation

`obied` reopens the audit log on SIGHUP. SIGHUP also reloads the
configuration and the allow-list files: a rotation applies any edit already
saved to them, so finish and check edits (`obied --check-config`) before
the nightly rotation. With logrotate:

```text
/var/log/obie/audit.jsonl {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    create 0640 obie obie
    postrotate
        systemctl kill --signal=HUP obied.service >/dev/null 2>&1 || pkill -HUP -x obied || true
    endscript
}
```

### Grafana Alloy / Promtail (Loki)

Grafana Alloy:

```alloy
local.file_match "obie_audit" {
  path_targets = [{"__path__" = "/var/log/obie/audit.jsonl", "job" = "obie-audit"}]
}

loki.source.file "obie_audit" {
  targets    = local.file_match.obie_audit.targets
  forward_to = [loki.process.obie_audit.receiver]
}

loki.process "obie_audit" {
  stage.json {
    expressions = {timestamp = "\"@timestamp\"", action = "event.action"}
  }
  stage.timestamp {
    source = "timestamp"
    format = "RFC3339Nano"
  }
  // event.action has a handful of values; never make source.ip a label.
  stage.labels {
    values = {action = ""}
  }
  forward_to = [loki.write.default.receiver]
}
```

Promtail (deprecated in favour of Alloy):

```yaml
scrape_configs:
  - job_name: obie-audit
    static_configs:
      - targets: [localhost]
        labels:
          job: obie-audit
          __path__: /var/log/obie/audit.jsonl
    pipeline_stages:
      - json:
          expressions:
            timestamp: '"@timestamp"'
            action: event.action
      - timestamp:
          source: timestamp
          format: RFC3339Nano
      - labels:
          action:
```

Query in Grafana with LogQL, e.g. every block of one address:

```logql
{job="obie-audit", action="block-added"} | json | source_ip="203.0.113.7"
```

### Filebeat (Elasticsearch)

```yaml
filebeat.inputs:
  - type: filestream
    id: obie-audit
    paths:
      - /var/log/obie/audit.jsonl
    parsers:
      - ndjson:
          target: ""
          overwrite_keys: true
          add_error_key: true
```

The records already carry ECS names, so they land in `event.*`,
`source.ip` and `rule.name`; the node-specific fields are under `obie.*`.
Filebeat follows the file across logrotate's rename by itself.

## Per-event trace

For simulations and short diagnostics, set `mesh.trace_path` to an
absolute file path in an existing directory and restart `obied`. The node
then appends one JSON line for every copy of an event it receives and for
every event it publishes:

```json
{"node":"12D3KooWNodeC","event":"0199a1b2-c3d4-7e5f-8a6b-00000000000a","from":"12D3KooWNodeB","at":"2026-09-30T12:00:00.123456789Z","outcome":"accepted"}
```

`node` is the node's own peer ID, `event` the event ID, `from` the peer
that forwarded the copy (the node itself for `published`), `at` when, and
`outcome` what became of it: `published`, an outcome of
`obie_events_received_total` (`accepted`, `duplicate`, `rate_limited`, …),
`duplicate` for a copy GossipSub dropped because the node had seen it, or
a reason of `obie_gossip_rejects_total` for one it dropped before
validation. The trace files of several nodes, joined, show for every
event which peer each node first accepted it from, and so the hops and
the path it took from its publisher; `internal/eventtrace` joins them.
A copy takes about 230 bytes; the node never rotates or reads the file,
so empty `mesh.trace_path` again when you are done. The file names peers,
not addresses. The design is in
[ADR 0032](../adr/0032-gossip-instrumentation-and-attribution.md).
