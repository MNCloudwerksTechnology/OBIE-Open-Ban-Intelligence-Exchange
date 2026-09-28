# Changelog

All notable changes to OBIE are recorded in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and OBIE uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

The first release, v0.1.0 "Stable Base".

### Added

- **Protocol.** The [obie/0.1 specification](documentation/spec/obie-0.1.md)
  with a JSON Schema and test vectors: signed verdicts and revocations on
  IPv4/IPv6 addresses and CIDR ranges (at most /16 and /32), Ed25519
  signatures over RFC 8785 canonical JSON, and a reference implementation
  in `pkg/obieproto`.
- **Node identity.** An Ed25519 key per node, created on first start, whose
  peer ID names the node on the mesh (`obied keygen`, `obied identity`,
  `obiectl identity`); `obied` refuses key files others can read.
- **Mesh.** A libp2p host (TCP and QUIC) with static bootstrap peers that
  are redialled with backoff, GossipSub on the topic `obie/0.1/verdicts`,
  validation of every received event, peer scoring, and rate limits per
  publisher and per forwarding peer (`mesh.rate_limit`).
- **Event store.** A local store of verdicts, revocations and operator
  overrides that drops duplicates, stale and expired events and keeps
  revocations for as long as the verdicts they revoke.
- **Reporting.** `obiectl report` turns a local detection into a signed
  verdict, hashing the evidence log lines on the node;
  `obiectl revoke` withdraws it. A repeated report on the same address
  refreshes the verdict instead of publishing another.
- **Fail2Ban action.** `contrib/fail2ban/action.d/obie.conf` reports every
  ban of a jail with one extra line in the jail
  ([guide](documentation/guides/fail2ban.md)).
- **Trust-weighted decisions.** Each node weights every publisher
  (`trust.publishers`) and blocks an address when the score reaches
  `decision.threshold` and `decision.quorum` publishers agree; the node's
  own verdicts can block at once (`decision.local_autoblock`).
  `obiectl explain` (with every verdict's reason), `decisions`,
  `indicators` and `show` show why.
- **Local sovereignty.** A built-in allow-list (loopback, private,
  special-purpose ranges, own and bootstrap addresses) plus
  `allowlist.cidrs` and `allowlist.files`; operator overrides
  `obiectl allow`, `block` and `unoverride`; `observe` and `enforce`
  modes; configuration reload on SIGHUP.
- **Enforcement.** A reconciler that keeps the nftables table `inet obie`
  exactly in line with the decisions, with per-element timeouts and a cap
  of `enforce.max_entries`; a `dryrun` backend; `obiectl enforced` and
  `obied teardown-firewall` ([nftables guide](documentation/guides/nftables.md)).
- **Observability.** Prometheus metrics, `/healthz` and `/readyz`, a
  Grafana dashboard, and a JSON-lines decision audit log with Elastic
  Common Schema fields ([monitoring](documentation/operations/monitoring.md)).
- **Admin API and CLI.** A local Unix-socket API restricted to root, the
  service user and the `obie` group, and `obiectl` on top of it
  (`status`, `peers` and the commands above).
- **Web console.** An opt-in, read-only browser view of the node
  (`console.enabled`, switched on and off by a reload), listening on a
  loopback address only, for the users of `obiectl` only and behind a
  token kept in `obied`'s memory (`obiectl console`, `--rotate`); it shows
  the node's health on every page and never stops the node
  ([web console](documentation/operations/console.md),
  [ADR 0019](documentation/adr/0019-local-web-console.md)). Its overview
  shows the node's identity, mode, uptime and configuration load, the
  readiness of every part, the key numbers (peers, indicators, decisions
  by state, applied firewall entries, overrides) and the conditions that
  need attention with a next step, explains what will appear on a node
  that has just started, and refreshes itself every 5 seconds
  ([ADR 0020](documentation/adr/0020-console-overview.md)). Its peers view
  lists every configured and connected peer — bootstrap peers, trusted
  publishers, peers that connected on their own — with its connection
  (since when, last seen, the last failed dial), its trust weight (and
  whether it has any influence on decisions), the verdicts the node holds
  and counts from it, and the events it sent in the last hour, accepted
  or rejected and why; it filters, sorts and pages on the node, and a
  peer's page lists the verdicts the node holds from it
  ([ADR 0021](documentation/adr/0021-console-peers.md)). Its decisions
  view lists every address and network the node decided on — state, score
  against threshold, publishers against quorum, reason, when decided and
  until when, and whether the firewall applies it and if not why — filters
  by state, reason, publisher and firewall, searches by address (an
  address finds the networks around it), sorts and pages on the node, fast
  with 1,000,000 decisions; an address's page explains it like
  `obiectl explain`, also one the node knows nothing about, and refreshes
  itself. Its firewall view lists what the backend applies and every
  difference from the decided blocks, and says in observe mode that
  nothing is applied by design. Addresses can be copied, and every view
  shared as a link on the same host
  ([ADR 0022](documentation/adr/0022-console-decisions-and-firewall.md)).
  Its verdicts view lists the verdicts this node published and those it
  holds from every other publisher — address, publisher and its trust
  weight (marked *No weight* at 0), action, confidence, reason, event
  count and the evidence's log hash, issue and expiry, and whether it
  counts — with totals per publisher; it filters by publisher, reason and
  address (the verdicts on one address, as `obiectl show` gives them),
  shows revoked verdicts with why and expired ones on request, and links
  every verdict to its publisher and its decision. The store now keeps
  revoked and expired verdicts for 24 hours after their expiry, with the
  revocation's reason — of other publishers at most a tenth of
  `store.max_indicators`
  ([ADR 0023](documentation/adr/0023-console-verdicts.md)).
- **Packaging.** Reproducible static release tarballs for linux/amd64 and
  linux/arm64 with CycloneDX SBOMs and `SHA256SUMS`, `install.sh`, a
  hardened systemd unit, a distroless container image, and a three-node
  compose lab with a smoke test ([install](documentation/operations/install.md)).
- **Testing.** An end-to-end test of report → block under quorum → revoke
  across several nodes, with an nftables variant in network namespaces.
- **Documentation.** A [quick start](documentation/operations/quickstart.md)
  whose every command is mapped to a test, a
  [configuration reference](documentation/operations/configuration.md)
  tested against the code, [federation](documentation/operations/federation.md),
  [operations](documentation/operations/operations.md) and
  [troubleshooting](documentation/operations/troubleshooting.md) guides,
  and a [threat model](SECURITY.md#threat-model). The
  [whitepaper](documentation/whitepaper.md) moved out of the README.

[Unreleased]: https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/commits/develop
