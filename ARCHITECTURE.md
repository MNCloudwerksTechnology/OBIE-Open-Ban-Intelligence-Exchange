# OBIE Architecture

This document is the binding technical baseline for the OBIE reference
implementation. Every work package builds on it; any change to the baseline
must update this file **and** be recorded as a new ADR in
[`documentation/adr/`](documentation/adr/). The decision record for the
initial baseline is
[ADR 0001](documentation/adr/0001-architecture-baseline.md); configuration
loading and logging are detailed in
[ADR 0002](documentation/adr/0002-configuration-and-logging.md); the daemon
lifecycle, ops endpoints and admin API in
[ADR 0003](documentation/adr/0003-daemon-lifecycle-and-admin-api.md); event
canonicalization and signing in
[ADR 0004](documentation/adr/0004-event-canonicalization-and-signing.md); the
node identity key in
[ADR 0005](documentation/adr/0005-node-identity-key.md); the protocol
specification, its JSON Schema and how both are kept in step with the code in
[ADR 0006](documentation/adr/0006-protocol-specification-and-schema.md); the
mesh host and static bootstrap peers in
[ADR 0007](documentation/adr/0007-mesh-host-and-bootstrap.md); the local event
store in [ADR 0008](documentation/adr/0008-local-event-store.md); the gossip
of events in [ADR 0009](documentation/adr/0009-gossip-of-events.md); the
trust-weighted decision engine in
[ADR 0011](documentation/adr/0011-trust-weighted-decision.md); the
allow-list, operator overrides, observe/enforce modes and configuration
reload in [ADR 0013](documentation/adr/0013-local-sovereignty.md); the
enforcement reconciliation in
[ADR 0014](documentation/adr/0014-enforcement-reconciliation.md); the
Prometheus metrics and the decision audit log in
[ADR 0015](documentation/adr/0015-metrics-and-audit-log.md); the
end-to-end test and its test hooks in
[ADR 0016](documentation/adr/0016-end-to-end-test.md); the website
stack and build in
[ADR 0010](documentation/adr/0010-website-stack-and-build.md); the landing
page content file and design system in
[ADR 0012](documentation/adr/0012-landing-page-content-and-design-system.md).

