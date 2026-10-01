# Routing simulation: A-coldboot

The Sybils connect as the honest nodes start, before the honest links come up (1–5 s), and drop everything.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `405e24c` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=A-coldboot` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 1,000 (10 % publish); each dials 20 random honest nodes (about 40 links each), as in the paper's testbed |
| Sybils | 4,000, 20 links each to random honest nodes, connect at 0 s |
| attack | Sybils drop everything from 0 s |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 240 s; publishing stops at 210 s |

## Variants

- `plain`: the paper's plain GossipSub: its mesh parameters, no peer scoring, no flood publishing, no outbound quota, gossip to D_lazy peers only, no connection manager.
- `paper`: the paper's GossipSub v1.1: its mesh parameters and score function, flood publishing, opportunistic grafting, no connection manager.
- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `plain` | `paper` | `v0.1` |
|---|---|---|---|
| Delivery ratio | 0.9998 [0.9998, 0.9998] | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] |
| Latency p50 (ms) | 5193.4 [5176.9, 5209.9] | 86.7 [84.9, 88.5] | 89.9 [87.9, 91.8] |
| Latency p99 (ms) | 7783.0 [7744.6, 7821.4] | 230.5 [228.0, 233.1] | 227.3 [224.5, 230.1] |
| Latency max (ms) | 10981.9 [10733.3, 11230.4] | 1259.7 [1229.1, 1290.4] | 1280.6 [1227.8, 1333.3] |
| Duplicate factor (copies per delivered verdict) | 1.33 [1.33, 1.34] | 3.20 [3.19, 3.21] | 3.15 [3.14, 3.17] |
| Hop count, mean | 4.28 [4.27, 4.28] | 4.84 [4.80, 4.88] | 5.01 [4.97, 5.04] |
| Sybil share of mesh slots | 1.0000 [1.0000, 1.0000] | 0.5924 [0.5915, 0.5933] | 0.5463 [0.5452, 0.5474] |
| Mesh recovery time (s), seeds that recovered | – | – | – |
| Seeds whose mesh recovered (share) | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |
| Mesh degree at the end | 9.42 [9.40, 9.44] | 9.38 [9.36, 9.40] | 8.37 [8.35, 8.40] |
| Hop count predicted, ln N / ln(D−1) | 3.55 (N 1000, D 8) | 3.55 (N 1000, D 8) | 4.29 (N 1000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `plain` | `paper` | `v0.1` |
|---|---|---|---|
| 1 | 0.0080 [0.0079, 0.0081] | 0.0383 [0.0380, 0.0386] | 0.0381 [0.0379, 0.0383] |
| 2 | 0.0513 [0.0509, 0.0517] | 0.0961 [0.0954, 0.0969] | 0.0942 [0.0933, 0.0951] |
| 3 | 0.1832 [0.1821, 0.1843] | 0.1562 [0.1542, 0.1581] | 0.1470 [0.1455, 0.1486] |
| 4 | 0.3267 [0.3256, 0.3279] | 0.1936 [0.1906, 0.1967] | 0.1773 [0.1751, 0.1796] |
| 5 | 0.2929 [0.2921, 0.2937] | 0.1822 [0.1797, 0.1848] | 0.1713 [0.1692, 0.1734] |
| 6 | 0.1203 [0.1188, 0.1219] | 0.1355 [0.1346, 0.1364] | 0.1383 [0.1371, 0.1396] |
| 7 | 0.0169 [0.0164, 0.0175] | 0.0862 [0.0846, 0.0879] | 0.0966 [0.0956, 0.0976] |
| 8 | 0.0006 [0.0005, 0.0006] | 0.0499 [0.0482, 0.0517] | 0.0605 [0.0592, 0.0618] |
| 9 | 0.0000 [0.0000, 0.0000] | 0.0273 [0.0259, 0.0287] | 0.0350 [0.0335, 0.0364] |
| 10 | 0.0000 [0.0000, 0.0000] | 0.0150 [0.0139, 0.0161] | 0.0192 [0.0180, 0.0204] |
| 11 | 0.0000 [0.0000, 0.0000] | 0.0082 [0.0075, 0.0090] | 0.0101 [0.0093, 0.0110] |
| 12+ | 0.0000 [0.0000, 0.0000] | 0.0113 [0.0097, 0.0128] | 0.0123 [0.0106, 0.0139] |
| unknown | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `plain` | `paper` | `v0.1` |
|---|---|---|---|
| `never_received` | 0.0002 [0.0002, 0.0002] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |

## Checks

| Check | Result | Measured |
|---|---|---|
| plain GossipSub loses verdicts (measurable loss) | pass | delivery ratio 0.9998 [0.9998, 0.9998], highest seed 0.999829 |
| the paper's scoring loses none | pass | delivery ratio 1.0000 [1.0000, 1.0000], lowest seed 1.000000 |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `plain` | 20 | 1000 | 4000 | 99806 | 174 | 154 |
| `paper` | 20 | 1000 | 4000 | 99806 | 174 | 195 |
| `v0.1` | 20 | 1000 | 4000 | 99806 | 174 | 218 |
