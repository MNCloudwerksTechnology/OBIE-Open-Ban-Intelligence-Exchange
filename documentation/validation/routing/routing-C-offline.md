# Routing simulation: C-offline

10 nodes are offline for an hour, then rejoin with the same identity and store.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `405e24c`, `5ce984d` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=C-offline` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 300 (10 % publish), static-bootstrap graph |
| outage | 10 random nodes that are not hubs or publishers, offline from 60 s to 3,660 s; their links are gone meanwhile |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 3,780 s; publishing stops at 3,750 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.8126 [0.8100, 0.8151] |
| Latency p50 (ms) | 1060.2 [1041.3, 1079.1] |
| Latency p99 (ms) | 3297.1 [3243.6, 3350.7] |
| Latency max (ms) | 4436.4 [4307.1, 4565.7] |
| Duplicate factor (copies per delivered verdict) | 1.11 [1.11, 1.12] |
| Hop count, mean | 3.05 [2.96, 3.13] |
| Mesh recovery time (s), seeds that recovered | 0.9 [0.6, 1.1] (17 of 20 seeds) |
| Seeds whose mesh recovered (share) | 0.8500 [0.6785, 1.0000] |
| Mesh degree at the end | 0.25 [0.23, 0.27] |
| Delivery ratio, during the outage, rejoined nodes | 0.0003 [0.0002, 0.0004] |
| Delivery ratio, after the rejoin, rejoined nodes | 0.9275 [0.9147, 0.9402] |
| Hop count predicted, ln N / ln(D−1) | 3.54 (N 300, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0149 [0.0100, 0.0198] |
| 2 | 0.3253 [0.3196, 0.3310] |
| 3 | 0.3687 [0.3235, 0.4138] |
| 4 | 0.2083 [0.1861, 0.2305] |
| 5 | 0.0598 [0.0446, 0.0750] |
| 6 | 0.0198 [0.0061, 0.0335] |
| 7 | 0.0016 [0.0001, 0.0031] |
| 8 | 0.0017 [0.0000, 0.0045] |
| 9 | 0.0000 [0.0000, 0.0000] |
| 10 | 0.0000 [0.0000, 0.0000] |
| 11 | 0.0000 [0.0000, 0.0000] |
| 12+ | 0.0000 [0.0000, 0.0000] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.1550 [0.1525, 0.1575] |
| `offline` | 0.0324 [0.0324, 0.0325] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 300 | 0 | 601 | 3734 | 219 |
