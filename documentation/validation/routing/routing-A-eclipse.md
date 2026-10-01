# Routing simulation: A-eclipse

A warm network; at 60 s the Sybils connect and drop everything.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `5ce984d`, `97dfecb`, `cd3b43f`, `e463f47` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=A-eclipse` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 1,000 (10 % publish); each dials 20 random honest nodes (about 40 links each), as in the paper's testbed |
| Sybils | 4,000, 100 links each to random honest nodes, connect at 60 s |
| attack | Sybils drop everything from 60 s |
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
| Delivery ratio | 0.8460 [0.8356, 0.8564] | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] |
| Latency p50 (ms) | 1650.8 [1155.7, 2145.9] | 57.6 [54.1, 61.0] | 60.4 [56.4, 64.4] |
| Latency p99 (ms) | 25750.5 [25197.2, 26303.8] | 173.1 [171.1, 175.1] | 171.7 [169.9, 173.5] |
| Latency max (ms) | 41413.3 [39985.2, 42841.5] | 1271.2 [1246.1, 1296.2] | 1171.9 [1129.6, 1214.2] |
| Duplicate factor (copies per delivered verdict) | 2.38 [2.33, 2.42] | 4.68 [4.63, 4.72] | 3.85 [3.82, 3.89] |
| Hop count, mean | 10.53 [10.28, 10.78] | 3.62 [3.60, 3.64] | 4.00 [3.98, 4.02] |
| Sybil share of mesh slots | 0.9069 [0.9061, 0.9078] | 0.4575 [0.4533, 0.4618] | 0.4683 [0.4666, 0.4700] |
| Mesh recovery time (s), seeds that recovered | – | – | – |
| Seeds whose mesh recovered (share) | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |
| Mesh degree at the end | 9.25 [9.23, 9.27] | 9.25 [9.23, 9.27] | 8.31 [8.28, 8.33] |
| Delivery ratio, before the attack | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] |
| Delivery ratio, during the attack | 0.8160 [0.8040, 0.8281] | 1.0000 [1.0000, 1.0000] | 1.0000 [1.0000, 1.0000] |
| Hop count predicted, ln N / ln(D−1) | 3.55 (N 1000, D 8) | 3.55 (N 1000, D 8) | 4.29 (N 1000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `plain` | `paper` | `v0.1` |
|---|---|---|---|
| 1 | 0.0038 [0.0038, 0.0039] | 0.0369 [0.0366, 0.0372] | 0.0372 [0.0370, 0.0374] |
| 2 | 0.0128 [0.0126, 0.0131] | 0.1307 [0.1293, 0.1321] | 0.1121 [0.1112, 0.1130] |
| 3 | 0.0397 [0.0388, 0.0406] | 0.2982 [0.2942, 0.3023] | 0.2254 [0.2231, 0.2278] |
| 4 | 0.0795 [0.0772, 0.0817] | 0.3256 [0.3228, 0.3285] | 0.2770 [0.2747, 0.2793] |
| 5 | 0.0873 [0.0845, 0.0900] | 0.1485 [0.1448, 0.1523] | 0.2029 [0.2016, 0.2042] |
| 6 | 0.0703 [0.0682, 0.0724] | 0.0429 [0.0403, 0.0454] | 0.0967 [0.0947, 0.0988] |
| 7 | 0.0708 [0.0689, 0.0727] | 0.0122 [0.0110, 0.0134] | 0.0339 [0.0325, 0.0354] |
| 8 | 0.0808 [0.0784, 0.0832] | 0.0035 [0.0030, 0.0040] | 0.0104 [0.0096, 0.0112] |
| 9 | 0.0840 [0.0816, 0.0864] | 0.0010 [0.0008, 0.0012] | 0.0030 [0.0026, 0.0033] |
| 10 | 0.0774 [0.0751, 0.0796] | 0.0003 [0.0002, 0.0004] | 0.0009 [0.0007, 0.0010] |
| 11 | 0.0644 [0.0625, 0.0663] | 0.0001 [0.0001, 0.0002] | 0.0003 [0.0002, 0.0003] |
| 12+ | 0.3292 [0.3168, 0.3417] | 0.0001 [0.0000, 0.0001] | 0.0002 [0.0001, 0.0002] |
| unknown | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

| Cause | `plain` | `paper` | `v0.1` |
|---|---|---|---|
| `never_received` | 0.1540 [0.1436, 0.1644] | 0.0000 [0.0000, 0.0000] | 0.0000 [0.0000, 0.0000] |

## Checks

| Check | Result | Measured |
|---|---|---|
| plain GossipSub loses verdicts (measurable loss) | pass | delivery ratio 0.8460 [0.8356, 0.8564], highest seed 0.890864 |
| the paper's scoring loses none | pass | delivery ratio 1.0000 [1.0000, 1.0000], lowest seed 1.000000 |

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `plain` | 20 | 1000 | 4000 | 419806 | 174 | 207 |
| `paper` | 20 | 1000 | 4000 | 419806 | 174 | 322 |
| `v0.1` | 20 | 1000 | 4000 | 419806 | 174 | 386 |
