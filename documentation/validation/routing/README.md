# Routing simulation: the v0.1 baseline

This is how OBIE v0.1 routes verdicts on 10³–10⁴ nodes, with and without
hostile peers. Every routing work package of epic #1761 reruns these
scenarios on its branch and compares the results with this baseline: the
work packages #1767, #1768, #1770, #1771 and #1772.
[ADR 0035](../../adr/0035-routing-simulation-in-virtual-time.md) defines
the harness, the scenarios and every metric. The
[harness validation](harness-validation.md) shows that the harness
reproduces the GossipSub v1.1 paper.

| Baseline | |
|---|---|
| Harness | `make sim-routing SCENARIO=all` (test/sim, ADR 0035) |
| Runs | 440: 22 configurations of a scenario and a router variant, 20 seeds each (seeds 1–20) |
| OBIE | the runs ran at `405e24c` (141), `5ce984d` (166), `97dfecb` (4), `cd3b43f` (89) and `e463f47` (40) |
| go-libp2p-pubsub, go-libp2p | `v0.17.0`, `v0.50.0` |
| Deferred runs | none |

The commits between `405e24c` and `e463f47` fixed the C scenarios, the
report and a deadlock at shutdown, and made T-burst-regular smaller. Every
run that one of them changed was run again. WP-1764's instruments were
merged into the node after the runs (`e857173`). They change no routing
decision, and the reduced scenario gives the same results with them (see
the harness validation).