The [whitepaper in the README](README.md) describes the long-term vision. This
file describes what v0.1 actually builds; where the two differ, this file wins
for implementation work (see [Deviations from the whitepaper](#deviations-from-the-whitepaper)).

## Architecture baseline (v0.1)

- **Binaries:** `obied` daemon; `obiectl` talks to it over a local admin API (HTTP/JSON on a Unix socket, default `/run/obie/obie.sock`, mode 0660, group `obie`).
- **Layout:** `cmd/` entry points; `internal/` for everything (config, logging, event, crypto/identity, store, mesh, admin API, ingest, decision, enforce, metrics, audit); `pkg/obieproto` only for the public protocol types + sign/verify that third parties may import. `documentation/spec/` for the protocol spec.
- **Config:** one YAML file (default `/etc/obie/obie.yaml`), strictly validated (unknown keys are errors). State dir default `/var/lib/obie`.
- **Logging:** `log/slog` JSON to stderr.
- **Events:** obie/0.1 JSON; signatures are Ed25519 over the RFC 8785 (JCS) canonical form of the event with `publisher.signature` removed; `signature` = `"ed25519:" + base64url(no padding)`. IDs are UUIDv7.
- **Identity:** one Ed25519 key per node; the libp2p peer ID is derived from it (same key for mesh and event signing).
- **Storage:** BadgerDB v4 in `<state_dir>/db`: deduplicated events, the latest verdict per (publisher, indicator), revocations, TTL-based expiry with change notifications, operator overrides (ADR 0008).
- **Mesh:** go-libp2p (TCP + QUIC, Noise), GossipSub topic `obie/0.1/verdicts` (no pubsub signatures, message ID = event ID, validation before relay, per-publisher and per-peer rate limits, peer scoring), static bootstrap peers in v0.1.
- **Decision:** operator-assigned per-publisher trust weights; `score = Σ weight(publisher) × confidence` over distinct publishers' latest active verdicts; enforce iff score ≥ threshold (default 1.8) AND distinct publishers with weight > 0 ≥ quorum (default 2) — local verdicts count with `local_weight` and, with `decision.local_autoblock` (default), block on their own; only `ban` verdicts count (ADR 0011). Allow-list always wins (built-in ranges, own and bootstrap addresses, `allowlist.cidrs`, `allowlist.files`); operator force-allow / force-block overrides; mode `observe` (default) or `enforce`; SIGHUP reloads (ADR 0013).
- **Enforcement:** pluggable enforcer (`Setup`/`List`/`Apply`/`Teardown`, entries with timeouts); `dryrun` (default) and `nftables` (netlink via google/nftables, own table `inet obie` with interval+timeout sets `obie_v4`/`obie_v6` and a priority -10 input chain, optional forward chain, CAP_NET_ADMIN only, `obied teardown-firewall`; ADR 0015) backends; reconcile loop with `enforce.max_entries` cap and allow-list re-check (ADR 0014).
- **Ops:** Prometheus `/metrics` (namespace `obie_`, no high-cardinality labels), `/healthz`, `/readyz` on a separate listen address (default `127.0.0.1:9464`); ECS JSON-lines decision audit log at `audit.path`, reopened on SIGHUP (ADR 0015).
- **Testing:** table-driven unit tests, fuzz tests on all decoders (see [Fuzz testing](CONTRIBUTING.md#fuzz-testing)), in-process multi-node integration tests, above all the four-node end-to-end test in `test/e2e` (ADR 0016); privileged tests behind the `privileged` build tag, run in a fresh network namespace (see [Privileged tests](CONTRIBUTING.md#privileged-tests)).

## Repository layout

```text
cmd/
  obied/            node daemon entry point
  obiectl/          operator CLI entry point
internal/           all non-public code (one package per concern listed above)
  admin/            admin API on the Unix socket (server, wire types, obiectl client)
  audit/            decision audit log: ECS JSON lines, reopened on SIGHUP
  cli/              flag handling and commands of both binaries
  config/           YAML configuration schema, defaults, strict decoding, validation
  daemon/           wires the obied subsystems together and runs them
  decision/         trust-weighted consensus per indicator, explanations, block and transition streams
  enforce/          mode gate, reconciler and enforcement backends (dryrun)
  gossip/           GossipSub topic: validation, dedupe, rate limits, Publish, metrics
  httpserver/       HTTP server as a lifecycle subsystem
  identity/         persistent Ed25519 node key (<state_dir>/node.key); peer ID, signing
  lifecycle/        ordered subsystem start/stop with timeouts; status and readiness
  logging/          slog JSON handler; per-component loggers
  mesh/             go-libp2p host, bootstrap peers with backoff, peer view
  ops/              /healthz, /readyz and Prometheus /metrics
  sovereignty/      allow-list (built-in, config, files, own and bootstrap addresses), override precedence
  store/            BadgerDB event and indicator state: dedupe, expiry, overrides
  verdicts/         this node's own verdicts: report (hash evidence, coalesce), revoke, list
  version/          build version, injected via -ldflags
pkg/
  obieproto/        public protocol types + sign/verify (importable by third parties)
    internal/jcs/   RFC 8785 JSON Canonicalization Scheme
documentation/
  adr/              architecture decision records
  examples/         commented example configuration (tested against the schema)
  guides/           integration guides (Fail2Ban)
  operations/       monitoring: metrics, audit log, collector snippets, Grafana dashboard
  spec/             obie/0.1 protocol specification (obie-0.1.md) and JSON Schema
    test-vectors/   signing test vectors (generated by `go generate ./pkg/obieproto`)
test/
  e2e/              four complete obied nodes in one process: report → quorum block → revoke (ADR 0016)
diagrams/           whitepaper diagrams (PlantUML sources + PNG)
website/            public website, independent of the node (see Website)
```

Only the packages that exist today are listed in detail; the remaining
`internal/` packages are added by the work packages that need them.

## Conventions

- **Entry points stay thin.** `main()` only wires `os.Args`, stdio and the exit
  code into testable code under `internal/`.
- **Subsystems.** Every long-running part of `obied` implements
  `lifecycle.Subsystem` and is registered in `internal/daemon`: subsystems
  start in registration order and stop in reverse order within
  `node.shutdown_timeout` (ADR 0003).
- **Versioning.** Both binaries share `internal/version.Version`, which defaults
  to `dev` and is set at build time with
  `-ldflags "-X github.com/MNCloudwerksTechnology/obie/internal/version.Version=<v>"`
  (`make build` does this from `git describe`).
- **Static binaries.** Builds use `CGO_ENABLED=0`; the target platforms are
  Linux amd64 and arm64.
- **Protocol model.** `pkg/obieproto` is the single source of truth for the
  obie/0.1 event format. Received events go through `obieproto.Decode`
  (strict: size ≤ 4 KiB, no unknown/duplicate keys, full validation); events a
  node publishes are built, `Normalize`d and `Validate`d. Validation never
  rewrites a received event, and indicators in internal or special-purpose
  ranges are always rejected — a node must never publish internal addresses.
  Published events are signed with `obieproto.Sign`; received events are
  checked with `obieproto.Verify` after `Decode` — decoding and verification
  are separate steps and a receiver needs both. `Verify` takes the key from
  the Ed25519 peer ID in `publisher.peer_id`. The test vectors in
  `documentation/spec/test-vectors/` are generated and compared by the tests;
  regenerate them with `go generate ./pkg/obieproto` only for an intended
  change of the signed format. Messages from the mesh go through
  `obieproto.Receive` (decode, verify, then clock skew and expiry);
  which verdicts are in effect follows `Event.Supersedes` and
  `Event.Withdraws`. The specification
  [`documentation/spec/obie-0.1.md`](documentation/spec/obie-0.1.md) is
  normative for third parties; every MUST in it is tagged and mapped to a
  test, and the JSON Schema is tested to agree with `Decode` (ADR 0006).
- **Node identity.** `obied` loads `<state_dir>/node.key` (the libp2p
  marshaled Ed25519 private key) before any subsystem starts and generates it
  atomically on the first start; `obied keygen [--force]` creates it offline
  and `obied identity` / `obiectl identity` (`GET /v1/identity`) show the peer
  ID and the key fingerprint. A key file that is not a regular file, is
  accessible by group or others, or is owned by another user, and a state
  directory writable by group or others, stop `obied` with an error naming
  the fix. Subsystems receive an `identity.Identity`
  (`PeerID`, `PublicKey`, `Sign`) and never the private key (ADR 0005).
- **Mesh.** The `mesh` subsystem runs a go-libp2p host under the node
  identity (through a `crypto.PrivKey` adapter, so the private key still never
  leaves `internal/identity`), listening on `mesh.listen` over TCP and QUIC
  (IPv4 and IPv6 by default) with Noise, yamux, a connection manager with
  water marks 32/128 and the resource manager with default scaled limits. It
  is ready once it listens; zero connected peers is a valid, degraded state
  shown in the status detail, never "not ready". Every `mesh.bootstrap` peer
  is dialed at start and redialed whenever it is disconnected, with
  exponential backoff (1 s up to 5 min, equal jitter), and is protected from
  connection trimming. `obiectl peers` (`GET /v1/peers`) lists the connected
  peers with name and trust weight from `trust.publishers`, addresses,
  connected-since and ping latency. DHT, mDNS, relay, hole punching, NAT port
  mapping and the AutoNAT service are explicitly disabled (ADR 0007). The admin package
  does not import `internal/mesh`: `obiectl` stays free of go-libp2p.
- **Gossip.** The mesh joins the GossipSub topic `obie/0.1/verdicts` through
  `internal/gossip`: messages carry no author or pubsub signature
  (`StrictNoSign`) and their ID is the event ID. A topic validator checks
  size, format, signature, clock, duplicates (`store.Seen`) and the
  `mesh.rate_limit` token buckets per publisher and per forwarding peer, in
  that order; invalid events are rejected (and penalise the forwarder
  through peer scoring), duplicates and rate-limited events are ignored,
  accepted ones are stored and relayed. `Mesh.Publish` stores an event of
  the node first, then publishes it. Outcomes are counted in
  `obie_events_received_total` and reported through the `gossip.Metrics`
  interface (ADR 0009).
- **Decision.** The `decision` subsystem (registered right after `store`)
  subscribes to the store's change notifications, re-evaluates changed
  indicators on one worker goroutine and keeps the decision of every
  indicator with active verdicts. Evaluation is a pure, deterministic
  function (`decision.Evaluate`): only active `ban` verdicts of publishers
  with weight > 0 contribute; `block` iff score ≥ `decision.threshold` and
  contributors ≥ `decision.quorum`, or — with `decision.local_autoblock` —
  when this node itself reported a ban. A block expires with the latest
  contributing verdict, capped at `decision.max_ttl` from evaluation and
  refreshed while its verdicts live. `Engine.Subscribe` streams
  added/updated/removed block changes with their cause for the enforcer.
  `obiectl explain <ip>` (`GET /v1/decisions/{indicator}`) shows every
  publisher's weight, confidence and contribution, score vs threshold,
  count vs quorum and the final decision; `obiectl decisions`
  (`GET /v1/decisions?state=block`) lists them (ADR 0011).
- **Sovereignty.** `internal/sovereignty` builds the effective allow-list —
  built-in ranges (loopback, RFC 1918, CGNAT, link-local, ULA, multicast,
  unspecified, broadcast, IPv4-mapped, documentation), `allowlist.cidrs`,
  the IPs of `mesh.listen` (all interface addresses for an unspecified
  one; a public IP behind NAT goes into `allowlist.cidrs`), the `mesh.bootstrap` peers' IPs (DNS names resolved) and
  `allowlist.files` (one IP/CIDR per line) — and judges an indicator
  against it and the stored overrides, first match wins: force-allow
  (overlapping) > built-in/own/bootstrap entry (overlapping) > force-block
  (exact indicator) > `allowlist.cidrs`/files entry (overlapping) > the
  trust-weighted decision. Allow-listed indicators get state `allowed`;
  a force-block needs no verdicts and lasts until its override ends,
  capped at `decision.max_ttl` and refreshed. `obiectl allow|block <ip|cidr>
  [--ttl] [--note]`, `obiectl overrides` and `obiectl unoverride`
  (`GET/POST /v1/overrides`, `DELETE /v1/overrides/{indicator}`) manage the
  overrides; `obiectl explain` names the rule, its source and the match.
- **Modes and reload.** `internal/enforce.Gate` subscribes to the block
  change stream and is the only path to the enforcer: in `observe`
  (default) it logs every change and notifies nothing, in `enforce` it
  notifies the reconciler; switching applies or withdraws the current
  blocks. The mode is set only in the configuration (`obiectl status` shows it first). SIGHUP
  re-reads the configuration and the allow-list files and applies
  `node.mode`, `trust`, `decision` and `allowlist` at once; an invalid
  configuration or allow-list file is logged and the running one kept;
  changes to other keys are logged as needing a restart (ADR 0013).
- **Enforcement.** The `enforce` subsystem (`internal/enforce.Reconciler`,
  registered right after `decision`) makes the backend's entries match the
  gate's blocks: at start, ~250 ms after a block change or mode switch and
  every `enforce.reconcile_interval`, it computes the desired entries
  (unexpired address/CIDR blocks; allow-list re-checked right before
  apply — protected entries always refuse, `allowlist.cidrs`/files unless
  force-blocked; at most `enforce.max_entries`, highest score first),
  diffs them against `Enforcer.List` and applies the minimal add/remove.
  Every entry carries its remaining timeout, so the backend expires it
  even if `obied` dies. In `observe` it never sets up, lists or applies —
  it tears the backend down once — and its status says `observing`. A
  failed pass is retried with backoff (1 s up to the interval) and makes
  `/readyz` answer 503. `dryrun` (default) keeps entries in memory and
  logs every add/remove as JSON. `obiectl enforced` (`GET /v1/enforced`)
  lists the applied entries (ADR 0014).
- **Local verdicts.** `internal/verdicts` turns a local detection into a
  signed `indicator.verdict` (`obiectl report`, `POST /v1/reports`): defaults
  confidence 0.8, action `ban` and TTL `decision.default_ttl`, capped at
  `decision.max_ttl`. `evidence_lines` are hashed (`sha256` over the lines
  joined with `\n`) into `evidence.log_hash` and dropped — never stored,
  published or logged. Allow-listed (`allowlist.cidrs`) and non-public
  indicators are refused with 422. A report on an indicator this node already
  has an active verdict on refreshes it (new ID and expiry, cumulative
  counts); within 60 s of the last verdict a report is coalesced into the
  next refresh. `obiectl revoke` (`POST /v1/revocations`) withdraws this
  node's own active verdict by event ID or indicator (404 if there is none);
  `obiectl indicators` and `obiectl show` (`GET /v1/indicators[/{indicator}]`)
  list the active verdicts. Events are signed through the node identity
  (`obieproto.SignWith`) and published with `Mesh.Publish`. Every admin API
  request is checked against the peer's `SO_PEERCRED` credentials: only
  root, obied's own user and members of `admin.socket_group` get past 403
  (ADR 0012).
- **Observability.** Metrics are defined in the package that updates them
  and registered on the Prometheus default registry, which `internal/ops`
  serves; label values come from closed sets only (the admin endpoint label
  is the route pattern, never the path). With `audit.path` set, the `audit`
  subsystem (registered after `store`) appends one ECS JSON line per
  decision change: the engine's block changes (`block-added|updated|removed`)
  and transitions to `allowed` (`Engine.SubscribeTransitions`), override
  changes, local reports and revocations; startup state is not recorded.
  SIGHUP reopens it before the reload. Operator documentation, collector
  snippets and a Grafana dashboard are in `documentation/operations/`
  (ADR 0015).
- **End-to-end test.** `test/e2e` runs four complete nodes in one process
  through `daemon.Run`, each from its own configuration file, store, admin
  socket and libp2p host on `127.0.0.1` port 0, and drives them only
  through the admin API, `/metrics` and the mesh. What a test needs beyond
  the configuration file — documentation-range indicators on every layer,
  an ops port chosen by the OS, the bound addresses, an nftables network
  namespace per node — is in `daemon.Options.Testing`, which production
  leaves zero and the configuration cannot reach. Timing is asserted by
  polling with deadlines, never by sleeping (ADR 0016).
- **Quality gate.** `make ci` (gofmt check, `go vet`, golangci-lint,
  race-enabled tests, govulncheck, actionlint on the CI workflows) must pass
  before every commit. Tool versions are pinned in the `Makefile`. The CI
  pipeline (`.gitea/workflows/ci.yml`, mirrored byte-identical to
  `.github/workflows/ci.yml`) runs the same `make ci` on every pull request and
  every push to `develop`/`main`, plus a linux/amd64 + arm64 build matrix.

## Website

`website/` holds OBIE's public website (landing page, inquiry form, legal
pages). It is a separate product from the node: no Go code imports it, it
has its own build (`website/Makefile`) and gate (`make -C website ci`, the
`website` CI job), and the root `make ci` does not touch it
([ADR 0010](documentation/adr/0010-website-stack-and-build.md)).

- **Front end:** Angular (standalone components, strict TypeScript, SCSS) in
  `website/frontend/`, prerendered to static HTML at build time; no Node
  runtime in production and no third-party requests from the browser.
  Landing page copy lives in one typed content file; fonts are self-hosted;
  colours, spacing and type come from design tokens with light and dark
  themes ([ADR 0012](documentation/adr/0012-landing-page-content-and-design-system.md)).
- **Back end:** Spring Boot 3 on Java 21 in `website/backend/` (package
  `org.obie.website`, Maven wrapper). It serves the prerendered pages as
  static resources, the API under `/api/**` and Actuator health at
  `/api/health`; unknown URLs get a real 404 page, not the index.
- **Build and deployment:** the build produces one artefact,
  `website/backend/target/obie-website.jar`, which contains the front end and
  runs with `java -jar` on port 8080. See
  [`website/README.md`](website/README.md) for the commands.

## Future work

Deliberately out of scope for v0.1, recorded here so the current design does
not preclude them:

- **Key rotation statements.** A node can only replace its key
  (`obied keygen --force`), which gives it a new peer ID that every trusting
  peer must re-enter. A later release adds a statement signed by the old key
  that vouches for the new one, so peers can carry trust weights over.
- **Peer discovery and NAT traversal.** v0.1 only connects to the static
  `mesh.bootstrap` peers and to peers that dial in. A Kademlia DHT for peer
  discovery, mDNS for LAN discovery, and circuit relay with hole punching
  (plus AutoNAT and NAT port mapping) for nodes behind NAT are planned for
  later releases; they are deliberately disabled in the host today.
- **Organisational identity.** v0.1 knows only per-node peer IDs. Binding
  nodes to an organisation (for example by a DNS domain challenge), and
  quorum rules over distinct organisations, follow in a later release.

## Deviations from the whitepaper

v0.1 deliberately narrows the whitepaper to a shippable core:

| Whitepaper                                        | v0.1 baseline                                               |
|---------------------------------------------------|-------------------------------------------------------------|
| Kademlia DHT discovery                            | Static bootstrap peers                                      |
| Per-protocol topics (`obie.v0.ssh`, …)            | Single GossipSub topic `obie/0.1/verdicts`                  |
| Computed multidimensional peer reputation         | Operator-assigned per-publisher trust weights               |
| ASN / organisation diversity quorum (3 each)      | Distinct-publisher quorum (default 2)                       |
| OPA/Rego policy engine                            | Built-in weighted-score decision with allow-list            |
| JSON or CBOR/COSE events                          | JSON only, JCS-canonicalised, Ed25519-signed                |
| RocksDB/BadgerDB, CRDTs                           | BadgerDB v4, no CRDTs                                       |
| nftables, eBPF, Fail2Ban shims                    | `nftables` and `dryrun` enforcers                           |
