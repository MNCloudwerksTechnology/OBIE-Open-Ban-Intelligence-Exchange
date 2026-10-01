# Routing simulation: T-static

No attack; 1 verdict/s on the static-bootstrap graph.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `5ce984d` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=T-static` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 1000 (10 % publish), static-bootstrap graph |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 180 s; publishing stops at 150 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.8428 [0.8417, 0.8440] |
| Latency p50 (ms) | 1272.9 [1216.0, 1329.9] |
| Latency p99 (ms) | 3869.9 [3772.5, 3967.2] |
| Latency max (ms) | 5174.7 [5041.1, 5308.3] |
| Duplicate factor (copies per delivered verdict) | 1.11 [1.10, 1.11] |
| Hop count, mean | 5.20 [5.01, 5.38] |
| Mesh degree at the end | 0.24 [0.23, 0.25] |
| Hop count predicted, ln N / ln(D−1) | 4.29 (N 1000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0045 [0.0034, 0.0056] |
| 2 | 0.1084 [0.1067, 0.1101] |
| 3 | 0.1240 [0.1126, 0.1355] |
| 4 | 0.1964 [0.1832, 0.2097] |
| 5 | 0.1708 [0.1584, 0.1833] |
| 6 | 0.1466 [0.1383, 0.1548] |
| 7 | 0.0997 [0.0900, 0.1093] |
| 8 | 0.0624 [0.0545, 0.0703] |
| 9 | 0.0376 [0.0295, 0.0456] |
| 10 | 0.0229 [0.0152, 0.0306] |
| 11 | 0.0122 [0.0078, 0.0165] |
| 12+ | 0.0145 [0.0077, 0.0213] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.1572 [0.1560, 0.1583] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 1000 | 0 | 2015 | 118 | 27 |
