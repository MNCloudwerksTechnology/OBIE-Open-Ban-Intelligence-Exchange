# ADR 0033: A routing simulation of thousands of nodes in virtual time

- **Status:** Accepted
- **Date:** 2026-09-30
- **Work package:** [#1765](https://openproject.niew.dev/work_packages/1765)

## Context

No one can say how well OBIE's v0.1 routing spreads verdicts, or what it
loses under attack. The largest test runs four nodes on loopback
(ADR 0016), and the soak test runs three. The sibling work packages of
epic #1761 will change the mesh parameters, peer scoring, rate limits, storage
and connection admission. Each of them states a numeric target, so a
harness must measure the targets on 10³–10⁴ nodes with hostile peers. It
must also publish the baseline of v0.1 routing that every later change is
compared with.

The work package offers two approaches:

- **Shadow** runs the real binaries in discrete-event time.
- **An in-process libp2p mocknet** runs the node's code with test hooks.

The harness must also prove that it measures correctly. It reproduces the
result of Vyzovitis et al. 2020 (arXiv:2007.02754): plain GossipSub loses
messages under the paper's Sybil attacks, and scored GossipSub loses none.
The paper's own test plans are public (libp2p/gossipsub-hardening): the
attacker (`badboy.go`), the scenario files and the exact score parameters.

## Decision

### Approach: mocknet in virtual time

- **An in-process mocknet in virtual time.** Every run is one Go test bubble
  of `testing/synctest` (stable since Go 1.25). Inside the bubble the clock
  is virtual, and it advances only when every goroutine of the run waits
  for a timer or a channel. The run is a discrete-event simulation of the
  real code:
  - GossipSub heartbeats, backoffs, score decay and TTLs keep their real
    durations.
  - A one-hour outage costs only the CPU its events need.
  - The CPU never distorts a latency.
  - Hosts are go-libp2p `mocknet` peers. Their links carry a latency drawn
    from a geographic matrix. Streams deliver in order, after the link's
    latency.
  - The harness carries a copy of go-libp2p's mocknet
    (`test/sim/internal/mocknet`, MIT) with three changes. First, a write
    never waits for an earlier one to arrive, and no goroutine carries it:
    the reader waits for its arrival time. The original accepts a write of
    256 bytes or more only once the previous one arrived, so a link carried
    about two messages per latency, and one goroutine per stream end made
    up a third of a run's goroutines. Second, a stream opened while its
    connection closes is reset rather than left running. Third, no
    connection opens to or from a closed peer. The original opened one as
    long as the link remained, and nobody closed it. Its handshake then
    waited for bytes after the run's virtual clock had stopped, which
    failed the run.
- **Why not Shadow.**
  - *Fidelity.* Shadow would add the real TCP/QUIC stacks and the
    subsystems outside routing. It would add nothing to the routing
    decisions this work package measures. CPU time is free in Shadow's
    model too.
  - *Memory.* 5,000 `obied` processes at about 60 MB each (Go runtime,
    BadgerDB) need about 300 GB. The development machine has 124 GB.
  - *Tooling.* Shadow must be built from source (Rust and C), and Go
    programs under it have known caveats.
  - *CI cost.* No reduced scenario could build Shadow and run within CI's
    10 minutes.
- **Measured before deciding.** In the prototype, the mocknet ran 5,000
  real gossip nodes for 45 virtual seconds in 28 s of wall time, using
  7.5 GB. The paper's eclipse setup took 40 GB and 3.6 million goroutines
  (see *Size of the eclipse attack* below).

### What each node runs

- **An honest node runs the real routing code.** It runs `internal/mesh` —
  host, bootstrap redial with backoff and connection manager (32/128,
  bootstrap peers protected) — and `internal/gossip` in full:
  - message ID = event ID;
  - `StrictNoSign`;
  - the validator (`obieproto.Receive`, then `store.Seen`, then the
    per-publisher and per-peer token buckets);
  - peer scoring, flood publishing and the validation queue.
- **The node's store.** It is the real BadgerDB store (`store.NewMemory`)
  where eviction is measured (C-flood). Elsewhere it is a small in-memory
  store with the same `Seen` and `Put` rules: 10,000 BadgerDB instances do
  not fit in memory, and routing only asks the store whether it saw an
  event.
- **Subsystems that are not run.** The decision engine, enforcement, admin
  API, console and ops server take no part in routing. They also listen on
  real sockets, which would stop the virtual clock. The mesh pings its
  peers only once a day: pings only estimate latency for the admin API,
  and every peer pinged every 15 s cost a tenth of an eclipse run's CPU.
  This is where the simulation's fidelity ends. The daemon's hooks (ADR 0016) do not reach
  far enough, so two new hooks follow its rule ("test hooks, never
  configuration"). Production leaves both zero, and neither can be reached
  from the configuration file:
  - `mesh.Options.Testing.NewHost` builds the libp2p host from the node's
    key and its v0.1 connection manager. The simulation uses it to place
    the node on the mocknet.
  - `gossip.Options.Testing` holds three things:
    - `Router` replaces the GossipSub mesh parameters, peer scoring and
      flood publishing. Only the validation variants use it.
    - `Tracer` is a `pubsub.RawTracer`.
    - `Observe` reports the validator's outcome with the message ID.
    `mesh.Options.Testing.Gossip` passes these hooks through.
- **The adversary is the paper's own attacker, ported.** A Sybil speaks
  `/meshsub/1.1.0` directly (the paper's plan used 1.0.0):
  - It subscribes to and GRAFTs every peer that announces the topic.
  - After a PRUNE it re-GRAFTs after 1 min plus a random 0–15 s.
  - Before its attack starts it forwards to its mesh peers. Once the
    attack starts it drops everything (`sybil_degrade` 1.0).
  - It redials a peer that disconnects it, after 10 s. The paper's
    testbed never disconnected anyone, but v0.1's connection manager
    does.
  Two OBIE attackers use the same speaker:
  - The **junk flooder** injects valid verdicts signed by fresh keys. No
    node trusts those keys (trust weight 0).
  - The **preempter** answers a chosen event at once with a forged
    message carrying the same ID, and withholds the genuine one. This is
    the attack ADR 0009 describes.

### Router variants

| Variant | GossipSub parameters | Peer scoring | Other |
|---|---|---|---|
| `v0.1` | library defaults (D 6, D_lo 5, D_hi 12, D_lazy 6, D_out 2) | v0.1 (ADR 0009) | flood publish; connection manager 32/128 |
| `plain` | paper: D 8, D_lo 6, D_hi 12; D_lazy 8, D_out 0, gossip factor 0 | off | no flood publish; no connection manager |
| `paper` | paper: D 8, D_lo 6, D_hi 12, D_score 6, D_lazy 12, opportunistic graft every 60 heartbeats, outbound queue 128 | the paper's parameters (below) | flood publish; no connection manager |

`plain` is the paper's plain GossipSub, as far as go-libp2p-pubsub v0.17
allows. The paper's plain nodes set only D, D_lo and D_hi
(`honest_vanilla.go`). The library of 2020 gossiped to D peers outside
the mesh (go-libp2p-pubsub v0.2.7, `emitGossip`), so D_lazy is 8.
v0.17 applies some v1.1 hardening unconditionally:

- the GRAFT and PRUNE backoff;
- refusing an inbound GRAFT at D_hi;
- IDONTWANT.

The paper's score parameters come from its scenario files
(`1k-attack-*.json`):

- topic weight 0.25;
- P1: weight 0.0027, quantum 1 s, cap 3,600;
- P2: weight 0.664, decay 0.9916, cap 1,500;
- P3: weight −0.25, decay 0.997, cap 400, threshold 10, activation 1 min,
  window 5 ms;
- P3b: weight −0.25, decay 0.997;
- P4: weight −99, decay 0.9994;
- P5 and P6 off;
- decay interval 1 s, decay to zero 0.01, score retained 30 s;
- thresholds: gossip −4,000, publish −5,000, graylist −10,000, accept PX 0,
  opportunistic graft 0.

### Network, topologies and traffic

- **Latency.** Nodes are placed in eight regions:
  - Frankfurt 30 %
  - London 15 %
  - N. Virginia 20 %
  - Oregon 10 %
  - Tokyo 8 %
  - Singapore 10 %
  - São Paulo 4 %
  - Sydney 3 %

  A link's one-way latency is half the round-trip time between its
  regions (5 ms within a region), plus a random 0–10 % fixed per link. The
  round-trip times are rounded medians of public inter-region
  measurements. They are illustrative, not measured by this project.
- **Topologies.**
  - **Static bootstrap** (today's graph): k = N/50 hubs. Every other node
    lists 2 random hubs in `mesh.bootstrap`, and every hub lists 3 random
    other hubs. Nodes that share a hub are not connected to each other
    (ADR 0007).
  - **Random regular**: every node has degree 20. The dialer of each edge
    is chosen at random, and the edge is in the dialer's bootstrap list.
  - **The paper's testbed** (scenario A): every honest node dials 20
    random honest nodes (`RandomHonestTopology`), so it has about 40
    links.
- **Traffic.**
  - 10 % of the honest nodes publish.
  - Verdicts arrive as a Poisson process at 1 verdict/s across the whole
    network, 0.1/s in the low-rate case, plus a 60 s scanning burst of
    200 verdicts/s. Each verdict comes from a random publisher, is about
    1 KB and names a unique documentation address
    (`Testing.AllowDocumentationRanges`).
  - Publishing stops 30 s before a run ends.

### Scenarios

Every configuration below runs 20 seeds.

- **A: reproduces the paper.** 1,000 honest nodes on the paper's testbed
  topology and 4,000 Sybils, in the variants `plain`, `paper` and `v0.1`.
  The testbed's honest nodes each dial 20, which the paper's "20
  connections" means; a 20-regular graph halves their honest links, and
  plain GossipSub then loses 47 % rather than the paper's ~10 % in the
  eclipse attack (`documentation/validation/routing/harness-validation.md`):
  - `A-eclipse`: the network warms up. At 60 s the Sybils connect, 100
    links each, and drop everything. The run ends at 240 s.
  - `A-coldboot`: the Sybils, 20 links each, connect as the honest nodes
    start. The links between honest nodes come up after 1–5 s, and each
    node connects at its next bootstrap redial, so the honest connections
    form after about 1–12 s. The run ends at 240 s.
  - `A-covertflash`: like `A-coldboot`, but the Sybils forward until
    120 s. The run ends at 300 s.
- **B: scale.** 10,000 honest nodes and 10,000·f/(1−f) adversaries, for
  f ∈ {0.1, 0.3, 0.5}. All of them sit on one random 20-regular graph. The
  adversaries drop everything from the start. Variant `v0.1`; the run ends
  at 150 s.
- **C: OBIE's attacks.** Variant `v0.1` on the static-bootstrap graph:
  - `C-flood`:
    - 200 nodes with the real store, `store.max_indicators` 2,000.
    - Trusted verdicts (7-day TTL) from 30 s.
    - From 150 s to 210 s, 10 Sybil hosts inject 100 junk verdicts/s
      (30-day TTL), signed by 1,000 weight-0 keys in turn. The hosts
      forward honest traffic, so only the flood is measured.
    - The run measures the trusted verdicts retained by the nodes that
      accepted them. The publisher is left out, because a store never
      evicts its own node's verdicts.
  - `C-junk`: 300 nodes. From 60 s to 180 s, two Sybil hosts, both linked
    to the same hub, each inject junk at the default per-peer bucket rate
    (`mesh.rate_limit.peer`, 50/s); the hub relays it to its neighbors. The
    hosts forward honest traffic.
  - `C-preempt`: 1,000 nodes and 10 preempters, each with 100 links and a
    link to each chosen revoker. Three chosen revocations per seed, at 60,
    90 and 120 s, each by a different random publisher. The run ends at
    180 s.
  - `C-offline`: 300 nodes. 10 of them are offline from 60 s to 3,660 s,
    then rejoin with the same identity and store. The run ends at 3,780 s.
  - `C-bootkill`: 1,000 nodes. All hubs stop at 600 s. The run ends at
    900 s.
- **T: traffic and topology without attack.** Variant `v0.1`:
  - `T-static` and `T-regular`: 1,000 nodes, 1/s.
  - `T-lowrate`: 1,000 nodes on the static graph, 0.1/s for 600 s.
  - `T-burst-static` and `T-burst-regular`: 300 nodes, 1/s plus 200/s
    from 60 s to 120 s.
- **`reduced`** (CI): 100 honest nodes on a 20-regular graph and 400 cold
  boot Sybils (20 links each), one seed per variant.

### Metrics

- **The per-event trace.** Every honest node, and every Sybil for the
  copies it relays, records one record per copy of an event it receives,
  and per event it publishes. A record holds the node, the event, the peer
  it came from, the time and the outcome. This is the record shape of the
  per-event trace of ADR 0032 (#1764); the harness keeps it in memory
  through the tracer hook. One record's outcome is the validator's outcome
  (`accepted`, `rate_limited`, …) or GossipSub's reason for dropping the
  copy before validation (`duplicate`, `throttled`, …).
- **Joining the trace.** An event's path to a node follows the peer of each
  node's accepted copy back to the publisher. Its hop count is the length
  of that path, compared with ln N / ln(D−1) for the variant's D.
- **Per seed**, over the pairs of an honest verdict and an honest node
  that runs at the end of the run (a window's delivery ratio counts the
  same nodes):
  - **Delivery ratio**: the share of pairs the node accepted before the
    run ended.
  - **Latency**: acceptance time minus publish time, as p50, p99 and max.
  - **Duplicate factor**: copies received per delivered pair.
  - **Hop-count distribution.**
  - **Sybil share of mesh slots**: the mean share, over the attack window,
    of the honest nodes' mesh slots that hold an adversary.
  - **Mesh recovery time**: seconds from the disruption until the Sybil
    share falls to 10 % or less and stays there. After an outage or a hub
    kill, until the honest mesh slots are back to 90 % of their level
    before. A run that does not recover is reported as not recovered. The
    report averages the recovery time over the seeds that recovered and
    says how many did.
  - **Trusted verdicts retained under flood** (C-flood).
  - **Loss by cause**: the outcome of the first copy of an event it lost
    that a node validated. GossipSub remembers that copy's ID, so the node
    ignores later copies. Copies that arrive at the same instant are
    validated in parallel, so a duplicate can be recorded before the copy
    it duplicates; duplicates are therefore passed over. So is a copy
    dropped from a full validation queue, because GossipSub does not
    remember its ID and validates the next copy. A node that got
    no copy at all counts as `never_received`, or as `offline` if it was
    down at the time. GossipSub drops the RPCs of a graylisted peer before
    its tracer sees them, so their copies leave no record: a node that got
    an event only from graylisted peers counts as `never_received`.
- **Across seeds**: each metric is the mean of its per-seed values, with a
  95 % confidence interval from Student's t over the seeds. The interval
  is clipped to the values the metric can take: never below 0, and never
  above 1 for a share.
- **What a seed fixes.** A seed fixes the topology, the latencies, the
  publishers, the Sybils' targets and the traffic. It does not fix the
  run: goroutine scheduling, the jitter of GossipSub's and the mesh's
  timers and the Sybils' regraft delays vary, so two runs of one seed
  differ slightly. The confidence interval over 20 seeds covers both.

### The report and CI

- **The report.** `make sim-routing SCENARIO=<name|A|B|C|T|all>` runs
  `go test -tags sim` in `test/sim` with `SEEDS` (default 20), `SIMPARALLEL`
  runs at once and `SIMOUT`. For each scenario it writes
  `routing-<scenario>.md`, `routing-<scenario>.csv` (the summary with
  CIs) and `routing-<scenario>-seeds.csv` (one row per seed and metric).
  The report header records:
  - the report format version;
  - the OBIE version (`git describe`) that produced the results;
  - the go-libp2p-pubsub and go-libp2p versions;
  - the seeds;
  - the scenario's parameters.

  Each run's result is kept in `SIMCACHE`, so an interrupted run resumes.
  `SIMBUDGET` stops starting runs after a while; the report is written
  once every run of a scenario has a result.
- **CI.** The baseline of v0.1 lives in `documentation/validation/routing/`.
  A CI job runs the `reduced` scenario with assertions: `paper` loses
  nothing, `plain` loses something, and `v0.1` delivers at least 99 % (its
  baseline delivers everything). The job's timeout is 10 min.
- **Why the scenario runs outside `make test`.** The race detector allows
  8,128 live goroutines, and the reduced scenario needs about 47,000, so
  it runs without `-race` outside `make test`. The harness's unit tests
  run in `make ci`.

### Deviations from the paper

1. OBIE's traffic replaces the paper's 120 messages/s of 2 KiB:
   1 verdict/s of about 1 KB, through OBIE's validator and rate limits.
2. A geographic latency matrix replaces 25 ms ± 10 %.
3. Processing takes no time.
4. go-libp2p-pubsub v0.17 replaces the 2020 code, with the unconditional
   hardening listed under *Router variants*.
5. In cold boot, the honest nodes start at 0 s rather than 2 min, after
   the Sybils' links are made.

## Consequences

- **The baseline is the reference.** Every routing work package (#1767,
  #1768, #1770, #1771, #1772) re-runs the scenarios on its branch and
  compares with the committed baseline. A change in routing code shows up
  in the reduced CI scenario first.
- **Size of the eclipse attack.** The eclipse attack (400,000 honest–Sybil
  connections) needs about 40 GB and 2–7 minutes per seed, and runs one
  seed at a time. The full baseline takes about ten hours on the
  development machine. CI runs only `reduced`.
- **What is not modelled.** Processing time, bandwidth limits, packet
  loss, TCP and QUIC behaviour, and every subsystem outside the mesh.
  Results about queueing under CPU load therefore do not transfer. Every
  node, Sybils included, has an IP address of its own, so the score's IP
  colocation penalty never fires: the attacker has as many addresses as
  Sybils, the strongest case.
- **Scaled parameters.** `C-flood` scales `store.max_indicators` from
  1,000,000 to 2,000. Its flood of 6,000 junk verdicts is three times the
  store's capacity; at the default capacity the same flood would last
  about eight hours.
- **Hooks.** The hooks widen `gossip.Options` and `mesh.Options` by one
  `Testing` field each. Both default to production behaviour.
- **Once #1764 is merged,** its per-event trace files have the harness's
  record shape. The harness can then read `mesh.trace_path` files instead
  of the tracer hook.
