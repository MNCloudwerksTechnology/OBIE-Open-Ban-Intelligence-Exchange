# Routing simulation: T-burst-static

No attack; 1 verdict/s on the static-bootstrap graph, with a scanning burst of 200 verdicts/s for 60 s.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `cd3b43f` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=T-burst-static` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 300 (30 publish), static-bootstrap graph |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 180 s; publishing stops at 150 s |
| burst | a scanning campaign: Poisson, 200 verdicts/s from 60 s to 120 s on top |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.3857 [0.3832, 0.3882] |
| Latency p50 (ms) | 1280.1 [1233.1, 1327.0] |
| Latency p99 (ms) | 4013.0 [3927.4, 4098.7] |
| Latency max (ms) | 6003.1 [5835.3, 6170.9] |
| Duplicate factor (copies per delivered verdict) | 1.10 [1.09, 1.10] |
| Hop count, mean | 2.99 [2.94, 3.05] |
| Mesh degree at the end | 0.27 [0.25, 0.29] |
| Delivery ratio, during the burst | 0.3834 [0.3808, 0.3859] |
| Hop count predicted, ln N / ln(D−1) | 3.54 (N 300, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0314 [0.0213, 0.0415] |
| 2 | 0.4126 [0.4046, 0.4207] |
| 3 | 0.2300 [0.1950, 0.2650] |
| 4 | 0.2251 [0.2031, 0.2472] |
| 5 | 0.0689 [0.0579, 0.0799] |
| 6 | 0.0258 [0.0185, 0.0331] |
| 7 | 0.0050 [0.0032, 0.0068] |
| 8 | 0.0010 [0.0001, 0.0020] |
| 9 | 0.0001 [0.0000, 0.0004] |
| 10 | 0.0000 [0.0000, 0.0000] |
| 11 | 0.0000 [0.0000, 0.0000] |
| 12+ | 0.0000 [0.0000, 0.0000] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.2116 [0.2001, 0.2230] |
| `rate_limited` | 0.4027 [0.3912, 0.4142] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 300 | 0 | 601 | 12135 | 331 |
