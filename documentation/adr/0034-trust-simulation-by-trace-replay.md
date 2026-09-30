# ADR 0034: A trust simulation that replays traces through the real decision engine

- **Status:** Accepted
- **Date:** 2026-09-30
- **Work package:** [#1766](https://openproject.niew.dev/work_packages/1766)
  (epic [#1761](https://openproject.niew.dev/work_packages/1761)); builds on
  [ADR 0011](0011-trust-weighted-decision.md),
  [ADR 0013](0013-local-sovereignty.md) and
  [ADR 0032](0032-gossip-instrumentation-and-attribution.md)

## Context

The trust work packages of epic #1761 change how a node weighs its
publishers: learned weights with forgetting and a quarantine (#1776), an
ASN and operator diversity quorum (#1777), recalibrated confidence (#1779)
and category topics (#1774). Each states a numeric target. No one can say
today how well v0.1's static weights decide under attack, so there is
nothing to compare a target with.

v0.1 decides on each address from the latest active ban verdict of every
publisher: score = Σ weight × confidence, a block iff the score reaches
`decision.threshold` (1.8) and `decision.quorum` (2) publishers with a
weight above 0 contribute; the node's own verdict blocks alone. The
allow-list and the operator's overrides overrule the verdicts (ADR 0013).
A report's confidence is 0.8 unless it sets one, and the Fail2Ban action
sets none. Two fully trusted remotes therefore score 1.6, below 1.8. The
end-to-end test uses confidence 0.95 and the compose lab lowers the
threshold to 1.5, which hides this.

The work package asks for a simulator that replays attack and benign
traffic against publishers of known behavior, scores the enforced bans
against ground truth, and publishes the v0.1 baseline.

## Decision

### One observer node, replayed in virtual time

- **Trust is a local decision.** A run models one *observer* node, which
  trusts N remote publishers and runs Fail2Ban itself. Routing is the
  concern of #1765 (ADR 0033); here every verdict reaches the observer at
  the second its publisher issues it. The metrics are hourly, so the
  sub-second delays that #1765 measures do not matter.
- **The replay adapter runs the node's real code.** From a validated
  `config.Config` it builds what `obied` builds (`internal/daemon`):
  - `sovereignty.Build` makes the allow-list: the built-in ranges,
    `allowlist.cidrs`, no own or bootstrap addresses;
  - `decision.NewPolicy` makes the policy from `trust` and `decision`;
  - `store.NewMemory` is the in-memory BadgerDB store, with the node's
    `store.max_indicators`, `store.ended_retention` and its own peer ID;
  - `decision.New` is the engine, subscribed to the store.

  A virtual clock replaces the wall clock through the existing
  `Options.Now` of store and engine. The adapter delivers each event with
  `store.Put` and evaluates it at once with `Engine.Flush`, so every event
  is decided on its own and a run is deterministic. Every 5 virtual
  minutes it calls `store.Sweep`, as the node's sweep does. The background
  timers of store and engine are set to 24 hours of wall time, so they
  never fire during a run. The harness does not re-implement a rule of
  the decision or the allow-list.
- **What is left out**, and why:
  - *Signatures.* The store does not check them; the gossip validator
    does. Events are unsigned.
  - *Enforcement.* An enforced ban is a block in the engine's block
    change stream: the stream an `enforce`-mode node applies (ADR 0014).
    `enforce.max_entries` is never reached.
  - *Documentation ranges.* The simulated world lives in `2001:db8::/32`,
    so the adapter sets `sovereignty.Env.OmitDocumentationRanges`, the
    hook the multi-node tests use. No other built-in range is touched.
- **Attribution.** The block change stream names the verdicts that count
  in each block (ADR 0032). The harness attributes every enforced ban to
  its publishers from them.
- **Weights.** The harness reads each publisher's weight from
  `Engine.Policy().Weight` every 5 virtual minutes. In v0.1 it never
  changes. A trust work package that learns weights exposes them through
  the engine, and the probe reads them there. It feeds the signals its
  model learns from (a force-allow, a benign-set hit) through the same
  adapter.

### Traces

- **A trace** is what the simulation replays. It holds:
  - the *operators*: operator 0 is the observer, the others are the
    remote publishers, each with its ASN and Fail2Ban `bantime`;
  - the *addresses*, each with its ground-truth class (`attacker`, `cdn`,
    `crawler`, `customer`, `nat`) and ASN;
  - the *published benign ranges*: the CDN and crawler ranges anyone can
    download;
  - the *observations*: operator o's Fail2Ban banned address a at time t.

  It is stored as JSON lines (`obie-trust-trace/1`).
- **Real logs are not used yet.** The work package asks for Fail2Ban logs
  of at least 10 cooperating operators. Their licensing and their
  treatment under the DSGVO must be settled before any log is used, and
  that is not a decision a harness can take. The baseline therefore runs
  on synthetic traces. A recorded trace replays with `TRACE=<file>`.
- **The importer** turns an operator's `fail2ban.log` into a trace. It
  reads the `Ban` lines of `fail2ban.actions` and replaces every address
  before anything leaves the operator's host:
  - An IPv4 address `a.b.c.d` becomes
    `2001:db8:XXXX:YYYY::d`, where `XXXXYYYY` are the first 32 bits of
    HMAC-SHA-256 over `a.b.c` under a key the contributors share. The
    /24 becomes a /112, so prefix relations survive and one address maps
    alike in every operator's log.
  - An IPv6 address maps its /64 the same way and keeps 16 bits of the
    HMAC of the whole address as its host part.
  - Before the replacement, an address inside a CIDR of the benign-ranges
    file gets that range's class; every other address is presumed an
    attacker.

  Pseudonymised addresses are still personal data under the DSGVO. The
  shared key is what a data processing agreement must cover.
- **The synthetic world** (per seed, 168 hours):
  - 21 operators: the observer and 20 remote publishers. Their sizes
    follow a log-normal distribution (σ = 1); attackers pick targets in
    proportion to size. Bantimes: 10 minutes (25 %), 1 hour (25 %),
    1 day (35 %), 7 days (15 %).
  - Attackers arrive at 30 per hour: 60 % hit one operator, 30 % run a
    campaign against 4–6 operators within 30 minutes (Katti et al. 2005),
    10 % scan: each operator with probability 0.7, spread over
    1–12 hours. 25 % return once, 1–3 days later. Fail2Ban bans an
    attacker at a hit with probability 0.9, 1–10 minutes after it starts.
    55 % of attackers come from hosting ASNs, 35 % from residential ASNs
    and 10 % from behind a shared NAT address.
  - The protected population: 300 CDN edge addresses in 12 published
    ranges of 3 CDNs, 120 crawler addresses in 6 published ranges of
    2 search engines, 2,000 customer addresses and 200 shared NAT
    addresses, which also carry the attackers behind them. All
    2,620 are benign.
  - ASNs are numbered from the private range 64512–65534 (RFC 6996).
    Each ASN is a /48, each published range a /64 inside it.

### Publisher behavior models

A run's publishers take the trace's operators in order: operator i is
publisher slot i. For an adversary fraction f, the last round(N·f) slots
are adversaries, and the others are honest. The world of a seed is thus
the same for every model, fraction and setting (common random numbers).
The first two slots are *newcomers*: they join at hour 24. Adversaries
*defect* at hour 48; before that they behave honestly.

| Model | What it does from its defection |
|---|---|
| honest | Reports each of its operator's bans: confidence 0.8, TTL = bantime. 1 % of its reports name a CDN edge instead, as a reverse proxy that logs the CDN's address would. Every adversary does this too, as camouflage. |
| naive | Also reports victims from a pool of 100 benign addresses of any class, 20 per hour, confidence 1.0, TTL 7 days. |
| careful | Like naive, but the pool avoids the published ranges (customers and NAT addresses only), 2 per hour, confidence 0.8, TTL 1 day. |
| onoff | Careful poisoning at 8 per hour with a TTL of 1 hour, during the first D·P of every period P (P = 24 h, D = 0.25); honest otherwise. |
| whitewash | Naive poisoning. Once its key's weight is 0, it waits an hour and goes on under a new key that no trust entry lists. |
| sybil-1asn, sybil-masn | All adversaries are one coalition. Twice an hour it picks a careful-pool victim, and every member reports it within 2 minutes (confidence 0.8, TTL 1 day). Their `publisher.asn` is one ASN, or one of m = 3. |
| spies | Half the adversaries (rounded up) poison like naive at 4 per hour with a TTL of 1 day; the others stay honest and corroborate each poison verdict within 1–10 minutes (confidence 0.8). EigenTrust's threat model D. |
| suppressor | Shields the attackers of a third of the hosting ASNs: of its bans on them, half are never published and half are revoked after 1–5 minutes. |

Every event carries its publisher's `publisher.asn`, although v0.1 does
not read it, so that #1777 can use it.

### Settings

Every trusted remote is listed in `trust.publishers` with weight 1.0, the
ceiling. The observer's own verdicts count with `trust.local_weight` 1.0
and block on their own.

- `default`: `config.Default()` otherwise (threshold 1.8, quorum 2,
  default weight 0).
- `lab`: threshold 1.5, as the compose lab.
- `allowlist`: `default` plus the published CDN and crawler ranges in
  `allowlist.cidrs`. It is what an operator can do in v0.1 against a
  poisoner.

### Metrics

Each metric is computed per seed, over the window of one hour [h, h+1)
and cumulatively over [0, h+1), for every hour of the run.

- **Enforced bans.** An episode starts with a block `added` and ends at
  its `removed`, or earlier at the block's expiry. A block `updated`
  after its expiry starts a new episode. An address counts as banned in
  a window if one of its episodes overlaps it.
- **Precision, recall, F1.** Precision is the share of the banned
  addresses that are attackers. Recall is the share of the attackers
  active in the window (banned by the observer's or an honest
  publisher's Fail2Ban) that were banned in it.
- **False bans per protected victim.** Episodes on benign addresses that
  start in the window, divided by the size of the protected population.
- **Defection to neutralisation.** A publisher key is neutralised when its
  weight falls to 0: it no longer contributes to any ban. Hours and
  events (the key's own events) from its defection until then. A key not
  neutralised counts until the window ends (restricted mean), and the
  share neutralised is reported with it.
- **False bans caused before neutralisation.** False episodes among whose
  contributors, at the start or an update, is an adversary key not yet
  neutralised.
- **Honest publishers' mean weight** as a fraction of the ceiling, over
  the 5-minute probes in the window, for the honest publishers that
  have joined.
- **Newcomer convergence.** Hours from a newcomer's joining until its
  weight first reaches 90 % of the ceiling, restricted like
  neutralisation.
- **Whitewashing payoff.** A whitewasher's false bans, over all its keys,
  divided by the number of keys it burned (at least 1).
- **Calibration.** Over the ban verdicts the observer receives in the
  window, the outcome is 1 for an attacker and 0 otherwise. The Brier
  score is the mean of (confidence − outcome)². The ECE is computed over
  10 equal-width bins (Guo et al. 2017).
- Durations per key (neutralisation, convergence, payoff) have no
  meaning within one hour. Their hourly rows carry the cumulative value.
- **Feed metrics** per publisher over the whole run, after Li et al. 2019:
  - volume: the addresses it reported;
  - exclusive contribution: the share of those no other feed reported;
  - relative latency: the median delay of its first report behind the
    first report of any feed, over the addresses others reported too;
  - the benign-set accuracy bound: 1 − the share of its addresses inside
    the published ranges.

  The true accuracy from the ground truth is reported beside the bound,
  because a careful poisoner keeps the bound at 1.
- **Corroboration.** For the honest-only runs, the attackers are grouped
  by the most honest remotes with verdicts active on them at the same
  time, and by whether the observer banned them itself. The share of
  each group that was banned shows how many trusted remotes a ban needs.

### Statistics, scenarios, report

- Every configuration runs 20 seeds (`SEEDS`). A metric is reported as
  the mean with a 95 % Student-t interval over the seeds for which it is
  defined; their number is reported too.
- Scenarios (`make sim-trust SCENARIO=…`):
  - `baseline`: every model at 10, 20, 30 and 40 %, and honest-only, in
    the three settings.
  - One scenario per model with the same world: `honest`, `naive`,
    `careful`, `onoff`, `whitewash`, `sybil-1asn`, `sybil-masn`, `spies`,
    `suppressor`.
  - `reduced`: a 36-hour world of 11 operators at 10 attackers per hour
    (newcomers at 6 h, defection at 12 h, P = 12 h), honest-only and four
    models at 20 and 40 %, `default` and `lab`, 20 seeds. It asserts
    what v0.1 guarantees: two trusted remotes at 0.8 never ban under
    `default` and do under `lab`; weights never move; poisoners cause
    false bans and are never neutralised.
- The harness is the package `test/simtrust`; its entry test carries the
  build tag `simtrust`, like `make soak`. Runs are spread over
  GOMAXPROCS workers. Its other tests run with `make test`.
- The report is a Markdown file plus `summary.csv` (cumulative at the end),
  `hourly.csv` (every hour, both windows) and `feeds.csv`. Its header
  names the report format (1), the OBIE version and commit, the scenario,
  the seeds and the trace. The v0.1 baseline is committed under
  `documentation/validation/`.
- CI runs `make sim-trust SCENARIO=reduced` as a job of its own with a
  10-minute timeout.

## Alternatives considered

- **Running the mesh as well** (the mocknet of ADR 0033): a trust decision
  does not depend on the path a verdict took, and about 2,000 runs of a
  week each would cost hours of CPU for nothing. Rejected; routing
  delays are #1765's.
- **Calling `decision.Decide` directly:** it is the rule, but not the
  engine. The engine adds the latest verdict per publisher, revocations,
  expiry, re-evaluation and the change stream an enforcer applies.
  Rejected for the engine over the real store.
- **Real operator logs now:** licensing and the DSGVO are unsettled.
  Rejected until they are; the trace format and the importer are ready.
- **A fixed number of honest publishers with adversaries added:** the
  fractions then come out as 11 % or 29 %, not 10 % or 30 %. Rejected
  for a fixed N, where the adversaries replace honest publishers.

## Consequences

- Every trust work package runs `make sim-trust` before and after its
  change and compares with the committed baseline.
- The baseline is only as realistic as the synthetic world. Its
  parameters are stated in the report, and the same scenarios replay a
  recorded trace once one may be used.
- `test/simtrust` depends on the engine's `Options.Now`, `Flush`,
  `Subscribe` and `Policy`, and on the store's `Options.Now` and `Sweep`.
  A change to them must keep the simulation working; the reduced
  scenario in CI checks that.
