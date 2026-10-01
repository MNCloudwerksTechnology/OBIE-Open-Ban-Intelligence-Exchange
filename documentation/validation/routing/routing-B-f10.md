# Routing simulation: B-f10

10,000 honest nodes and 1111 adversaries (f = 0.1) on one random 20-regular graph; the adversaries drop everything.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `5ce984d`, `97dfecb` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=B-f10` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 10,000 (10 % publish) |
| adversaries | 1111 (f = 0.1 of all nodes), same graph, GRAFT every peer, drop everything from the start |
| graph | random 20-regular over all nodes; links between adversaries are left out |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 150 s; publishing stops at 120 s |

## Variants

- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `v0.1` |
|---|---|
| Delivery ratio | 1.0000 [1.0000, 1.0000] |
| Latency p50 (ms) | 75.3 [70.9, 79.6] |
| Latency p99 (ms) | 185.7 [184.2, 187.1] |
| Latency max (ms) | 1374.3 [1284.0, 1464.6] |
| Duplicate factor (copies per delivered verdict) | 4.68 [4.67, 4.69] |
| Hop count, mean | 6.66 [6.62, 6.69] |
| Sybil share of mesh slots | 0.2552 [0.2545, 0.2558] |
| Mesh recovery time (s), seeds that recovered | – |
| Seeds whose mesh recovered (share) | 0.0000 [0.0000, 0.0000] |
| Mesh degree at the end | 7.68 [7.67, 7.69] |
| Hop count predicted, ln N / ln(D−1) | 5.72 (N 10000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0018 [0.0018, 0.0018] |
| 2 | 0.0070 [0.0070, 0.0071] |
| 3 | 0.0244 [0.0242, 0.0247] |
| 4 | 0.0701 [0.0692, 0.0710] |
| 5 | 0.1620 [0.1598, 0.1641] |
| 6 | 0.2551 [0.2525, 0.2576] |
| 7 | 0.2298 [0.2279, 0.2317] |
| 8 | 0.1262 [0.1242, 0.1282] |
| 9 | 0.0556 [0.0540, 0.0573] |
| 10 | 0.0243 [0.0229, 0.0256] |
| 11 | 0.0129 [0.0120, 0.0139] |
| 12+ | 0.0307 [0.0285, 0.0329] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

No verdict was lost.

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 10000 | 1111 | 110009 | 90 | 458 |
