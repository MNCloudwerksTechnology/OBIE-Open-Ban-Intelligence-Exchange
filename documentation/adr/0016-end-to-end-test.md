# ADR 0016: The end-to-end test and its test hooks

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1664](https://openproject.niew.dev/work_packages/1664)

## Context

Every subsystem has its own tests, but nothing proved OBIE's core promise
as a whole: a report on one node becomes a block on another under
trust-weighted consensus, and a revocation lifts it. That proof has to run
on every pull request, so it cannot need root, fixed ports or a real
firewall, and it must not flake.

Three things stood in the way of running complete nodes side by side in
one test process:

- Every layer refuses the documentation ranges (192.0.2.0/24,
  198.51.100.0/24, 203.0.113.0/24, 2001:db8::/32, 3fff::/20): the
  verdict service, `obieproto.Receive` on the mesh (deliberately
  ignoring `AllowDocumentationRanges`), and the built-in allow-list,
  which the decision engine and the reconciler both apply. Any other
  address a test blocks could be a real host.
- `metrics.listen` refuses port 0, so the OS cannot choose the ops port;
  and `daemon.Run` does not tell its caller which ports it bound.
- The nftables backend programs the network namespace of the process, so
  several nodes in one process would share one table `inet obie`.

## Decision

- **`test/e2e`, a plain `go test` package.** It starts four complete
  nodes with `daemon.Run`, each from a configuration file written to its
  own temporary directory and read with `config.Load`, with its own
  identity key (created up front, like `obied keygen`, so that the trust
  sections can name the peer IDs), BadgerDB store, admin socket, audit log
  and libp2p host on `127.0.0.1` port 0. Each node bootstraps to the nodes
  started before it. A, B and C trust each other with weight 1.0; D is
  trusted by nobody (default weight 0). Threshold 1.8, quorum 2, `enforce`
  mode, `dryrun` backend. The test drives the nodes only through their
  admin API client (`report`, `revoke`, `explain`, `enforced`, `peers`),
  their `/metrics`, a bare GossipSub peer for the forged event, and a
  restart of C.
- **Scenarios run in order and build on each other**: local autoblock
  without quorum, block under quorum with `explain`, below the threshold,
  an untrusted publisher, an allow-listed report, a forged event, a
  restart restoring the blocks from the store, and a revocation. Each
  bounds propagation by 5 s and enforcement by 10 s; the whole test by
  60 s (it takes about 11 s).
- **No sleeps.** Helpers poll every 20 ms with a deadline (`within`) or
  check that a condition keeps holding for 1.5 s, longer than the
  reconciler's debounce (`holds`). Before the scenarios, each node
  publishes watch verdicts until they reach every other node, since
  GossipSub drops events published before it has seen a peer's
  subscription.
- **Test hooks, never configuration.** `daemon.Options.Testing` holds
  everything the test needs and production leaves zero; none of it is
  reachable from the configuration file:
  - `AllowDocumentationRanges` switches on, for this node only,
    `verdicts.Options.AllowDocumentationRanges`,
    `gossip.Options.AllowDocumentationRanges` (through the mesh), which
    passes the new `obieproto.ReceiveDocumentationRanges` option to
    `Receive`, and `sovereignty.Env.OmitDocumentationRanges`, which leaves
    the documentation ranges out of the built-in allow-list. The public
    `AllowDocumentationRanges` option keeps its meaning; `Receive` only
    accepts documentation ranges with the separate, explicitly named
    option.
  - `MetricsListen` replaces `metrics.listen` (e.g. `127.0.0.1:0`); the
    configuration keeps refusing port 0.
  - `Started` reports the bound mesh multiaddrs and ops address once every
    subsystem runs.
  - `NFTablesNetNS` is a file descriptor of the network namespace the
    nftables backend programs (`nft.Options.NetNS`).
- **Prometheus metrics are process-wide.** The nodes share the default
  registry, so the test reads counters as deltas and only where a single
  node can have caused the change (the forged event goes to C alone).
- **The privileged variant** (`//go:build privileged && linux`) runs the
  core scenario — report, block under quorum, revoke — with
  `enforce.backend: nftables`. Like the nftables package tests, `TestMain`
  re-executes the binary under `unshare -rn` (or `unshare -n` as root) and
  skips if neither works, so the host's firewall is never touched; inside,
  every node programs a namespace of its own. In C's namespace X is a
  local address with a TCP listener: connections from X pass before the
  block, time out while C blocks X and pass again after the revocation.
  It builds on the nftables backend of WP-1662 (PR #27).

## Consequences

- `make ci` runs the end-to-end test with every other package (`go test
  ./...`); `make test-privileged` adds the nftables variant.
- A new layer that refuses documentation ranges must honour the test
  option too, or the end-to-end test fails — which is the point.
- The hooks widen `daemon.Options`, `gossip.Options`, `mesh.Options`,
  `verdicts.Options`, `sovereignty.Env`, `nft.Options` and the public
  `obieproto` options by one field or option each; all default to
  production behaviour.
