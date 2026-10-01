# Routing simulation: T-regular

No attack; 1 verdict/s on the random 20-regular graph.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `5ce984d`, `cd3b43f` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=T-regular` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 1000 (10 % publish), random 20-regular graph |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 180 s; publishing stops at 150 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 1.0000 [1.0000, 1.0000] |
| Latency p50 (ms) | 64.5 [59.2, 69.8] |
| Latency p99 (ms) | 165.6 [161.3, 169.9] |
| Latency max (ms) | 233.3 [225.1, 241.4] |
| Duplicate factor (copies per delivered verdict) | 6.61 [6.58, 6.64] |
| Hop count, mean | 4.19 [4.16, 4.23] |
| Mesh degree at the end | 7.58 [7.56, 7.61] |
| Hop count predicted, ln N / ln(D−1) | 4.29 (N 1000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0189 [0.0188, 0.0190] |
| 2 | 0.0741 [0.0730, 0.0752] |
| 3 | 0.2052 [0.2011, 0.2094] |
| 4 | 0.3280 [0.3228, 0.3332] |
| 5 | 0.2346 [0.2313, 0.2378] |
| 6 | 0.0897 [0.0853, 0.0940] |
| 7 | 0.0293 [0.0268, 0.0318] |
| 8 | 0.0103 [0.0089, 0.0117] |
| 9 | 0.0047 [0.0039, 0.0054] |
| 10 | 0.0024 [0.0020, 0.0029] |
| 11 | 0.0012 [0.0010, 0.0015] |
| 12+ | 0.0015 [0.0009, 0.0021] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

No verdict was lost.

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 1000 | 0 | 10000 | 121 | 138 |
