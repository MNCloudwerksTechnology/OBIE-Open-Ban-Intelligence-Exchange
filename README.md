# OBIE: Open Ban Intelligence Exchange

**Shared intelligence, sovereign enforcement.** OBIE lets servers you run
tell each other which addresses attack them, and lets each server decide
for itself whether to block them. There is no central service: every
[node](documentation/glossary.md#node) signs what it reports, trusts only the
[peers](documentation/glossary.md#peer) its operator chose, and always has the last
word over its own firewall.

OBIE is at **v0.1**, its first release. It works end to end, but it is
young: read the [threat model](SECURITY.md#threat-model) before you trust
it with a production firewall, and start in
[observe mode](documentation/glossary.md#observe-mode).

## What v0.1 does

- **Reports attacks as signed [verdicts](documentation/glossary.md#verdict).**
  `obiectl report`, or one line in a
  [Fail2Ban](documentation/guides/fail2ban.md) jail, turns a local
  detection into an Ed25519-signed verdict on an IPv4/IPv6 address or CIDR
  range. Log lines given as evidence are hashed on the node; only the hash
  and the counts leave it. `obiectl revoke` withdraws a verdict.
- **Exchanges them with the peers you choose.** Nodes form a libp2p mesh
  with static bootstrap peers and gossip verdicts and
  [revocations](documentation/glossary.md#revocation)
  ([obie/0.1 protocol](documentation/spec/obie-0.1.md)). Invalid events
  are dropped and every publisher and relaying peer is rate-limited.
- **Decides locally by weighted consensus.** Each node gives every
  publisher a [trust weight](documentation/glossary.md#trust-weight) you set
  and blocks an address only when the weighted score reaches a
  [threshold](documentation/glossary.md#threshold) *and* enough distinct
  publishers agree ([quorum](documentation/glossary.md#quorum)).
  `obiectl explain` shows why an address is or is not blocked.
- **Never blocks what you protect.** Loopback, private, link-local and
  documentation ranges, the node's own addresses and its bootstrap peers
  are always allowed; you add your own networks. `obiectl allow` and
  `obiectl block` overrule the mesh for any address.
- **Observes before it enforces.** A node starts in `observe` mode and
  only shows what it would block. In `enforce` mode it keeps the
  [nftables](documentation/guides/nftables.md) table `inet obie` exactly in
  line with its decisions, and never touches any other table.
- **Is operable.** Prometheus metrics, health endpoints, a JSON audit log
  of every decision for your SIEM, a
  [Grafana dashboard](documentation/operations/monitoring.md), static
  binaries with checksums and SBOMs, a hardened systemd unit and a
  container image.

## What it does not do yet

- **No discovery.** Peers are configured by hand (peer ID and address);
  there is no DHT and no public mesh to join.
- **No computed reputation.** Trust is a number you assign per peer; there
  is no scoring by accuracy, no warm-up, no ASN or organisation diversity.
- **IP addresses only.** No domains, URLs, TLS/SSH fingerprints or file
  hashes; no per-protocol topics.
- **No key rotation or appeals.** A lost or stolen key means a new
  identity; the subject of a block cannot appeal through the mesh.
- **Linux only, nftables only.** No eBPF, iptables-legacy, BSD or Windows
  enforcement. The container image does not drive nftables.

The [whitepaper](documentation/whitepaper.md) describes where OBIE is
heading; [ARCHITECTURE.md](ARCHITECTURE.md#deviations-from-the-whitepaper)
lists how v0.1 differs from it.

## Install in 5 commands

On a Linux host with systemd (amd64; use `arm64` in the file name for
ARM):

```sh
curl -fL -O https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v0.1.0/obie-0.1.0-linux-amd64.tar.gz
curl -fL -O https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v0.1.0/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf obie-0.1.0-linux-amd64.tar.gz
sudo ./obie-0.1.0-linux-amd64/install.sh
```

The node is installed but not started, in observe mode. The
[quick start](documentation/operations/quickstart.md) takes it from here:
start it, connect Fail2Ban, check it works and switch to enforcement. To
try a three-node mesh on a laptop instead, run the
[compose lab](packaging/compose/README.md).

## Documentation

| For | Read |
|-----|------|
| First node, step by step | [Quick start](documentation/operations/quickstart.md) |
| Every configuration key | [Configuration reference](documentation/operations/configuration.md) |
| Connecting to other nodes | [Federation](documentation/operations/federation.md) |
| Day-2: metrics, audit log, upgrades, backup, uninstall | [Operations](documentation/operations/operations.md), [Monitoring](documentation/operations/monitoring.md) |
| Install options (tarball, container, lab) | [Installing and upgrading](documentation/operations/install.md) |
| When something is wrong | [Troubleshooting](documentation/operations/troubleshooting.md) |
| Fail2Ban and nftables | [Fail2Ban guide](documentation/guides/fail2ban.md), [nftables guide](documentation/guides/nftables.md) |
| Risks and reporting a vulnerability | [SECURITY.md](SECURITY.md) |
| What changed | [CHANGELOG.md](CHANGELOG.md) |
| The wire protocol | [obie/0.1 specification](documentation/spec/obie-0.1.md) |
| Design and decisions | [ARCHITECTURE.md](ARCHITECTURE.md), [ADRs](documentation/adr/) |
| The vision | [Whitepaper](documentation/whitepaper.md) |

## Build from source

Requires Go 1.26 or newer and GNU Make:

```sh
make build           # static obied and obiectl in ./bin/
make ci              # every check a change must pass
make release VERSION=0.1.0   # reproducible tarballs, SBOMs, SHA256SUMS in dist/release/
make image           # container image obie:<version>
make lab-smoke       # three-node compose lab: start, check, remove
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to contribute. OBIE is
released under the [MIT License](LICENSE.md).
