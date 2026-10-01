# Routing simulation: harness validation

Before the [v0.1 routing baseline](README.md) could be published, the
harness had to show that it measures what it claims to measure. Work
package [#1765](https://openproject.niew.dev/work_packages/1765) states the
test: rerun scenario A of Vyzovitis et al. 2020 (arXiv:2007.02754), the
GossipSub v1.1 paper.

- **Peer scoring off:** plain GossipSub must show measurable loss. The
  paper reports about 4–10 %.
- **The paper's scoring:** no verdict may be lost.
- **If either result differs,** the harness is investigated before any
  baseline is published.

[ADR 0035](../../adr/0035-routing-simulation-in-virtual-time.md) defines
the harness, the router variants `plain`, `paper` and `v0.1`, and the
scenarios. The reports this page cites are
[A-eclipse](routing-A-eclipse.md), [A-coldboot](routing-A-coldboot.md) and
[A-covertflash](routing-A-covertflash.md).

## Result

Both criteria hold in every seed. `plain` lost verdicts in each of its 60
runs, and `paper` lost none in any of its 60 runs. `v0.1` lost none either.

Scenario A has 1,000 honest nodes, 10 % of which publish, and 4,000
Sybils. Each value is the mean over 20 seeds, with its 95 % confidence
interval (Student's t) in brackets. "Lost" counts the pairs of a verdict
and an honest node where the node never accepted the verdict.

| Attack | Paper, plain GossipSub | Harness, `plain` | Paper, GossipSub v1.1 | Harness, `paper` | Harness, `v0.1` |
|---|---|---|---|---|---|
| Network-wide eclipse | about 10 % lost; latencies up to 50 s (§8.1) | 15.4 % lost [14.4, 16.4], 10.9–19.3 % per seed; p99 25.8 s, max 41.4 s | none lost; max 178 ms | none lost; p99 173 ms, max 1.3 s | none lost; p99 172 ms |
| Cold boot | about 4 % lost; p99 10 s (§8.2) | 0.022 % lost, 28–50 pairs per seed; p99 7.8 s, max 11.0 s | none lost | none lost; p99 231 ms | none lost; p99 227 ms |
| Covert flash | losses and delays over 15 s once the attack starts, no figure (§8.3) | 0.013 % lost, 20–44 pairs per seed; p99 7.5 s, max 11.2 s | none lost | none lost; p99 188 ms | none lost; p99 218 ms |

The scored variants' latencies are higher than the paper's 178 ms because
the harness draws link latencies from a geographic matrix, up to about
150 ms one way, while the paper's testbed used 25 ms ± 10 %.

## What differs, and why

Plain GossipSub's eclipse loss is half again the paper's. In cold boot and
covert flash, plain GossipSub loses a few dozen pairs per run rather than
4 %, although its latencies match the paper's: a p99 of 7–8 s against
10 s. The harness was investigated before the baseline runs, on
2026-09-30, and changed as a result (commit `405e24c`).

### The paper leaves the honest degree open

The paper gives honest nodes "20 connections" and Sybils 100. Its test
plan (`libp2p/gossipsub-hardening`, `RandomHonestTopology`) has every
honest node dial 20 random honest nodes. Each node then also accepts
about 20 dials, so it has about 40 honest links, not 20. The first version
of the harness built a random 20-regular graph instead.

The two readings, and the gossip fan-out D_lazy, move plain GossipSub's
loss by an order of magnitude:

| Honest links | D_lazy | Eclipse, lost | Cold boot, lost |
|---|---|---|---|
| 20-regular graph | 6 | 47 % (seed 1) | 2.7 % [2.3, 3.0], p99 14.7 s, max 22.2 s (13 seeds) |
| 20-regular graph | 8 | 51 % (seed 1) | not run |
| testbed: each dials 20 | 6 | 28 % (seed 1) | 0.16 % (seed 1) |
| testbed: each dials 20 (**the baseline**) | 8 | 15.4 % (20 seeds) | 0.022 % (20 seeds) |

The single-seed values are indicative only. Two runs of one seed differ
(ADR 0035, *What a seed fixes*), and the eclipse loss most: seed 1 lost
13.4 % in the investigation and 18.0 % in the baseline. The cold boot runs
on the 20-regular graph came from commit `66d6d6e`. That commit had the
same Sybils, traffic and metrics as the baseline, and it ran 13 of the 20
seeds before the topology changed.

The topology works the same way in both attacks. Without scoring, the
Sybils' GRAFTs fill most mesh slots: 91 % in the eclipse and all of them
in cold boot. Verdicts then spread mostly by gossip. A node announces an
event to D_lazy random peers outside its mesh, and only the honest ones
among them pass it on. The share of honest peers decides how often gossip
reaches a node in time. In the eclipse it is 40 of about 440 peers; in
cold boot, 40 of about 120.

No single setting matches both of the paper's figures. The 20-regular
graph comes close to its cold boot loss but loses half of all verdicts in
the eclipse. The testbed graph comes close to its eclipse loss but loses
almost nothing in cold boot. The baseline uses the testbed graph, because
it is the paper's own test plan, and D_lazy 8, because the paper's plain
nodes (`honest_vanilla.go`) set only D, D_lo and D_hi. The 2020 library
gossiped to D peers, which is 8 there (go-libp2p-pubsub v0.2.7).

### What else differs from the paper's testbed

None of these was measured on its own. They are why the remaining
difference in cold boot is not taken as a defect of the harness:

- **Traffic.** The paper sent 120 messages/s of 2 KiB to containers with
  1.2 vCPUs. The harness sends 1 verdict/s of about 1 KB, which processing
  never delays (ADR 0035, deviations 1 and 3). A rate of 120/s is not what
  OBIE carries. On 100 nodes, OBIE's per-publisher limit dropped 15 % of
  it. With the limits off, 300 virtual seconds took more than 9 minutes,
  too slow for 1,000 nodes and 20 seeds.
- **Library version.** go-libp2p-pubsub v0.17 always applies some v1.1
  hardening: the GRAFT and PRUNE backoff, refusal of an inbound GRAFT at
  D_hi, and IDONTWANT. The paper's plain GossipSub had none of them.
- **Cold boot timing.** The paper's honest nodes start 2 min after the
  Sybils. Here they start at 0 s, after the Sybils' links are made.

### Conclusion

The harness reproduces the paper's result in every seed: without scoring,
the Sybils take the mesh and verdicts are lost or arrive seconds late;
with the paper's scoring, no verdict is lost. How much plain GossipSub
loses depends on the honest degree, the gossip fan-out and the workload,
and the paper does not pin these down. With the paper's own topology, the
eclipse loss is close to the paper's. Cold boot loses less than in the
paper, but its latencies match. The remaining difference lies in the
workload and the library version, not in how the harness measures, so
the baseline was published.

## Other checks of the harness

- **The metrics on hand-made traces.** Unit tests in `test/sim` feed
  constructed traces to the hop counts, delivery, latencies, loss causes,
  retention and mesh shares, and check the results by hand.
- **Hop counts against WP-1764's trace files.** `TestHopsMatchTheEventTrace`
  lets every node of a small network write its trace file
  (`mesh.trace_path`, ADR 0032), joins the files with `internal/eventtrace`,
  and requires the harness's hop count and copy count for every event at
  every node.
- **The reduced scenario repeats.** CI runs `reduced`: 100 honest nodes on
  a 20-regular graph and 400 cold boot Sybils, one seed per variant. Twenty
  repeats of seed 1 all passed its checks. `plain` delivered 0.987–0.998,
  and `paper` and `v0.1` delivered everything in every repeat. After
  develop was merged, which brought WP-1764's instruments into the node,
  one more run gave 0.9957 for `plain` and 1.0 for the others, in 10 s.
- **Two independent reviews** of the harness, one before the baseline
  runs and one during them, found nothing wrong in scenario A. They found
  defects in the C scenarios, among them two in the metrics: retention
  counted the publisher, whose store never evicts its own verdicts, and a
  copy dropped from a full validation queue was taken as the cause of a
  loss. These were fixed, and the C scenarios were run again.