Each scenario has three files: a Markdown report, a summary CSV and a CSV
of every seed. A value is the mean over the seeds, with its 95 %
confidence interval (Student's t) in brackets. The delivery ratio counts
the pairs of a verdict and an honest node that runs at the end of the
run.

## Reports

| Scenario | What it runs | Report |
|---|---|---|
| A-eclipse | 1,000 honest nodes on the paper's testbed; 4,000 Sybils with 100 links each connect at 60 s and drop everything | [md](routing-A-eclipse.md), [csv](routing-A-eclipse.csv), [seeds](routing-A-eclipse-seeds.csv) |
| A-coldboot | as A-eclipse, but the Sybils, 20 links each, connect before the honest links come up | [md](routing-A-coldboot.md), [csv](routing-A-coldboot.csv), [seeds](routing-A-coldboot-seeds.csv) |
| A-covertflash | as A-coldboot, but the Sybils forward until 120 s | [md](routing-A-covertflash.md), [csv](routing-A-covertflash.csv), [seeds](routing-A-covertflash-seeds.csv) |
| B-f10, B-f30, B-f50 | 10,000 honest nodes and adversaries making up 10, 30 or 50 % of all nodes on one random 20-regular graph | [f10](routing-B-f10.md), [f30](routing-B-f30.md), [f50](routing-B-f50.md) |
| C-flood | 200 nodes with the real store; weight-0 junk at 100/s for 60 s | [md](routing-C-flood.md) |
| C-junk | 300 nodes; two junk hosts on one hub, each at the per-peer rate limit | [md](routing-C-junk.md) |
| C-preempt | 1,000 nodes; 10 preempters forge the IDs of three chosen revocations | [md](routing-C-preempt.md) |
| C-offline | 300 nodes; 10 of them offline for 1 h, then rejoining | [md](routing-C-offline.md) |
| C-bootkill | 1,000 nodes; every bootstrap hub stops at 10 min | [md](routing-C-bootkill.md) |
| T-static, T-regular | 1,000 nodes without attack, on the static-bootstrap graph or a random 20-regular graph | [static](routing-T-static.md), [regular](routing-T-regular.md) |
| T-lowrate | T-static at 0.1 verdicts/s | [md](routing-T-lowrate.md) |
| T-burst-static, T-burst-regular | a scanning burst of 200 verdicts/s for 60 s, on 300 and 100 nodes | [static](routing-T-burst-static.md), [regular](routing-T-burst-regular.md) |

The C and T reports link their CSVs the same way:
`routing-<scenario>.csv` and `routing-<scenario>-seeds.csv`.

## Findings

### Today's static-bootstrap graph loses a sixth of all verdicts without any attack

On the graph v0.1 builds from `mesh.bootstrap`, a node receives 84 % of
all verdicts (T-static 0.843 [0.842, 0.844], T-lowrate 0.838). The rest
never arrive, and the median verdict takes 1.3 s. On a random 20-regular
graph, the same code delivers every verdict, with a median of 65 ms and
a p99 of 166 ms (T-regular).

The numbers point to the shape of the graph. Of 1,000 nodes, 20 are hubs
with about 100 links each. Every other node links only to its two hubs,
and nodes that share a hub are not connected. A hub keeps at most D_hi =
12 peers in its GossipSub mesh, so it forwards a verdict at once to only
a few of its leaves. The others can learn of it only from the gossip
announcements the hubs send for 3 s, to a fraction of their peers.
Mesh slots per node at the end of a run average 0.24 on this graph and
7.6 on the regular one. Each copy is new to the node that gets it
(duplicate factor 1.1, against 6.6), and the loss is `never_received`.

Every scenario on the static graph inherits this ceiling. Before or apart
from their attacks, C-bootkill, C-junk, C-offline and C-preempt deliver
83–87 % of the verdicts, and C-flood's 200 nodes 91 %.

### v0.1 resists the paper's Sybil attacks

In all three attacks of scenario A, v0.1 loses no verdict, as the
paper's scoring does not, with a p99 of 172–227 ms. Its mesh never
recovers by the harness's measure: the Sybils hold 47–58 % of the honest
nodes' mesh slots until the end of the run, never 10 % or less. Without
scoring, plain GossipSub gives the Sybils 91–100 % of the slots and loses
15 % of the verdicts in the eclipse.

### More adversaries make paths longer, not lossy

On 10,000 honest nodes, v0.1 delivers every verdict at f = 0.1 and 0.3.
At f = 0.5, 19 of 20 seeds lose a few pairs, 0.0008 % of all. The cost
is time: the p99 grows from 186 ms to 996 ms and 2.8 s, the mean path
from 6.7 to 9.6 and 15.1 hops, and the adversaries hold 26, 65 and 75 %
of the mesh slots.

### OBIE's own attacks

- **A flood of weight-0 junk evicts every trusted verdict** (C-flood).
  When the store is full, it evicts the record that expires first. The
  trusted verdicts live 7 days and the junk 30, so the junk wins: the
  nodes that had accepted the trusted verdicts kept 0 % of them. While
  the flood runs, delivery falls from 0.91 to 0.65, and rate limits drop
  8 % of all pairs.
- **Junk at the per-peer rate through one hub** (C-junk) lowers delivery
  from 0.84 to 0.62 while it lasts. The hub relays the junk to its
  leaves, whose per-peer bucket for the hub then also drops honest
  verdicts: 13 % of all pairs are `rate_limited`.
- **Forged message IDs suppress a chosen revocation** (C-preempt). The
  three chosen revocations per seed reach 20 % of the nodes [19, 22]. A
  node that gets the forgery first remembers its ID and ignores the
  genuine revocation.
- **A node offline for an hour catches up** (C-offline): after rejoining,
  the ten nodes accepted 93 % of the new verdicts. Their mesh was back
  within 0.9 s in 17 of 20 seeds. They missed everything published while
  they were away; v0.1 does not resend it.
- **Without the bootstrap hubs, nothing is delivered** (C-bootkill). Once
  every hub stops at 10 min, no verdict reaches any other node again: a
  node knows no peer but its hubs.

### Bursts hit the per-peer rate limit

A scanning campaign of 200 verdicts/s for 60 s overruns the per-peer
limit of 50/s (`mesh.rate_limit.peer`). On the static graph a leaf
receives everything from its two hubs, so 40 % of all pairs are
`rate_limited`, and delivery falls to 0.39 (T-burst-static, 300 nodes).
On a 20-regular graph the copies come from many peers: 8 % of the pairs
are `rate_limited`, and delivery is 0.92 (T-burst-regular, 100 nodes).

## Hop counts

The hop count of a verdict at a node is the length of the path its
accepted copy took from the publisher, joined from the per-event trace.
ln N / ln(D−1) is the diameter of a random D-regular mesh of N nodes,
with v0.1's D = 6. On the random regular graph without attack, the mean
path is close to it. Attacks and the static graph lengthen it.

| Scenario (`v0.1`) | Honest nodes N | Mean hops | ln N / ln(D−1) |
|---|---|---|---|
| T-regular | 1,000 | 4.19 [4.16, 4.23] | 4.29 |
| T-static | 1,000 | 5.20 [5.01, 5.38] | 4.29 |
| T-lowrate | 1,000 | 4.80 [4.69, 4.91] | 4.29 |
| T-burst-regular | 100 | 2.36 [2.33, 2.39] | 2.86 |
| T-burst-static | 300 | 2.99 [2.94, 3.05] | 3.54 |
| A-eclipse | 1,000 | 4.00 [3.98, 4.02] | 4.29 |
| A-coldboot | 1,000 | 5.01 [4.97, 5.04] | 4.29 |
| A-covertflash | 1,000 | 4.68 [4.65, 4.72] | 4.29 |
| B-f10 | 10,000 | 6.66 [6.62, 6.69] | 5.72 |
| B-f30 | 10,000 | 9.59 [9.52, 9.65] | 5.72 |
| B-f50 | 10,000 | 15.1 [14.6, 15.6] | 5.72 |
| C-flood | 200 | 3.83 [3.74, 3.91] | 3.29 |
| C-junk | 300 | 3.09 [3.02, 3.15] | 3.54 |
| C-preempt | 1,000 | 4.82 [4.72, 4.91] | 4.29 |
| C-offline | 300 | 3.05 [2.96, 3.13] | 3.54 |
| C-bootkill | 1,000 | 4.73 [4.61, 4.85] | 4.29 |

Each report gives the distribution of the hop counts as well.

## What the baseline does not show

- **CPU, bandwidth and packet loss.** Processing takes no time, and links
  neither lose nor queue data (ADR 0035, *What is not modelled*). A burst
  that saturates a real node's CPU is not modelled.
- **Scaled scenarios.** C-flood shrinks `store.max_indicators` from
  1,000,000 to 2,000; at the default, the same flood would last about
  eight hours. T-burst-static runs 300 nodes and T-burst-regular 100: on a
  healthy mesh every node validates every verdict, and a seed of 300 nodes
  took about 17 minutes.
- **One IP address per Sybil.** The score's IP colocation penalty never
  fires, the strongest case for the attacker.

## Comparing a change with the baseline

Run the scenarios the change affects on its branch, with an empty cache,
and compare the new reports with these:

```sh
make sim-routing SCENARIO=T SIMOUT=/tmp/routing-after
make sim-routing SCENARIO=A-eclipse SIMPARALLEL=1 SIMOUT=/tmp/routing-after
```

An A-eclipse run needs about 40 GB and a B run up to 25 GB; the whole
baseline takes about ten hours on 32 cores. `SIMBUDGET` stops starting
runs after a while, and the same command resumes from the cache
([CONTRIBUTING](../../../CONTRIBUTING.md#routing-simulation)).
