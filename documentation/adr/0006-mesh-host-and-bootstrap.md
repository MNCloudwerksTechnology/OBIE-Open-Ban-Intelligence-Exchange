# ADR 0006: Mesh host and static bootstrap peers

- **Status:** Accepted
- **Date:** 2026-09-27
- **Work package:** [#1655](https://openproject.niew.dev/work_packages/1655)

## Context

ADR 0001 fixes go-libp2p (TCP + QUIC, Noise) and static bootstrap peers for
the v0.1 mesh; ADR 0005 fixes the node key and that the private key never
leaves `internal/identity`. Before verdicts can be gossiped (WP #1656), nodes
must form an encrypted, authenticated mesh with the peers the operator
configured, and stay connected when those peers go away and come back.

## Decision

- **Dependency:** `github.com/libp2p/go-libp2p` v0.50. It requires Go 1.26,
  so the module's Go baseline moves from 1.23 to 1.26 (superseding the
  "newest release that still supports Go 1.23" pin of ADR 0003; CI reads the
  version from `go.mod`). Older go-libp2p releases that support Go 1.23 are
  unmaintained and carry known vulnerabilities.
- **Subsystem:** `internal/mesh` is a lifecycle subsystem named `mesh`,
  registered after `ops` and before `admin`. `Start` creates the host and
  returns as soon as it listens; dialing happens in the background. It is
  **ready when listening**, independent of peers: zero connected peers is a
  valid, degraded state reported as the subsystem's status detail (a new
  optional `lifecycle.DetailReporter`), not as "not ready" — local
  protection must keep working without peers.
- **Host configuration:** listen on `mesh.listen`; transports TCP and QUIC
  only (no WebSocket, WebTransport, WebRTC); security Noise (QUIC brings its
  own libp2p TLS 1.3 handshake, which authenticates the same key); muxer
  yamux; connection manager with low/high water marks 32/128 and a one
  minute grace period; the resource manager with go-libp2p's default limits
  scaled to the machine's memory and file descriptors. A listen address that
  cannot be bound is skipped with a log line (e.g. IPv6 disabled on the
  host); if none can be bound the subsystem fails to start.
- **Logging:** go-libp2p's own log records at warn level and above go
  through `obied`'s JSON handler with `component` `libp2p`, so all output
  stays one JSON stream.
- **Explicitly disabled in v0.1:** DHT (no routing), mDNS, circuit relay
  (client and service), AutoRelay, hole punching, NAT port mapping and
  the AutoNAT service (go-libp2p always runs the AutoNAT v1 client; it stays
  inert because no OBIE node serves AutoNAT). go-libp2p's black-hole detectors are disabled too: they suppress
  UDP/IPv6 dials after repeated failures, which must not happen to addresses
  the operator configured. These features are planned for later releases.
- **Key:** the host uses the node identity through an adapter that
  implements libp2p's `crypto.PrivKey` on top of `identity.Identity`: signing
  delegates to `Identity.Sign`, the public key comes from
  `Identity.PublicKey`. The private key therefore still never leaves
  `internal/identity`. `Raw()`, which go-libp2p only uses to derive QUIC
  stateless-reset and token keys, returns key-bound secret material instead
  of the key: the node's (deterministic) Ed25519 signature over a fixed,
  domain-separated string that no OBIE or libp2p protocol ever signs. It is
  stable across restarts, as QUIC requires, and never leaves the process.
- **Bootstrap:** every `mesh.bootstrap` peer is dialed at start and whenever
  it is not connected, by one goroutine per peer. Failed dials are retried
  with exponential backoff (1 s doubling to a 5 min cap) with equal jitter
  (a delay `d` becomes a random value in `[d/2, d)`); after a disconnect the
  node waits one backoff step before redialing, and the backoff is reset only
  once a connection stayed up for the cap, so a flapping peer is not dialed
  in a tight loop. go-libp2p's own per-peer dial backoff is cleared before
  each attempt, since the mesh schedules retries itself. Bootstrap peers are
  protected from connection-manager trimming. Entries with the same peer ID
  are merged into one peer with several addresses; an entry naming the node
  itself is ignored with a warning.
- **Peer view:** the mesh pings connected peers every 15 s to measure
  latency. `GET /v1/peers` on the admin API (and `obiectl peers`) lists the
  connected peers with peer ID, configured name and trust weight (from
  `trust.publishers`, else `trust.default_weight`), remote addresses,
  connected-since (the oldest open connection), latency and whether the peer
  is a bootstrap peer. The admin package defines the wire types and does not
  import `internal/mesh`, so the admin API and its client stay free of
  go-libp2p types; the daemon converts between them.

## Consequences

- The binaries and CI need Go ≥ 1.26.
- go-libp2p registers its Prometheus collectors with the default registry,
  so libp2p metrics appear on `/metrics`.
- A node only knows the peers it dials and the peers that dial it; without
  discovery, two nodes that share a bootstrap peer are not connected to each
  other. Gossip (WP #1656) relays through the bootstrap peers.
- The `Raw()` substitute means the host key cannot be marshaled and re-read
  by go-libp2p (it never needs to: the in-memory peerstore is used); the key
  file remains the only persistent form of the key.
