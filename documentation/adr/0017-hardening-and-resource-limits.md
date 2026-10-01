# ADR 0017: Hardening and resource limits for release

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1667](https://openproject.niew.dev/work_packages/1667)

## Context

v0.1 nodes run exposed to the internet and to peers that may be buggy or
hostile. Before the release there had to be evidence that no outside
input crashes a node or grows its memory or disk without bound, that a
damaged disk is reported instead of misread, and that a node keeps
protecting itself when the mesh is gone. Several limits existed only in
part: the store had no size bound at all (ADR 0008), GossipSub accepted
RPCs of 1 MiB although an event is at most `obieproto.MaxEventSize`, the
HTTP servers had only a header timeout, and nothing measured the node
under sustained load.

## Decision

- **Every HTTP server has timeouts and a header cap.** `internal/httpserver`
  sets `ReadHeaderTimeout` 5 s, `ReadTimeout` 10 s, `WriteTimeout` 30 s,
  `IdleTimeout` 60 s and `MaxHeaderBytes` 16 KiB on every server it runs
  (admin API and ops endpoints); the endpoints answer from memory or the
  local store, so a slower request is stuck. Admin request bodies stay
  bounded (reports and revocations 1 MiB, overrides 16 KiB); an oversized
  body is answered with 413 on every endpoint.
- **GossipSub RPCs are capped at 16 × `MaxEventSize`** (64 KiB) in both
  directions with `pubsub.WithMaxMessageSize`: a larger RPC resets the
  stream before anything in it is parsed. The per-publisher and per-peer
  token buckets (ADR 0009) are unchanged.
- **`store.max_indicators` bounds the store** (default 1,000,000). It counts
  verdict records — one per publisher and indicator — which is the unit a
  flood of unique indicators grows. At the cap the record expiring first
  is evicted (`ReasonEvict`, `obie_store_evictions_total`); this node's own
  verdicts are never evicted, and a foreign verdict expiring sooner than
  every candidate is refused instead. An evicted event keeps its seen
  marker, so a replay stays a duplicate. `obie_store_verdict_records` exposes the
  count, kept in memory and initialised by a key-only scan at start.
- **A damaged store is detected before Badger opens it.** Badger does not
  report all damage as an error: it opens a directory without MANIFEST as
  a new database, does not verify table checksums on open, panics in its
  own goroutines on some damaged tables and leaks goroutines when opening
  fails. `store.Start` therefore first reads the key registry, the value-
  and memtable-log headers, the MANIFEST and every table it lists
  (verifying every block checksum, recovering panics), and reports damage
  as `store.ErrCorrupt` with the remedy: restore a backup or move the
  directory aside.
- **Fuzzing covers every outside input**: event decoding and validation,
  canonicalization, signature verification, the configuration file, admin
  API requests and the allow-list. `make fuzz FUZZTIME=…` runs every
  `Fuzz*` function; CI runs each for 30 s; before a release each runs for
  at least 10 minutes.
- **Static analysis**: gosec runs inside golangci-lint in `make ci`, and
  govulncheck in `make vuln`. A suppression is a `#nosec G… -- reason`
  comment on the line, so golangci-lint and a standalone gosec agree.
- **The soak test and goleak.** `make soak` (build tag `soak`, not in CI)
  runs three nodes at 50 events/s of 2-minute verdicts for 30 minutes and
  asserts delivery, propagation p99 < 2 s, no growth of the goroutines and
  at most 20 % growth of the live heap after warm-up. The heap is measured
  without Badger's block and index caches (`store.DB.CacheBytes`): they
  are bounded by their configured sizes but fill and empty in a sawtooth
  with compaction, which would hide a leak or fake one. Memtable flushes
  briefly hold the table they build (about 60 MiB per node); the smallest
  of three readings leaves them out, the report keeps the peak. Its
  `mesh.rate_limit` is raised above the defaults, which a publisher of
  50/3 events per second exceeds. Every package's `TestMain` runs
  `goleak.VerifyTestMain` (`go.uber.org/goleak`, a test-only dependency),
  so no test may leave a goroutine behind. The test reaches each node's
  store through the hook `daemon.Testing.Store` (ADR 0016's rule: test
  switches live in `daemon.Options.Testing`, never in the configuration).

## Consequences

- Memory and disk of a node are bounded by `store.max_indicators` rather
  than by the behaviour of its peers; operators size it to their host and
  watch `obie_store_evictions_total`.
- A trusted peer that floods unique indicators displaces the verdicts
  expiring first, never this node's own — local protection survives.
- A corrupt store stops the node at start with a message naming the damaged
  file and the remedy, rather than starting empty or crashing later.
- The mesh now binds its listen addresses after the libp2p host is built,
  because `libp2p.New` leaks the swarm's goroutines when no address can be
  bound; goleak found it. An empty `mesh.listen` keeps libp2p's default
  addresses, as before.
- Stores written before this change may hold revoked verdict records
  without an expiry index entry. They are not counted against
  `store.max_indicators` and never evicted, but the sweep removes them
  when they expire (at most `decision.max_ttl` later). Only development
  stores are affected; no release preceded this one.
- Results of fuzzing and the soak test are recorded in
  `documentation/operations/performance.md` and must be refreshed before
  each release.
