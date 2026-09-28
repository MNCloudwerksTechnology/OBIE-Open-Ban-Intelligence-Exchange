# Security policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.1.x | yes: security fixes are released as 0.1.x patch releases |
| builds of `develop` and `main` before v0.1.0 | no |

OBIE v0.1 is a first release. Read the [threat model](#threat-model)
below before you let it enforce on a production host, and start in
observe mode ([quick start](documentation/operations/quickstart.md)).
Fixes land on `develop` first and are released from there.

## Reporting a vulnerability

Please do not describe a vulnerability in a public issue, pull request or
discussion.

1. Report it privately through GitHub: the repository's **Security** tab →
   **Report a vulnerability**
   ([private advisory](https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/security/advisories/new)).
   If that is not available to you, open an issue titled "Security
   contact request" that contains no details about the problem; a
   maintainer will reply with a private way to send the report.
2. Include the affected version (`obied --version`), what an attacker
   needs (a trusted key? a mesh connection? local access?), and how to
   reproduce it.
3. We confirm receipt within 5 working days and agree on a disclosure date
   with you, by default at most 90 days after the report. We credit you in
   the advisory and the [changelog](CHANGELOG.md) unless you prefer not.

In scope: the node (`obied`, `obiectl`), the Fail2Ban action, the release
artefacts (tarballs, systemd unit, container image), the obie/0.1 protocol
specification and its reference implementation in `pkg/obieproto`, and the
website in `website/`. The threats below that v0.1 knowingly leaves open
("remaining risk") are not vulnerabilities by themselves, but ways to
exploit them beyond what is described are.

## Threat model

### What OBIE protects and whom it trusts

A node holds an **identity key** (`/var/lib/obie/node.key`), controls an
**nftables table** (`inet obie`) on its host, and receives signed
**verdicts** ("block this address") from the peers of its mesh. Its job is
to block attackers without ever blocking what its operator needs.

- **The operator** is fully trusted: whoever has root or is in the group
  `obie` can publish verdicts, set overrides and read the configuration.
- **Trusted publishers** are the peers listed in `trust.publishers`, and
  are trusted *only as far as their weight*: none of them alone should be
  able to cut the host off.
- **Everyone else on the network** is untrusted, including peers that
  connect to the mesh port, relay events or create keys.

The attacker may run any number of nodes, connect to the mesh port of
every node, replay anything they have seen, and may have stolen the key
of, or be the operator of, a trusted publisher. The protocol-level
analysis is in the specification's [security
considerations](documentation/spec/obie-0.1.md#14-security-considerations).

### Poisoning by a trusted peer

A trusted publisher (malicious, compromised or simply wrong) reports
addresses that are not attackers, to get them blocked on your host: a
competitor, a customer, a payment provider.

**v0.1 mitigation.** A remote verdict counts only with its publisher's
weight, and an address is blocked only when the weighted score reaches
`decision.threshold` *and* `decision.quorum` distinct publishers agree
(defaults 1.8 and 2: at least three agreeing publishers at weight 0.8).
The allow-list and `obiectl allow` always win. Verdicts cannot target
private, special-purpose or documentation ranges, nor CIDR ranges wider
than /16 (IPv4) or /32 (IPv6). Every block ends after at most
`decision.max_ttl` (30 days). `obiectl explain` and the audit log show
which publishers caused every block, and removing a publisher from
`trust.publishers` with a reload lifts its blocks at once.

**Remaining risk.** Trust is a static number: there is no reputation, no
accuracy tracking and no cap on how many addresses one publisher may get
blocked. With `quorum: 1`, or enough colluding trusted publishers, the
attacker blocks any public address or /16 range on your host until you
notice. The subject of a block cannot appeal through the mesh.

### Sybil peers

The attacker creates many keys and nodes, so that "many publishers agree".

**v0.1 mitigation.** Keys cost nothing, so they are worth nothing:
publishers not in `trust.publishers` have `trust.default_weight`, 0 by
default, and a publisher with weight 0 neither adds to the score nor
counts towards the quorum. Nodes only dial the peers you configure; there
is no discovery. Every connected peer's forwarded events are
rate-limited, and peers that forward invalid events are scored down and
eventually ignored by GossipSub.

**Remaining risk.** Any node that reaches the mesh port can connect and
gossip; v0.1 has no connection allow-list, so Sybil nodes cost you
bandwidth, CPU and storage (see resource exhaustion). Their verdicts are
stored and relayed, only never counted. Setting `trust.default_weight`
above 0 hands the decision to whoever creates the most keys. Diversity
checks (ASN, organisation) do not exist yet.

### Replay

The attacker re-sends old signed events: an expired or revoked verdict, to
get an address blocked again.

**v0.1 mitigation.** Every event has a unique ID (UUIDv7) and is signed as
a whole; a known ID is dropped as a duplicate. Expired events are dropped,
and events dated more than five minutes in the future are ignored. A
revocation outlives the verdict it revokes, so a revoked verdict stays
revoked, and an older verdict never replaces a newer one of the same
publisher. Only a verdict's own publisher can revoke it.

**Remaining risk.** Replay works within a verdict's lifetime on a node
that never saw its revocation: v0.1 has no catch-up, so a node that was
disconnected when the revocation was published keeps, or can be sent
again, the revoked verdict until it expires. The same holds after the
event store (`/var/lib/obie/db`) is lost. Nodes need synchronised clocks;
a clock far off drops valid events or keeps expired ones.

### Key theft

The attacker reads a node's `node.key` and publishes (or revokes) verdicts
in its name, with the weight its peers give it.

**v0.1 mitigation.** The key is created with mode 0600 in a 0700 state
directory owned by the service user `obie`; `obied` refuses to start with a
key that another user owns or that group or others may read, and with a
state directory others may write. The systemd unit runs `obied`
unprivileged, with only `CAP_NET_ADMIN` and a strict sandbox. The key
never leaves the host; peers only see signatures.

**Remaining risk.** Anyone with root on the host has the key. v0.1 has no
key rotation and no way to revoke a key on the mesh: after a theft the
operator creates a new key ([operations](documentation/operations/operations.md#back-up-the-node-key))
and every peer must remove the old peer ID from `trust.publishers` by
hand. Until they do, the thief is as trusted as you were.

### Resource exhaustion

The attacker floods a node with events, connections or oversized
messages, to exhaust CPU, memory, disk or the firewall, or to push real
blocks out.

**v0.1 mitigation.** Events larger than 4 KiB are rejected before the
signature check. Token buckets limit the events accepted per publisher
(10/s, burst 50) and per connected peer (50/s, burst 250); events beyond
them are dropped and not relayed. libp2p's resource manager bounds
connections and streams, and the connection manager trims them back when
there are more than 128. Events expire from the store with their TTL. The firewall holds
at most `enforce.max_entries` (100,000) blocks, and when full, drops the
lowest scores first; nftables sets are kernel hash sets with per-element
timeouts. The admin API is a Unix socket, reachable only locally.

**Remaining risk.** The store has no size cap: its growth is bounded only
by the rate limits and the TTL, and new keys bypass the per-publisher
limit, so a patient attacker with many connections can fill the disk.
Keep the mesh port open only to your peers. A flood of well-formed events
from a trusted publisher can fill `enforce.max_entries` with its blocks;
alert on `obie_enforcer_skipped_total`.

### Self-DoS via allow-list gaps

OBIE blocks something the host needs: the operator's own address, a
monitoring system, a DNS resolver, the upstream gateway, a CDN or a peer.

**v0.1 mitigation.** Always allowed and beyond even a force-block:
loopback, private (RFC 1918), CGNAT, link-local, ULA, multicast,
unspecified and documentation ranges, the node's own listen and interface
addresses, and the IPs of its bootstrap peers. The operator adds
`allowlist.cidrs` and `allowlist.files`; `obiectl allow` exempts any
address at once. Nodes start in observe mode, and with the default
threshold and quorum no single remote peer can block anything. `obied teardown-firewall` or `nft delete table inet
obie` removes every block, since OBIE only ever touches its own table
([locked out](documentation/operations/troubleshooting.md#locked-out)).

**Remaining risk.** OBIE cannot know what you depend on: every address
that is not in the built-in ranges or your allow-list can be blocked. A
public address behind NAT is not detected as the node's own. A bootstrap
peer given by DNS name is only protected under the addresses it resolved
to at the last start or reload. Shared addresses (NAT gateways, proxies,
cloud egress) are blocked for everyone behind them. `obiectl report` and
the Fail2Ban action refuse only addresses in `allowlist.cidrs` as loaded
at start, so an address in `allowlist.files` or added with a reload can
still be reported to your peers, and blocked on theirs.

### Local web console

Someone other than the operator reads the node through the web console:
an attacker on the network, another user of the host, or a web page open
in the operator's browser that forges requests or rebinds a DNS name to
the host.

**v0.1 mitigation.** The console is off unless `console.enabled` is set,
and listens only on a loopback address; any other `console.listen` is a
configuration error. It serves only root, the service user and members of
the group `obie` — the connecting process's user is read from the
kernel's socket table — and only browsers signed in with a 256-bit token
that `obied` keeps in memory and hands out only through the admin socket
(`obiectl console`; `--rotate` replaces it and ends every session, as
does every restart). Sessions are HMAC-signed, `HttpOnly`,
`SameSite=Strict` cookies that last 12 hours. Requests addressed to
another host name (DNS rebinding), requests from other sites or other
local ports (Fetch Metadata, `Origin`) and framing are refused; a strict
content security policy allows no other origin and no inline code. The
console is read-only, loads nothing from outside the node, and never
stops or degrades the node. Pages that refresh themselves fetch only the
console's own escaped template output, behind the same session, and
parse it into an inert document before showing it. Details and the
threats considered are in
[ADR 0019](documentation/adr/0019-local-web-console.md),
[ADR 0020](documentation/adr/0020-console-overview.md),
[ADR 0021](documentation/adr/0021-console-peers.md) and
[ADR 0022](documentation/adr/0022-console-decisions-and-firewall.md); the
peers view shows peer names, addresses and dial errors as received from
the configuration and the network, and the decisions views show the
reasons and protocols of verdicts as received, escaped like everything
else. The copy buttons write only text to the clipboard; a shared link
holds no secret and still needs a session.

**Remaining risk.** Through an SSH port forward, every user of the
operator's workstation can reach the forwarded port, and on the node the
connection counts as the operator's login user: only the token protects
it there. Cookies are not isolated by port, so on the workstation the
session cookie also reaches other servers on its loopback interface. Use
a workstation you control, sign out, and rotate the token when in doubt.
On platforms without the Linux socket table, only the token protects the
console. The console speaks plain HTTP; on the host, loopback traffic is
visible to root only.

### Privacy leakage

Verdicts and the mesh reveal more than the operator intends: about the
reported addresses, about the operator's own infrastructure, or log
content.

**v0.1 mitigation.** Log lines given as evidence (Fail2Ban's matched
lines) are hashed on the node and dropped; only the SHA-256 and the number
of events are published, never the lines, user names or host names. A
verdict carries the indicator, protocol, reason, event count, confidence,
TTL, optional MITRE technique IDs and the publisher's peer ID, and is only
sent to connected peers. Metrics labels never carry IP addresses or peer
IDs. Private addresses are never published.

**Remaining risk.** Every verdict tells your peers that your node, at that
time, was attacked on that protocol, and so which services you run. The
log hash is not salted: a peer that can guess the log lines (a common
user name, a known attack) can confirm the guess. Reported IP addresses
are personal data in many jurisdictions; sharing them is your
responsibility as operator. Peers learn your node's IP address, listen
addresses and `obied` version (libp2p identify). Connections are encrypted,
but events are not: every peer that relays an event can read it.
