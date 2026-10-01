# Routing simulation: B-f30

10,000 honest nodes and 4286 adversaries (f = 0.3) on one random 20-regular graph; the adversaries drop everything.

| Report | |
|---|---|
| Format | `routing-report/1` |
| OBIE | `5ce984d`, `97dfecb`, `cd3b43f` |
| go-libp2p-pubsub | `v0.17.0` |
| go-libp2p | `v0.50.0` |
| Go | `go1.26.7` |
| Seeds | 1–20 per variant |
| Generated | 2026-10-01 |
| Harness | `make sim-routing SCENARIO=B-f30` (test/sim, ADR 0035) |

## Scenario

| Parameter | Value |
|---|---|
| honest nodes | 10,000 (10 % publish) |
| adversaries | 4286 (f = 0.3 of all nodes), same graph, GRAFT every peer, drop everything from the start |
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
| Latency p50 (ms) | 98.0 [94.3, 101.7] |
| Latency p99 (ms) | 996.3 [969.1, 1023.4] |
| Latency max (ms) | 2244.2 [2159.2, 2329.2] |
| Duplicate factor (copies per delivered verdict) | 2.16 [2.15, 2.18] |
| Hop count, mean | 9.59 [9.52, 9.65] |
| Sybil share of mesh slots | 0.6484 [0.6474, 0.6494] |
| Mesh recovery time (s), seeds that recovered | – |
| Seeds whose mesh recovered (share) | 0.0000 [0.0000, 0.0000] |
| Mesh degree at the end | 8.73 [8.72, 8.74] |
| Hop count predicted, ln N / ln(D−1) | 5.72 (N 10000, D 6) |

## Hop-count distribution

Share of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.

| Hops | `v0.1` |
|---|---|
| 1 | 0.0014 [0.0014, 0.0014] |
| 2 | 0.0036 [0.0035, 0.0036] |
| 3 | 0.0085 [0.0083, 0.0086] |
| 4 | 0.0193 [0.0189, 0.0197] |
| 5 | 0.0398 [0.0389, 0.0406] |
| 6 | 0.0708 [0.0694, 0.0723] |
| 7 | 0.1076 [0.1056, 0.1095] |
| 8 | 0.1395 [0.1372, 0.1417] |
| 9 | 0.1536 [0.1519, 0.1552] |
| 10 | 0.1409 [0.1395, 0.1423] |
| 11 | 0.1083 [0.1063, 0.1103] |
| 12+ | 0.2069 [0.1998, 0.2140] |
| unknown | 0.0000 [0.0000, 0.0000] |

## Loss by cause

Share of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.

No verdict was lost.

## Runs

A seed draws its own graph and traffic, so links and verdicts are means over the seeds.

| Variant | Seeds | Honest nodes | Adversaries | Links, mean | Verdicts, mean | Wall time per seed (s), mean |
|---|---|---|---|---|---|---|
| `v0.1` | 20 | 10000 | 4286 | 130006 | 89 | 466 |
