# Routing simulation: C-preempt

Preempters linked to the revokers and to many honest nodes answer each chosen revocation at once with a forgery of its ID.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `5ce984d` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=C-preempt` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 1,000 (10 % publish), static-bootstrap graph |
| preempters | 10, each linked to 100 random honest nodes and to the 3 revokers; they forward everything else |
| chosen revocations | 3 per seed, at 60, 90 and 120 s, each by a different random publisher of a verdict it published 20 s before |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 180 s; publishing stops at 150 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 0.8510 [0.8487, 0.8533] |
| Latency p50 (ms) | 1178.1 [1117.4, 1238.8] |
| Latency p99 (ms) | 4116.6 [4056.2, 4177.0] |
| Latency max (ms) | 5480.1 [5357.8, 5602.5] |
| Duplicate factor (copies per delivered verdict) | 1.23 [1.22, 1.24] |
| Hop count, mean | 4.82 [4.72, 4.91] |
| Mesh degree at the end | 0.29 [0.28, 0.30] |
| Delivery ratio, chosen revocations | 0.2040 [0.1868, 0.2213] |
| Hop count predicted, ln N / ln(D−1) | 4.29 (N 1000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0043 [0.0032, 0.0053] |
| 2 | 0.1241 [0.1212, 0.1270] |
| 3 | 0.1036 [0.0945, 0.1127] |
| 4 | 0.2860 [0.2727, 0.2993] |
| 5 | 0.1495 [0.1402, 0.1587] |
| 6 | 0.1619 [0.1538, 0.1700] |
| 7 | 0.0736 [0.0670, 0.0801] |
| 8 | 0.0474 [0.0433, 0.0515] |
| 9 | 0.0242 [0.0201, 0.0284] |
| 10 | 0.0123 [0.0097, 0.0148] |
| 11 | 0.0076 [0.0048, 0.0104] |
| 12+ | 0.0057 [0.0033, 0.0081] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.1334 [0.1313, 0.1355] |
| `preempted` | 0.0156 [0.0148, 0.0164] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 1000 | 10 | 3042 | 124 | 77 |
