# ADR 0017: Release packaging and the state directory format

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1665](https://openproject.niew.dev/work_packages/1665)

## Context

OBIE could be built from source, but there was no supported way to install
and run it in production: no release artefacts, no service unit, no
container image. Operators need artefacts they can verify, a sandboxed
service that holds only the privilege the nftables backend needs, a way to
try a mesh locally, and upgrades that cannot silently corrupt a node's
state when a newer version is rolled back.

## Decision

- **Plain Makefile, no GoReleaser.** `make release VERSION=x.y.z` runs
  `packaging/release.sh`, which builds, per platform (linux/amd64,
  linux/arm64), a tarball `obie-<v>-linux-<arch>.tar.gz` with `obied`,
  `obiectl`, `install.sh`, `etc/obie.yaml` (the example configuration),
  `systemd/obied.service`, `fail2ban/action.d/obie.conf`, `LICENSE.md` and
  `README.md`; a CycloneDX JSON SBOM per binary
  (`obie-<v>-linux-<arch>.<binary>.cdx.json`); and `SHA256SUMS` over all of
  them. One more tool (`cyclonedx-gomod`, pinned in the Makefile like the
  linters) is cheaper to own than a GoReleaser configuration.
- **Reproducible.** Binaries are built with `CGO_ENABLED=0`, `-trimpath`
  and `-ldflags=-buildid=`; tar entries are sorted, owned by 0:0 and dated
  `SOURCE_DATE_EPOCH` (default: the commit time), and `gzip -n` stores no
  name or time. The SBOM is read from each binary's embedded build
  information (`cyclonedx-gomod bin`, no serial number, no timestamp), so
  it lists exactly the modules linked in. CI builds the release twice and
  requires identical `SHA256SUMS`.
- **Release workflow on tags only.** `release.yml`, byte-identical in
  `.gitea/workflows/` and `.github/workflows/` like `ci.yml`, triggers only
  on pushed `v*` tags. It runs `make ci`, `make release` and `make
  check-unit`, attaches the artefacts to the forge's release through its
  REST API (`packaging/publish-release.sh`; Gitea and GitHub differ only in
  the API root and the upload call) and pushes the multi-arch image to the
  forge's registry (`ghcr.io` on GitHub). The CI gate builds everything but
  pushes nothing.
- **systemd unit.** `packaging/systemd/obied.service` runs obied as the
  system user `obie` with `CAP_NET_ADMIN` as its only (ambient and bounding)
  capability, `StateDirectory`, `RuntimeDirectory` (admin socket, 0750),
  `ConfigurationDirectory` and `LogsDirectory` named `obie`, and a sandbox
  (`ProtectSystem=strict`, `@system-service` without `@privileged`, no
  namespaces, `MemoryDenyWriteExecute`, …) that `systemd-analyze security`
  rates **1.7 (OK)**. `PrivateUsers` and `PrivateNetwork` are deliberately
  not set: either would take `CAP_NET_ADMIN` away from the host's network
  namespace. `make check-unit` runs `systemd-analyze verify` (any warning
  fails) and requires an exposure of at most 3.0.
- **install.sh** installs an extracted tarball: user and group `obie`,
  binaries into `$PREFIX/bin` (default `/usr/local`), the configuration only
  if there is none (the current example always goes to
  `obie.yaml.example`), the unit and, if Fail2Ban is installed, its action.
  It is idempotent, so a second run is the upgrade; it never starts obied.
  `DESTDIR` stages it for packaging and tests.
- **Container image.** A multi-stage `Dockerfile` cross-compiles on the
  build platform and copies the binaries into
  `gcr.io/distroless/static-debian12:nonroot` with
  `packaging/docker/obie.yaml` (dryrun backend, admin socket group
  `nonroot`, IPv4 listeners) and an `obiectl status` health check. The
  image does not enforce: the nftables backend belongs on the host.
- **Compose lab.** `packaging/compose` runs three nodes on a bridge network
  in `enforce` mode with the dryrun backend. A one-shot `init` service (the
  Dockerfile stage `lab-init`: busybox plus obied) creates each node's key
  once and writes configurations in which every node bootstraps to
  (`/dns4/<node>/tcp/4001/p2p/<id>`) and fully trusts the others.
  `make lab-smoke` starts it, waits until each node has both others as
  peers and until node3 blocks an address reported by node1 and node2, and
  removes it.
- **State directory format.** `<state_dir>/FORMAT` holds the format version
  as a decimal integer; the current format is 1 (`node.key` and `db/`).
  Before loading the identity, obied stamps a directory without the file —
  new, or written before the file existed — with its format, and refuses to
  start on a directory with a higher format, naming the directory, both
  formats and the fix (run the newer obied again or restore a backup). A
  future format change raises the version and migrates older directories
  in `internal/statedir` before anything else opens them.

## Consequences

- Releases are cut by pushing a `v*` tag; nothing is published from branch
  pushes. Verifying a download is `sha256sum -c SHA256SUMS`; signing the
  artefacts is future work (out of scope for v0.1).
- Operators get one supported install path per environment: tarball and
  `install.sh` with systemd (the only one that enforces with nftables), or
  the container image in observe or dryrun setups.
- The CI gate needs Docker, Docker Compose and `systemd-analyze` on its
  runner for the `package` job.
- Downgrading obied across a format change fails loudly instead of
  misreading the store; every format change from now on needs a migration
  and a test in `internal/statedir`.
