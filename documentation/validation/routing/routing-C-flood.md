# Routing simulation: C-flood

Weight-0 Sybil publishers flood junk verdicts with the longest TTL; the real store evicts the verdict that expires first.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `cd3b43f`, `e463f47` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=C-flood` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 200 (10 % publish), static-bootstrap graph, real store (BadgerDB in memory) with store.max_indicators 2,000 (scaled from 1,000,000) |
| trusted verdicts | Poisson, 1/s from 30 s, TTL 7 days; the retained share counts those published before the flood (30–150 s) |
| flood | 10 Sybil hosts, 10 links each, that forward honest traffic; 100 junk verdicts/s network-wide from 150 s to 210 s (6,000), TTL 30 days, signed by 1,000 weight-0 keys in turn |
| run | 240 s; publishing stops at 210 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.8219 [0.8161, 0.8277] |
| Latency p50 (ms) | 286.2 [266.2, 306.3] |
| Latency p99 (ms) | 3169.3 [3139.1, 3199.4] |
| Latency max (ms) | 3647.3 [3562.2, 3732.3] |
| Duplicate factor (copies per delivered verdict) | 1.21 [1.19, 1.22] |
| Hop count, mean | 3.83 [3.74, 3.91] |
| Trusted verdicts retained under flood | 0.0000 [0.0000, 0.0000] |
| Mesh degree at the end | 0.76 [0.74, 0.79] |
| Delivery ratio, before the flood | 0.9073 [0.9048, 0.9097] |
| Delivery ratio, during the flood | 0.6519 [0.6413, 0.6625] |
| Hop count predicted, ln N / ln(D−1) | 3.29 (N 200, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0224 [0.0136, 0.0312] |
| 2 | 0.3238 [0.3147, 0.3329] |
| 3 | 0.1415 [0.1147, 0.1682] |
| 4 | 0.2207 [0.1942, 0.2472] |
| 5 | 0.0591 [0.0458, 0.0724] |
| 6 | 0.1506 [0.1234, 0.1778] |
| 7 | 0.0291 [0.0177, 0.0405] |
| 8 | 0.0375 [0.0296, 0.0454] |
| 9 | 0.0083 [0.0040, 0.0127] |
| 10 | 0.0061 [0.0038, 0.0084] |
| 11 | 0.0006 [0.0000, 0.0012] |
| 12+ | 0.0003 [0.0000, 0.0005] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.0939 [0.0916, 0.0962] |
| `rate_limited` | 0.0842 [0.0791, 0.0892] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 200 | 10 | 498 | 183 | 208 |
