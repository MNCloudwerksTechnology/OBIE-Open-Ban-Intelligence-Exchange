# Routing simulation: B-f50

10,000 honest nodes and 10000 adversaries (f = 0.5) on one random 20-regular graph; the adversaries drop everything.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `cd3b43f` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=B-f50` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 10,000 (10 % publish) |
| adversaries | 10000 (f = 0.5 of all nodes), same graph, GRAFT every peer, drop everything from the start |
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
| Latency p50 (ms) | 245.1 [235.0, 255.2] |
| Latency p99 (ms) | 2812.3 [2742.4, 2882.1] |
| Latency max (ms) | 5583.8 [5409.3, 5758.3] |
| Duplicate factor (copies per delivered verdict) | 1.73 [1.71, 1.75] |
| Hop count, mean | 15.12 [14.62, 15.62] |
| Sybil share of mesh slots | 0.7484 [0.7458, 0.7509] |
| Mesh recovery time (s), seeds that recovered | – |
| Seeds whose mesh recovered (share) | 0.0000 [0.0000, 0.0000] |
| Mesh degree at the end | 7.92 [7.91, 7.93] |
| Hop count predicted, ln N / ln(D−1) | 5.72 (N 10000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0010 [0.0010, 0.0010] |
| 2 | 0.0031 [0.0030, 0.0032] |
| 3 | 0.0067 [0.0064, 0.0071] |
| 4 | 0.0113 [0.0105, 0.0121] |
| 5 | 0.0166 [0.0154, 0.0179] |
| 6 | 0.0230 [0.0212, 0.0248] |
| 7 | 0.0307 [0.0283, 0.0332] |
| 8 | 0.0396 [0.0365, 0.0428] |
| 9 | 0.0492 [0.0455, 0.0530] |
| 10 | 0.0587 [0.0547, 0.0627] |
| 11 | 0.0671 [0.0631, 0.0710] |
| 12+ | 0.6929 [0.6721, 0.7137] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `v0.1` |
|---|---|
| `never_received` | 0.0000 [0.0000, 0.0000] |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 10000 | 10000 | 150042 | 92 | 445 |
