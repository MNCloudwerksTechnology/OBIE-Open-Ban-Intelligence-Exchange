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
| `obie_events_published_total` | counter | `type` (`verdict`, `revoke`) | Events this [node](../glossary.md#node) published. |
| `obie_store_active_indicators` | gauge | | [Indicators](../glossary.md#indicator) with at least one active [verdict](../glossary.md#verdict). |
| `obie_store_active_verdicts` | gauge | | Active verdicts (one per publisher and indicator). |
| `obie_decisions` | gauge | `state` (`block`, `none`, `allowed`) | Decisions the engine keeps, by state. |
| `obie_enforcer_entries` | gauge | `family` (`ipv4`, `ipv6`) | Entries the enforcement backend applies (0 in [observe mode](../glossary.md#observe-mode)). |
| `obie_enforcer_apply_total` | counter | `result` (`success`, `error`) | Reconciliation passes. |
| `obie_enforcer_apply_duration_seconds` | histogram | | Duration of the backend calls that apply a change. |
| `obie_enforcer_skipped_total` | counter | `reason` (`allowlist`, `max_entries`) | Decided blocks newly left out of the backend. |
| `obie_propagation_delay_seconds` | histogram | | Receipt time minus `issued_at` of accepted events. `issued_at` has whole seconds, and the clocks of both nodes count in. |
| `obie_admin_requests_total` | counter | `endpoint`, `code` | Admin API requests by endpoint pattern (e.g. `POST /v1/reports`; `unmatched` for none) and HTTP status. |
| `obie_store_events_total` | counter | `result` | Events passed to the store, by outcome; `full` counts verdicts refused because the store was full and they would have expired first. |
| `obie_store_verdict_records` | gauge | | Verdicts the store holds (active, [revoked](../glossary.md#revocation) or expired but not yet swept), bounded by `store.max_indicators`. |
| `obie_store_evictions_total` | counter | | Stored verdicts evicted, the one expiring first each, to keep the store within `store.max_indicators`. |

Useful queries:

```promql
obie_peers_connected == 0                                    # isolated node
rate(obie_events_received_total{outcome=~"invalid_.*"}[5m])  # peers sending garbage
rate(obie_enforcer_apply_total{result="error"}[5m]) > 0      # enforcement failing
histogram_quantile(0.95, sum by (le) (rate(obie_propagation_delay_seconds_bucket[15m])))
rate(obie_store_evictions_total[15m]) > 0                    # store full: a flood, or max_indicators too low
```

### Grafana dashboard

[`grafana-dashboard.json`](grafana-dashboard.json) is a minimal dashboard
with the mode, peers, events by outcome, active blocks, enforcer apply
latency and propagation delay. Import it (Dashboards → New → Import) and
pick your Prometheus data source; the `instance` variable selects nodes.

## Audit log

Set `audit.path` to an absolute file path, e.g.
`/var/log/obie/audit.jsonl`; the directory must exist and be writable by
the user `obied` runs as. `obied` then appends one JSON object per line
for every decision change:

| `event.action` | When |
|---|---|
| `block-added` | An indicator became blocked (consensus, local autoblock or force-block). |
| `block-updated` | A block's expiry, score, publishers or rule changed. |
| `block-removed` | An indicator is no longer blocked. |
| `allowed-by-allowlist` | The [allow-list](../glossary.md#allow-list) or a force-allow keeps an indicator with verdicts from being blocked. |
| `override-set` / `override-removed` | The operator set or deleted an [override](../glossary.md#override) (`obiectl allow`, `block`, `unoverride`). |
| `local-report` | This node issued a verdict (`obiectl report`, Fail2Ban). |
| `revocation` | This node revoked one of its verdicts (`obiectl revoke`). |

Records follow the Elastic Common Schema (nested objects):

```json
{"@timestamp":"2026-09-28T12:00:00.123Z","event":{"kind":"event","module":"obie","dataset":"obie.audit","action":"block-added","outcome":"success","reason":"consensus: score 1.8 >= threshold 1.8, 2 >= quorum 2"},"source":{"ip":"203.0.113.7"},"rule":{"name":"consensus"},"obie":{"indicator":"ipv4:203.0.113.7","mode":"enforce","state":"block","score":1.8,"threshold":1.8,"publishers":2,"cause":"verdict","expires_at":"2026-09-29T12:00:00.123Z"}}
```

- `source.ip` is set for single addresses; ranges are only in
  `obie.indicator` (e.g. `cidr:203.0.113.0/24`).
- `rule.name` is `consensus`, `local_autoblock`, `allowlist`,
  `force_allow`, `force_block`, `local_report` or `revocation`.
- `obie.score`, `obie.threshold` and `obie.publishers` (contributing
  publishers) are set for decisions; `obie.mode` is the `node.mode` at the
  time; `obie.cause` says what triggered a decision change (`verdict`,
  `revoke`, `expiry`, `evict`, `override`, `refresh`, `reload`).
- Blocks that exist when `obied` starts are not recorded again; use
  `obiectl decisions` for the current state.

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
