# Routing simulation: A-covertflash

Like cold boot, but the Sybils forward until 120 s, then drop everything.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `405e24c` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=A-covertflash` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 1,000 (10 % publish); each dials 20 random honest nodes (about 40 links each), as in the paper's testbed |
| Sybils | 4,000, 20 links each to random honest nodes, connect at 0 s |
| attack | Sybils drop everything from 120 s |
| traffic | Poisson, 1 verdict/s network-wide, from 30 s |
| run | 300 s; publishing stops at 270 s |

## Variants

- `plain`: the paper's plain GossipSub: its mesh parameters, no peer scoring, no flood publishing, no outbound quota, gossip to D_lazy peers only, no connection manager.
- `paper`: the paper's GossipSub v1.1: its mesh parameters and score function, flood publishing, opportunistic grafting, no connection manager.
- `v0.1`: OBIE v0.1 as shipped: library mesh parameters, v0.1 peer scoring, flood publishing, connection manager 32/128.

## Results

Each cell is the mean over the seeds with its 95 % confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.

| Metric | `plain` | `paper` | `v0.1` |
|---|---|---|---|
| Delivery ratio | 0.9999 [0.9999, 0.9999] | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] |
| Latency p50 (ms) | 4128.6 [4024.8, 4232.5] | 68.4 [63.6, 73.2] | 80.0 [76.2, 83.8] |
| Latency p99 (ms) | 7462.3 [7402.2, 7522.5] | 188.0 [185.1, 190.9] | 217.7 [214.0, 221.4] |
| Latency max (ms) | 11217.4 [10967.7, 11467.1] | 1322.2 [1293.3, 1351.1] | 1283.8 [1262.0, 1305.6] |
| Duplicate factor (copies per delivered verdict) | 2.92 [2.85, 2.99] | 4.28 [4.22, 4.34] | 3.95 [3.91, 3.99] |
| Hop count, mean | 6.12 [6.04, 6.20] | 4.38 [4.35, 4.41] | 4.68 [4.65, 4.72] |
| Sybil share of mesh slots | 1.0000 [1.0000, 1.0000] | 0.6212 [0.6201, 0.6223] | 0.5795 [0.5784, 0.5806] |
| Mesh recovery time (s), seeds that recovered | – | – | – |
| Seeds whose mesh recovered (share) | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |
| Mesh degree at the end | 9.44 [9.43, 9.45] | 9.47 [9.45, 9.48] | 8.42 [8.39, 8.45] |
| Delivery ratio, before the attack | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] |
| Delivery ratio, during the attack | 0.9998 [0.9998, 0.9998] | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] |
| Hop count predicted, ln N / ln(D−1) | 3.55 (N 1000, D 8) | 3.55 (N 1000, D 8) | 4.29 (N 1000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `plain` | `paper` | `v0.1` |
|---|---|---|---|
| 1 | 0.0050 [0.0048, 0.0051] | 0.0376 [0.0373, 0.0378] | 0.0376 [0.0374, 0.0378] |
| 2 | 0.0377 [0.0370, 0.0384] | 0.1042 [0.1033, 0.1052] | 0.0993 [0.0985, 0.1002] |
| 3 | 0.1148 [0.1117, 0.1179] | 0.1905 [0.1881, 0.1929] | 0.1642 [0.1621, 0.1662] |
| 4 | 0.2360 [0.2321, 0.2400] | 0.2452 [0.2425, 0.2480] | 0.2069 [0.2042, 0.2097] |
| 5 | 0.1820 [0.1773, 0.1866] | 0.1950 [0.1933, 0.1967] | 0.1885 [0.1866, 0.1905] |
| 6 | 0.1589 [0.1551, 0.1626] | 0.1122 [0.1105, 0.1138] | 0.1326 [0.1313, 0.1340] |
| 7 | 0.0109 [0.0105, 0.0113] | 0.0575 [0.0559, 0.0590] | 0.0802 [0.0785, 0.0820] |
| 8 | 0.0810 [0.0764, 0.0856] | 0.0280 [0.0267, 0.0293] | 0.0443 [0.0427, 0.0460] |
| 9 | 0.0007 [0.0004, 0.0010] | 0.0135 [0.0126, 0.0144] | 0.0227 [0.0214, 0.0241] |
| 10 | 0.0665 [0.0623, 0.0707] | 0.0069 [0.0063, 0.0075] | 0.0113 [0.0104, 0.0122] |
| 11 | 0.0007 [0.0004, 0.0011] | 0.0038 [0.0034, 0.0041] | 0.0056 [0.0050, 0.0061] |
| 12+ | 0.1059 [0.1000, 0.1117] | 0.0057 [0.0048, 0.0066] | 0.0067 [0.0057, 0.0077] |
| unknown | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `plain` | `paper` | `v0.1` |
|---|---|---|---|
| `never_received` | 0.0001 [0.0001, 0.0001] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |

## Checks

| Check | Result | Measured |
|---|---|---|
| plain GossipSub loses verdicts (measurable loss) | pass | delivery ratio 0.9999 [0.9999, 0.9999], highest seed 0.999908 |
| the paper's scoring loses none | pass | delivery ratio 1.0000 [1.0000, 1.0000], lowest seed 1.000000 |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `plain` | 20 | 1000 | 4000 | 99806 | 233 | 318 |
| `paper` | 20 | 1000 | 4000 | 99806 | 233 | 412 |
| `v0.1` | 20 | 1000 | 4000 | 99806 | 233 | 408 |
