# Routing simulation: C-bootkill

Every bootstrap hub stops for good at 10 min; nodes know no other peers.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `405e24c`, `5ce984d` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=C-bootkill` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 1,000 (10 % of the non-hubs publish), static-bootstrap graph (20 hubs) |
| kill | all 20 hubs stop at 600 s |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 900 s; publishing stops at 870 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.5641 [0.5599, 0.5684] |
| Latency p50 (ms) | 1180.9 [1164.5, 1197.3] |
| Latency p99 (ms) | 3448.4 [3393.8, 3503.0] |
| Latency max (ms) | 5021.6 [4837.3, 5205.9] |
| Duplicate factor (copies per delivered verdict) | 1.08 [1.08, 1.09] |
| Hop count, mean | 4.73 [4.61, 4.85] |
| Mesh recovery time (s), seeds that recovered | – |
| Seeds whose mesh recovered (share) | 0.0000 [0.0000, 0.0000] |
| Mesh degree at the end | 0.00 [0.00, 0.00] |
| Delivery ratio, before the kill | 0.8298 [0.8286, 0.8309] |
| Delivery ratio, after the kill | 0.0000 [0.0000, 0.0000] |
| Hop count predicted, ln N / ln(D−1) | 4.29 (N 1000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0000 [0.0000, 0.0000] |
| 2 | 0.1013 [0.1006, 0.1020] |
| 3 | 0.1768 [0.1649, 0.1888] |
| 4 | 0.2282 [0.2146, 0.2417] |
| 5 | 0.1944 [0.1872, 0.2017] |
| 6 | 0.1389 [0.1315, 0.1464] |
| 7 | 0.0770 [0.0685, 0.0855] |
| 8 | 0.0435 [0.0363, 0.0506] |
| 9 | 0.0213 [0.0172, 0.0255] |
| 10 | 0.0101 [0.0075, 0.0127] |
| 11 | 0.0049 [0.0032, 0.0067] |
| 12+ | 0.0035 [0.0017, 0.0052] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.4359 [0.4316, 0.4401] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 1000 | 0 | 2015 | 836 | 116 |
