# Performance and hardening results

This page records the evidence that an OBIE node is safe to run exposed to
the internet and to misbehaving peers: the fuzzing campaign, static
analysis, the resource limits and the soak test. The design is recorded in
[ADR 0017](../adr/0017-hardening-and-resource-limits.md). Refresh the
numbers before every release.

## Test machine

| | |
|---|---|
| Date | 2026-09-28 |
| Version | `factory/agent/planner-1667` (WP-1667) |
| CPU | AMD Ryzen 9 7950X3D, 16 cores / 32 threads |
| Memory | 124 GiB |
| OS | Linux 7.0 (x86_64) |
| Go | 1.26.7 |

## Fuzzing

Every target ran for 10 min 30 s (`-fuzztime=10m30s -parallel=4`), all
seven at once. None failed, so no regression corpus entry was needed; the
CI job `fuzz` runs each for 30 s on every pull request (`make fuzz
FUZZTIME=30s`).

| Target | Package | Input | Executions | Result |
|---|---|---|---:|---|
| `FuzzDecode` | `pkg/obieproto` | event decoding and validation | 20.6 M | pass |
| `FuzzVerify` | `pkg/obieproto` | signature verification | 18.2 M | pass |
| `FuzzTransform` | `pkg/obieproto/internal/jcs` | JSON canonicalization | 32.6 M | pass |
| `FuzzParse` | `internal/config` | configuration file | 7.3 M | pass |
| `FuzzRequests` | `internal/admin` | admin API bodies, paths and queries | 2.4 M | pass |
| `FuzzParseEntry` | `internal/sovereignty` | allow-list entry (IP or CIDR) | 6.6 M | pass |
| `FuzzParseFile` | `internal/sovereignty` | allow-list file | 6.5 M | pass |

Reproduce with `make fuzz FUZZTIME=10m`.

## Static analysis

| Tool | Result |
|---|---|
| gosec (in golangci-lint, `make lint`) | 0 issues |
| gosec standalone, `-tests -tags soak` | 0 issues; every suppression is a `#nosec G… -- reason` on its line |
| govulncheck (`make vuln`) | no vulnerability reachable or in an imported package |

Found and resolved: `go.opentelemetry.io/otel` (pulled in by Badger) was
raised from v1.37.0 to v1.46.0 for GO-2026-5506 and GO-2026-5158. Open:
GO-2026-5932 (`golang.org/x/crypto/openpgp` is unmaintained) has no fix;
the package is not imported, so it only shows at module level.

## Resource limits

Each limit is verified by a test that runs in `make ci`.

| Limit | Value | Test |
|---|---|---|
| GossipSub RPC size | 64 KiB (16 × `MaxEventSize`) | `internal/gossip` integration tests |
| Events per publisher / per peer | `mesh.rate_limit`, default 10/s (burst 50) / 50/s (burst 250) | `internal/gossip` validate and integration tests |
| Stored verdicts | `store.max_indicators`, default 1,000,000; the verdict expiring first is evicted, never this node's own; `obie_store_evictions_total` | `TestCapBoundsFloodFromTrustedPeer` and the other `TestCap…` in `internal/store` |
| Verdicts kept after they ended | of other publishers a tenth of `store.max_indicators` (at least 1,000) revoked and as many expired ones, for a day after their expiry; beyond it only this node's own are kept; `obie_store_ended_verdicts` | `TestEndedVerdictsAreCapped` in `internal/store` |
| Admin request bodies | 1 MiB (reports, revocations), 16 KiB (overrides); 413 beyond | `internal/admin` |
| HTTP timeouts and headers | read header 5 s, read 10 s, write 30 s, idle 60 s, headers 16 KiB, on every server | `internal/httpserver` |

## Graceful degradation

- A damaged store stops `obied` at start with `event store is corrupt`,
  the damaged file and the remedy (restore a backup, or move the directory
  aside). Tested for damaged, truncated and missing MANIFEST, key registry,
  value log and tables (`TestStartDetectsCorruption`, `TestRunRefusesCorruptStore`),
  and against random damage of every file (`TestStartNeverCrashesOnDamage`);
  a store copied while open, as a crash leaves it, still opens
  (`TestStartOpensStoreAfterCrash`).
