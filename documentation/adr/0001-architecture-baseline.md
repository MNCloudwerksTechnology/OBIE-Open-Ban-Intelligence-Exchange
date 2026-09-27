# ADR 0001: Architecture baseline for v0.1

- **Status:** Accepted
- **Date:** 2026-09-27
- **Work package:** [#1646](https://openproject.niew.dev/work_packages/1646)

## Context

The repository started as a whitepaper only. Before the first functional work
packages (configuration, daemon lifecycle, event model, mesh, decision,
enforcement) are implemented, all of them need one shared set of technical
decisions — otherwise each package would pick its own formats, libraries and
defaults, and the pieces would not fit together.

The whitepaper describes a broad long-term vision (DHT discovery, computed
reputation, OPA policies, CBOR/COSE, eBPF). v0.1 must be small enough to ship
and for "a competent engineer to deploy in a weekend", while staying
compatible with that vision.

## Decision

We adopt the following baseline for v0.1. It is mirrored in
[`ARCHITECTURE.md`](../../ARCHITECTURE.md), which is kept current as the
single reference.

- **Binaries:** `obied` daemon; `obiectl` talks to it over a local admin API (HTTP/JSON on a Unix socket, default `/run/obie/obie.sock`, mode 0660, group `obie`).
- **Layout:** `cmd/` entry points; `internal/` for everything (config, logging, event, crypto/identity, store, mesh, admin API, ingest, decision, enforce, metrics, audit); `pkg/obieproto` only for the public protocol types + sign/verify that third parties may import. `documentation/spec/` for the protocol spec.
- **Config:** one YAML file (default `/etc/obie/obie.yaml`), strictly validated (unknown keys are errors). State dir default `/var/lib/obie`.
- **Logging:** `log/slog` JSON to stderr.
- **Events:** obie/0.1 JSON; signatures are Ed25519 over the RFC 8785 (JCS) canonical form of the event with `publisher.signature` removed; `signature` = `"ed25519:" + base64url(no padding)`. IDs are UUIDv7.
- **Identity:** one Ed25519 key per node; the libp2p peer ID is derived from it (same key for mesh and event signing).
- **Storage:** BadgerDB v4 in the state dir.
- **Mesh:** go-libp2p (TCP + QUIC, Noise), GossipSub topic `obie/0.1/verdicts`, static bootstrap peers in v0.1.
- **Decision:** operator-assigned per-publisher trust weights; `score = Σ weight(publisher) × confidence` over distinct publishers' latest active verdicts; enforce iff score ≥ threshold (default 1.8) AND distinct publishers ≥ quorum (default 2) — local verdicts count with `local_weight`. Allow-list always wins. Mode `observe` (default) or `enforce`.
- **Enforcement:** pluggable enforcer; `dryrun` and `nftables` (own table `inet obie`, timeout sets) backends; reconcile loop.
- **Ops:** Prometheus `/metrics`, `/healthz`, `/readyz` on a separate listen address (default `127.0.0.1:9464`); JSON decision audit log.
- **Testing:** table-driven unit tests, fuzz tests on all decoders, in-process multi-node integration tests; privileged tests behind the `privileged` build tag.

Build tooling: Go module `github.com/MNCloudwerksTechnology/obie` (Go ≥ 1.23),
static `CGO_ENABLED=0` builds, and a `make ci` quality gate (gofmt, vet,
golangci-lint, race tests, govulncheck) with tool versions pinned in the
`Makefile`.

## Consequences

- Later work packages implement against this baseline and must not diverge
  silently; a change requires updating `ARCHITECTURE.md` and a superseding ADR.
- Keeping everything in `internal/` except `pkg/obieproto` lets us refactor
  freely while giving third parties a stable, minimal import surface for the
  protocol.
- One Ed25519 key for both libp2p and event signing keeps key management
  simple, at the cost of coupling mesh identity to publisher identity.
- JCS canonicalisation makes signatures independent of JSON encoder details,
  so non-Go implementations can verify events.
- Operator-assigned weights and static bootstrap peers are deliberately simple;
  computed reputation and DHT discovery are deferred, not rejected.
- Starting in `observe` mode by default means a fresh node never blocks traffic
  until the operator opts into `enforce`.
