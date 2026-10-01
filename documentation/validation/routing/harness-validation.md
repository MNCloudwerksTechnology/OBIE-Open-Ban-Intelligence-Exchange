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
Sybils. The Sybils have 100 links each in the eclipse and 20 in cold boot
and covert flash, as in the paper's scenario files
(`network.attack_degree` in `scripts/configs/1k-attack-*.json` of
`libp2p/gossipsub-hardening`). Each harness value is the mean over 20
seeds, with its 95 % confidence interval (Student's t) in brackets. "Lost"
counts the pairs of a verdict and an honest node where the node never
accepted the verdict.

| Attack | Paper, plain GossipSub | Harness, `plain` | Paper, GossipSub v1.1 | Harness, `paper` | Harness, `v0.1` |
|---|---|---|---|---|---|
| Network-wide eclipse | about 10 % lost; latencies up to 50 s (§8.1) | 15.4 % lost [14.4, 16.4], 10.9–19.3 % per seed; p99 25.8 s, max 41.4 s | none lost; p99 141 ms, max 178 ms | none lost; p99 173 ms, max 1.3 s | none lost; p99 172 ms |
| Cold boot | about 4 % lost; p99 10 s, max 17 s (§8.2) | 0.022 % lost, 28–50 pairs per seed; p99 7.8 s, max 11.0 s | none lost; p99 205 ms, max 1.2 s | none lost; p99 231 ms, max 1.3 s | none lost; p99 227 ms |
| Covert flash | losses and delays over 15 s once the attack starts, no figure (§8.3) | 0.013 % lost, 20–44 pairs per seed; p99 7.5 s, max 11.2 s | p99 197 ms, max about 1 s | none lost; p99 188 ms, max 1.3 s | none lost; p99 218 ms |

The paper observed no loss for GossipSub v1.1 in any of its tests (§7).
The harness's p99 latencies for `paper` are close to the paper's. Its
maximum in the eclipse is not: 1.3 s against 178 ms. Link latency is not
the reason. The harness's links take up to about 180 ms one way, drawn
from a geographic matrix, where the paper's testbed used 25 ms ± 10 %; but
without an attack, on a regular graph, v0.1's maximum is 233 ms
(T-regular). The long tail comes with the attack, probably from verdicts
that reach a node only by gossip while Sybils hold its mesh slots. The
paper found its scored eclipse latencies identical to those without an
attack (§8.1).

## What differs, and why

Four results differ from the paper:

- **Eclipse.** `plain` loses about 1.5 times the paper's share, and the
  slowest verdict of `paper` takes 1.3 s rather than 178 ms (see above).
- **Cold boot and covert flash.** `plain` loses a few dozen pairs per run:
  in cold boot 0.022 %, about 180 times less than the paper's 4 %. Its
  latencies are of the same order as the paper's: a p99 of 7.8 s against
  10 s, and a maximum of 11 s against 17 s.
- **Mesh recovery.** The paper's scored GossipSub recovers the mesh
  within about 1.5 min in cold boot (§8.2) and returns it "to a healthy
  state" in covert flash (§8.3). The harness's `paper` variant never
  does: on average over the attack, the Sybils hold 59 % of the honest
  nodes' mesh slots in cold boot and 62 % in covert flash, and in none of
  these 40 runs does their share fall to 10 % or less and stay there. It
  loses no verdict all the same.
- **Sybils in the eclipse mesh.** In the paper's eclipse, Sybils hold 2–4
  slots of an honest node's mesh on average (§8.1). Here they hold 46 % of
  the slots on average, about 4 of the 9.25 a node has at the end of the
  run.

The harness was investigated before the baseline runs, on 2026-09-30, and
changed as a result (commit `405e24c`).

### The paper leaves the honest degree open

The paper lets Sybils have 100 connections each, "while honest nodes
only up to 20" (§7.3). Its test plan
(`RandomHonestTopology`) has every honest node dial 20 random honest
nodes. Each node then also accepts about 20 dials, so it has about 40
honest links, not 20. The first version of the harness built a random
20-regular graph instead.

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

The likely reason is how many of a node's peers are honest. Without
scoring, the Sybils' GRAFTs fill most mesh slots: 91 % in the eclipse and
all of them in cold boot. Verdicts then spread mostly by gossip. A node
announces an event to D_lazy random peers outside its mesh, and only the
honest ones among them pass it on. On the testbed graph, 40 of a node's
peers are honest: of about 440 in the eclipse, and of about 120 in cold
boot. On the 20-regular graph, 20 are.

No setting matches both of the paper's figures. The 20-regular graph comes
close to its cold boot loss but loses half of all verdicts in the eclipse.
The testbed graph comes close to its eclipse loss but loses almost nothing
in cold boot. The baseline uses the testbed graph, because it is the
paper's own test plan, and D_lazy 8, because the paper's plain nodes
(`honest_vanilla.go`) set only D, D_lo and D_hi. The 2020 library gossiped
to D peers, which is 8 there (go-libp2p-pubsub v0.2.7).

### OBIE's traffic is probably too slow for the paper's P3

A trace of one node's GRAFTs and PRUNEs suggests why the Sybils keep their
slots. It was taken on 2026-09-30, before the topology changed: a cold
boot run of `paper` on the 20-regular honest graph, with the harness's
code before `66d6d6e`. The node's honest mesh peers were pruned
when the score's P3 became active, 60 s after their GRAFT. P3 expects at
least 10 deliveries from a mesh peer, and at 1 verdict/s across the whole
network, a peer delivers fewer first copies in that minute. The prune then
charged them P3b, which lasts. The Sybils, by contrast, were mostly pruned
for oversubscription before their P3 became active, so they were never
charged. They grafted again 60–75 s later with a clean score. In 180 s,
72 of the 80 Sybils linked to that node held one of its mesh slots at
some time. The paper sent 120 messages/s, at which every honest mesh peer
meets P3's threshold easily. This was not measured on other nodes, seeds
or the testbed graph. It is at most part of the reason: v0.1 has no P3,
because verdicts are sparse (ADR 0009), and its mesh does not recover
either.

### What else differs from the paper's testbed

None of these was measured on its own:

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

In every seed, the harness shows the paper's qualitative result. Without
scoring, the Sybils take the mesh, and verdicts are lost or arrive seconds
late. With the paper's scoring, no verdict is lost, and the p99
latencies are close to the paper's. How much plain GossipSub loses depends on the
honest degree and the gossip fan-out, which the paper's text leaves open:
the two readings above differ by an order of magnitude. With the paper's
own topology, the eclipse loss is near the paper's, but cold boot loses
about 180 times less.

The rest of the gap, and the mesh that does not recover, may come from the
workload and the library version. That was not measured. The checks below
found no error in how the harness measures. On that basis, it was judged
fit to publish the baseline, with these differences stated.

## Other checks of the harness

- **The metrics on hand-made traces.** Unit tests in `test/sim` feed
  constructed traces to the hop counts, delivery, latencies, loss causes,
  retention and mesh shares, and check the results by hand.
- **Hop counts against WP-1764's trace files.** `TestHopsMatchTheEventTrace`
  lets every honest node of a small network write its trace file
  (`mesh.trace_path`, ADR 0032), joins the files with `internal/eventtrace`,
  and requires the harness's hop count and copy count for every event at
  every honest node, in all three variants. With forwarding Sybils, the
  files give no path where the harness follows one through a Sybil, as
  expected.
- **The reduced scenario repeats.** CI runs `reduced`: 100 honest nodes on
  a 20-regular graph and 400 cold boot Sybils, one seed per variant. Twenty
  repeats of seed 1 all passed its checks. `plain` delivered 0.987–0.998,
  and `paper` and `v0.1` delivered everything in every repeat. After
  develop was merged, which brought WP-1764's instruments into the node,
  two more runs gave 0.9957 and 0.9969 for `plain` and 1.0 for the others,
  in about 10 s each.
- **Two independent reviews**, one before the baseline runs and one
  during them, found no defect in scenario A's results. They found defects
  in the C scenarios, among them two in the metrics: retention counted the
  publisher, whose store never evicts its own verdicts, and a copy dropped
  from a full validation queue was taken as the cause of a loss. These
  were fixed, and C-flood, C-junk and C-preempt, the scenarios the fixes
  changed, were run again.