- With every peer gone a node stays ready, reports locally and blocks what
  it reports itself (local autoblock), and unblocks on revocation
  (`TestMeshDownKeepsLocalProtection` in `test/e2e`).

## Soak test

`make soak`: three complete nodes in one process that trust each other,
50 unique reports per second (10 % bans, the rest watches) spread evenly,
verdicts living 2 minutes, for 30 minutes. `mesh.rate_limit` is raised to
50 events/s per publisher (burst 250) and 100 per peer (burst 500), because
each node publishes 50/3 per second, above the default of 10. See
[CONTRIBUTING.md](../../CONTRIBUTING.md#soak-test).

| Criterion | Bound | Result |
|---|---|---|
| Reports sent | 90,000 | 90,000, 0 failed |
| Deliveries (each event to the 2 other nodes) | all | 180,000, 0 missing, 0 rate-limited |
| Propagation, report to stored on another node | p99 < 2 s | p50 1 ms, p99 1 ms, max 25 ms |
| Live heap without Badger caches, median 5–10 min → last 5 min | ≤ +20 % | 98.0 → 107.2 MiB, **+9.4 %** |
| Goroutines after warm-up | no growth | 327 throughout |
| Goroutine leaks after the nodes stopped | none (goleak) | none |

Memory of the whole test process (three nodes and the test), sampled
every 30 s after a garbage collection:

| Time | Live heap without caches | Badger caches | Largest reading |
|---:|---:|---:|---:|
| 5 min | 94.0 MiB | 16.1 MiB | 110.6 MiB |
| 10 min | 99.0 MiB | 41.6 MiB | 320.7 MiB (flush) |
| 16 min 30 s | 107.6 MiB | 79.4 MiB | 187.1 MiB |
| 17 min | 97.5 MiB | 3.2 MiB | 100.7 MiB |
| 25 min | 103.2 MiB | 56.3 MiB | 159.9 MiB |
| 30 min | 107.9 MiB | 87.9 MiB | 197.3 MiB |

How to read it:

- **Badger's block and index caches** (32 + 16 MiB per node) fill as the
  nodes read their tables and empty when compaction deletes tables (17
  min): a sawtooth bounded by their configured sizes. The test measures
  the heap without them (`store.DB.CacheBytes`).
- **Memtable flushes** briefly hold the table they build, about 60 MiB per
  node; the three nodes flush together because they get the same load, so
  single readings reach 275–345 MiB. The test takes the smallest of three
  readings 2 s apart; the table keeps the largest. At this load a node
  needs about 85 MiB of Go heap with full caches (a third of the process
  above), plus up to 60 MiB while it flushes; its RSS is higher than
  its heap.
- **The remaining drift** (about 10 MiB over 20 minutes for three nodes)
  comes from the caches' own bookkeeping (ristretto's per-entry maps,
  which their cost does not count) and follows their sawtooth; the
  decision engine, the gossip caches and the store hold a steady working
  set of about 9,000 verdicts per node.

While these tests were written, goleak found two goroutine leaks,
both fixed (ADR 0017): Badger's goroutines after a failed open of a
damaged store, and libp2p's swarm when no listen address could be bound.

## Resource usage of one node

