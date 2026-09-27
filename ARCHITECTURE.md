# OBIE Architecture

This document is the binding technical baseline for the OBIE reference
implementation. Every work package builds on it; any change to the baseline
must update this file **and** be recorded as a new ADR in
[`documentation/adr/`](documentation/adr/). The decision record for the
initial baseline is
[ADR 0001](documentation/adr/0001-architecture-baseline.md).

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
- **Storage:** BadgerDB v4 in the state dir.
- **Mesh:** go-libp2p (TCP + QUIC, Noise), GossipSub topic `obie/0.1/verdicts`, static bootstrap peers in v0.1.
- **Decision:** operator-assigned per-publisher trust weights; `score = Σ weight(publisher) × confidence` over distinct publishers' latest active verdicts; enforce iff score ≥ threshold (default 1.8) AND distinct publishers ≥ quorum (default 2) — local verdicts count with `local_weight`. Allow-list always wins. Mode `observe` (default) or `enforce`.
- **Enforcement:** pluggable enforcer; `dryrun` and `nftables` (own table `inet obie`, timeout sets) backends; reconcile loop.
- **Ops:** Prometheus `/metrics`, `/healthz`, `/readyz` on a separate listen address (default `127.0.0.1:9464`); JSON decision audit log.
- **Testing:** table-driven unit tests, fuzz tests on all decoders, in-process multi-node integration tests; privileged tests behind the `privileged` build tag.

## Repository layout

```text
cmd/
  obied/            node daemon entry point
  obiectl/          operator CLI entry point
internal/           all non-public code (one package per concern listed above)
  cli/              shared flag handling for the binaries
  version/          build version, injected via -ldflags
pkg/
  obieproto/        public protocol types + sign/verify (importable by third parties)
documentation/
  adr/              architecture decision records
  spec/             obie/0.1 protocol specification
diagrams/           whitepaper diagrams (PlantUML sources + PNG)
```

Only the packages that exist today are listed in detail; the remaining
`internal/` packages are added by the work packages that need them.

## Conventions

- **Entry points stay thin.** `main()` only wires `os.Args`, stdio and the exit
  code into testable code under `internal/`.
- **Versioning.** Both binaries share `internal/version.Version`, which defaults
  to `dev` and is set at build time with
  `-ldflags "-X github.com/MNCloudwerksTechnology/obie/internal/version.Version=<v>"`
  (`make build` does this from `git describe`).
- **Static binaries.** Builds use `CGO_ENABLED=0`; the target platforms are
  Linux amd64 and arm64.
- **Quality gate.** `make ci` (gofmt check, `go vet`, golangci-lint,
  race-enabled tests, govulncheck, actionlint on the CI workflows) must pass
  before every commit. Tool versions are pinned in the `Makefile`. The CI
  pipeline (`.gitea/workflows/ci.yml`, mirrored byte-identical to
  `.github/workflows/ci.yml`) runs the same `make ci` on every pull request and
  every push to `develop`/`main`, plus a linux/amd64 + arm64 build matrix.

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
