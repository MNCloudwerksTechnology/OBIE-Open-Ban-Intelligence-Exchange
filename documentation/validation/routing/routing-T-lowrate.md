# Routing simulation: T-lowrate

No attack; 0.1 verdict/s on the static-bootstrap graph.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `5ce984d` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=T-lowrate` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 1000 (10 % publish), static-bootstrap graph |
| traffic | Poisson, 0.1 verdict/s network-wide, from 30 s |
| run | 660 s; publishing stops at 630 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.8376 [0.8365, 0.8387] |
| Latency p50 (ms) | 1130.8 [1110.7, 1150.9] |
| Latency p99 (ms) | 3396.2 [3349.3, 3443.1] |
| Latency max (ms) | 4544.4 [4403.1, 4685.8] |
| Duplicate factor (copies per delivered verdict) | 1.12 [1.12, 1.12] |
| Hop count, mean | 4.80 [4.69, 4.91] |
| Mesh degree at the end | 0.22 [0.21, 0.23] |
| Hop count predicted, ln N / ln(D−1) | 4.29 (N 1000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0042 [0.0031, 0.0053] |
| 2 | 0.1055 [0.1042, 0.1068] |
| 3 | 0.1697 [0.1595, 0.1799] |
| 4 | 0.2094 [0.1996, 0.2193] |
| 5 | 0.1922 [0.1848, 0.1997] |
| 6 | 0.1399 [0.1325, 0.1473] |
| 7 | 0.0864 [0.0783, 0.0945] |
| 8 | 0.0462 [0.0398, 0.0525] |
| 9 | 0.0232 [0.0184, 0.0280] |
| 10 | 0.0121 [0.0087, 0.0154] |
| 11 | 0.0052 [0.0036, 0.0068] |
| 12+ | 0.0059 [0.0034, 0.0084] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.1624 [0.1613, 0.1635] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 1000 | 0 | 2015 | 61 | 24 |
