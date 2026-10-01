# Routing simulation: C-junk

Two Sybil hosts inject valid junk into one hub at the per-peer rate limit; the hub relays it and its neighbors' buckets for it run dry.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `5ce984d`, `cd3b43f` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=C-junk` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 300 (10 % publish), static-bootstrap graph (6 hubs) |
| junk | 2 Sybil hosts linked to hub 0 that forward honest traffic, each injecting 50 junk verdicts/s (the default per-peer limit, mesh.rate_limit.peer) from 60 s to 180 s, signed by 500 weight-0 keys in turn |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 210 s; publishing stops at 180 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.6662 [0.6513, 0.6811] |
| Latency p50 (ms) | 1302.3 [1255.7, 1349.0] |
| Latency p99 (ms) | 3878.4 [3776.2, 3980.5] |
| Latency max (ms) | 5109.6 [4958.8, 5260.5] |
| Duplicate factor (copies per delivered verdict) | 1.10 [1.09, 1.10] |
| Hop count, mean | 3.09 [3.02, 3.15] |
| Mesh degree at the end | 0.26 [0.24, 0.27] |
| Delivery ratio, before the junk | 0.8446 [0.8413, 0.8479] |
| Delivery ratio, during the junk | 0.6216 [0.6046, 0.6387] |
| Hop count predicted, ln N / ln(D−1) | 3.54 (N 300, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0176 [0.0124, 0.0229] |
| 2 | 0.3900 [0.3753, 0.4046] |
| 3 | 0.2458 [0.2142, 0.2774] |
| 4 | 0.2299 [0.2049, 0.2548] |
| 5 | 0.0758 [0.0639, 0.0877] |
| 6 | 0.0350 [0.0230, 0.0469] |
| 7 | 0.0054 [0.0033, 0.0074] |
| 8 | 0.0006 [0.0000, 0.0013] |
| 9 | 0.0000 [0.0000, 0.0000] |
| 10 | 0.0000 [0.0000, 0.0000] |
| 11 | 0.0000 [0.0000, 0.0000] |
| 12+ | 0.0000 [0.0000, 0.0000] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.2069 [0.1879, 0.2258] |
| `rate_limited` | 0.1269 [0.1185, 0.1354] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 300 | 2 | 603 | 153 | 181 |