What one node costs to run, as the
[capability overview](../capabilities.md#resources) reports it. `make
resources` (`TestResources` in `test/resources`, build tag `resources`)
starts three `obied` processes on 127.0.0.1 that trust each other (weight
0.8, threshold 1.2, quorum 2) and measures the first, A. A runs in
enforce mode with the `dryrun` backend, keeping every block without a
firewall, and writes an audit log. The two others report the same public
addresses at 100 reports per second each, so A receives 200 verdicts a
second and blocks every address. Every node runs with `GOMAXPROCS=1`, as
on a host with one processor core, and with rate limits raised to let the
load through. The test reads A's resident memory (RSS), its peak in each
phase and its CPU time from `/proc`, and the disk blocks of its state
directory and audit log.

Run on 2026-09-29 on the test machine above (AMD Ryzen 9 7950X3D, Linux
7.0), `obied` built from `1b3edc3`, the code of the upcoming 0.1.0, up
to the verdicts a node keeps by default (`make resources
RESOURCESVERDICTS=10000,100000,1000000`, 91 minutes). The CPU share of a
load phase is measured over its last 30 seconds of load:

| Phase | Verdicts held | Blocks | RSS | Peak RSS | CPU (share of one core) | State directory | Audit log |
|---|---:|---:|---:|---:|---:|---:|---:|
| idle for 2 min, 2 peers | 0 | 0 | 33 MiB | 33 MiB | 0.2 % | 0 MiB | 0.0 MiB |
| receiving 200 verdicts/s | 10,000 | 5,000 | 123 MiB | 123 MiB | 10.8 % | 15 MiB | 2.2 MiB |
| receiving 200 verdicts/s | 100,000 | 50,000 | 370 MiB | 491 MiB | 63.2 % | 65 MiB | 22.0 MiB |
| receiving 200 verdicts/s, 4,801 of them lost | 995,199 | 495,299 | 2,970 MiB | 3,675 MiB | 99.6 % | 633 MiB | 217.9 MiB |
| at rest for 2 min | 995,199 | 495,299 | 2,578 MiB | 3,039 MiB | 25.0 % | 633 MiB | 217.9 MiB |

`make resources` with its defaults, up to 100,000 verdicts, run the same
day on the same build, gave the same numbers within 8 %, and at rest for
2 minutes with 100,000 verdicts: RSS 362 MiB, 2.5 % of a core, 65 MiB of
state and 22.0 MiB of audit log. How to read them:

- **Memory grows with the verdicts held**, by about 3 MiB per 1,000
  once the node is at rest: 362 MiB with 100,000 verdicts, 2.5 GiB with
  about 1,000,000, the default `store.max_indicators`. The peak while
  receiving is up to a third higher (3.6 GiB at the cap); size a server
  for the peak.
- **CPU per received verdict grows with the decisions kept.** After every
  batch of decisions, the engine counts all it keeps for its metrics
  ([Console](#console): 11 ms at 1,000,000), so the same 200 verdicts a
  second cost 10.8 % of a core at 10,000 verdicts, 63.2 % at 100,000 and
  the whole core near 1,000,000.
- **A node that cannot keep up misses verdicts.** On the way to
  1,000,000, A could no longer take in 200 verdicts a second, and 4,801 of
  the phase's 900,000 (0.5 %) never arrived: GossipSub drops what a full
  queue cannot take. Its peers do not send them again, so they never
  count on A.
- **At rest, the periodic work grows with what the node keeps:** 2.5 % of
  a core with 100,000 verdicts, 25.0 % with 1,000,000.
- **The firewall holds 100,000 blocks by default.** A decided 495,299
  blocks; beyond `enforce.max_entries` the blocks with the lowest score
  are left out and logged ([configuration](configuration.md#enforce)).
- **Disk:** the state directory took about 0.65 KiB per verdict (65 MiB at
  100,000, 633 MiB at 995,199), the audit log about 0.45 KiB per blocked
  address.
- **The core is fast.** A small cloud server's core is slower, so expect
  higher CPU shares there, and a node that stops keeping up with fewer
  verdicts; memory and disk do not depend on the processor.

## Console

The console's decisions list reads its page on the node, in one pass over
the decisions the engine keeps, under the engine's read lock
([ADR 0022](../adr/0022-console-decisions-and-firewall.md)). Measured on
the test machine above on 2026-09-28 (WP-1685), Go 1.26.7, with 1,000,000
kept decisions — a tenth networks, a fifth IPv6, a third blocks, four
reason categories, eight publishers — for a page of 50:

| Page | Time | Allocated |
|---|---:|---:|
| First page, the last decided first (the default) | 19 ms | 34 KiB |
| First page by address | 21 ms | 32 KiB |
| A deep page by address (cursor half way) | 22 ms | 32 KiB |
| Last page by score | 33 ms | 33 KiB |
| Blocks only | 29 ms | 28 KiB |
| One reason category and one publisher | 26–29 ms | 37 KiB |
| Search for an address (it and the networks around it) | 4–6 µs | 6 KiB |
| Search for a /16 network | 13–15 ms | 34 KiB |
| A filter on the range, e.g. IPv4 only | 16 ms | 34 KiB |
| For comparison: the engine's own pass after every evaluation (metrics) | 11 ms | — |

The firewall filter (*Applied by the firewall* or *Not applied*) asks the
reconciler's last pass for every decision: 31 ns per decision with
100,000 entries (the default `enforce.max_entries`), so about 31 ms more
at 1,000,000 decisions. The time grows linearly with the decisions and
the memory with the page only; a page deep in the list costs the same as
the first. The list is read when the page opens, never refreshed on its
own. Reproduce with:

```sh
go test ./internal/decision -run '^$' -bench 'BenchmarkBrowse|BenchmarkPublishMetrics' -benchtime 30x
go test ./internal/enforce -run '^$' -bench BenchmarkSnapshotApplies
```

The verdicts view pages the active verdicts the same way, in one pass over
the engine's decisions and their verdicts
([ADR 0023](../adr/0023-console-verdicts.md)). Measured on the same machine
on 2026-09-28 (WP-1686), with the 1,000,000 decisions above holding
2,000,000 active verdicts, for a page of 50:

| Page | Time | Allocated |
|---|---:|---:|
| First page, by address and publisher | 22 ms | 13 KiB |
| A deep page (cursor half way) | 20 ms | 13 KiB |
| One publisher | 23 ms | 13 KiB |
| Received: every publisher but this node | 22 ms | 13 KiB |
| One reason category | 22 ms | 13 KiB |
| The verdicts on one address | 1 µs | 4 KiB |

The verdicts that were revoked or expired are read from the store, which
keeps them for 24 hours after their expiry. A walk over the keys of
100,000 of them — about what 700,000 indicators with the default 7-day
lifetime leave in a day, and the default cap — takes 41–43 ms, with the
page's 50 decoded, and grows linearly with the ended verdicts. The totals
are counted as the store keeps the verdicts (reading them takes under a
microsecond), and recounted by the sweep every minute with one such walk. Opening the verdicts view costs one
engine pass and one walk. Reproduce with:

```sh
go test ./internal/decision -run '^$' -bench BenchmarkVerdicts -benchtime 30x
go test ./internal/store -run '^$' -bench BenchmarkEnded100k -benchtime 30x
```

## Fail2Ban versions

`make fail2ban-versions`
([`contrib/fail2ban/check-versions.sh`](../../contrib/fail2ban/check-versions.sh))
installs the Fail2Ban of a distribution in a container and runs a real
`fail2ban-server` with one jail that uses the OBIE action and a stand-in
for `obiectl`. Two failed logins must make Fail2Ban report the address
with the failure count, the ban time as the verdict's lifetime and the two
matched lines as evidence; unbanning must revoke the verdict. Run on
2026-09-29 with the action of `ecfc6cc`:

| Distribution (image) | Fail2Ban | Result |
|---|---|---|
| Ubuntu 22.04 | 0.11.2 | pass |
| Debian 12, Ubuntu 24.04 | 1.0.2 | pass |
| Debian 13, Ubuntu 26.04, Alpine 3.22, Rocky Linux 9 (EPEL) | 1.1.0 | pass |
| Ubuntu 18.04, run once by hand | 0.10.2 | the report carries no lifetime: Fail2Ban 0.10 does not pass the ban time to the action, so the verdict lives `decision.default_ttl` |

Debian 11 can no longer be checked: its package mirrors stopped serving
it when its long-term support ended in August 2026. CI checks the action's
configuration with the Fail2Ban of its Ubuntu runner on every pull request
(`TestFail2BanAcceptsAction`).
