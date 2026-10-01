# Routing simulation: T-burst-regular

No attack; 1 verdict/s on the random 20-regular graph, with a scanning burst of 200 verdicts/s for 60 s.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `e463f47` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=T-burst-regular` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 100 (30 publish), random 20-regular graph |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 180 s; publishing stops at 150 s |
| burst | a scanning campaign: Poisson, 200 verdicts/s from 60 s to 120 s on top |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.9159 [0.9099, 0.9219] |
| Latency p50 (ms) | 60.2 [54.6, 65.9] |
| Latency p99 (ms) | 151.6 [145.2, 158.0] |
| Latency max (ms) | 505.6 [306.7, 704.5] |
| Duplicate factor (copies per delivered verdict) | 6.37 [6.26, 6.49] |
| Hop count, mean | 2.36 [2.33, 2.39] |
| Mesh degree at the end | 7.59 [7.48, 7.70] |
| Delivery ratio, during the burst | 0.9155 [0.9095, 0.9215] |
| Hop count predicted, ln N / ln(D−1) | 2.86 (N 100, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.1812 [0.1783, 0.1841] |
| 2 | 0.4020 [0.3931, 0.4109] |
| 3 | 0.3138 [0.3093, 0.3184] |
| 4 | 0.0845 [0.0775, 0.0915] |
| 5 | 0.0154 [0.0120, 0.0189] |
| 6 | 0.0027 [0.0017, 0.0037] |
| 7 | 0.0003 [0.0002, 0.0004] |
| 8 | 0.0001 [0.0000, 0.0001] |
| 9 | 0.0000 [0.0000, 0.0000] |
| 10 | 0.0000 [0.0000, 0.0000] |
| 11 | 0.0000 [0.0000, 0.0000] |
| 12+ | 0.0000 [0.0000, 0.0000] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `rate_limited` | 0.0841 [0.0781, 0.0901] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 100 | 0 | 1000 | 12140 | 486 |
