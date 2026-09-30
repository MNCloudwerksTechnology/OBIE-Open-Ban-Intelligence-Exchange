# Configuration reference

`obied` reads one YAML file, `/etc/obie/obie.yaml` by default (`obied
--config <file>` for another). This page lists every key. The annotated
[example configuration](../examples/obie.yaml), which `install.sh` installs
as `/etc/obie/obie.yaml` and `/etc/obie/obie.yaml.example`, spells out the
same keys with their defaults. `obied setup` replaces it with a short file
that sets only the keys its questions cover
([Set up and check a node](setup.md)).

A test (`TestConfigurationReferenceDocumentsEveryKey` in
`internal/config`) keeps this page honest: every key of the configuration
has a row here, every row names a real key, and every default below is
parsed and compared with the built-in default.

## Rules for every key

- **Every key is optional.** An omitted key takes its default; an empty
  file configures the same node as the example.
- **Mistakes are errors.** Unknown keys, values of the wrong type and
  invalid values stop `obied` from starting. The error names the key path,
  e.g. `trust.publishers[1].weight`. Check a file before you use it:

  ```sh
  sudo obied --config /etc/obie/obie.yaml --check-config
  ```

- **Reload or restart.** `sudo systemctl reload obied` (SIGHUP) applies the
  keys marked *reload* at once. Keys marked *restart* need `sudo systemctl
  restart obied`; a reload logs them as `configuration changes that need a
  restart were not applied`. An invalid file or allow-list file is rejected
  on reload, and the running configuration is kept.
- **See what runs.** The [web console](console.md#the-configuration-view)
  shows the configuration the node runs with, every key marked *Default*
  where the file does not set it, when it was loaded, whether the last
  reload succeeded, and which changes in the file on disk wait for a reload
  or a restart.
- **Durations** are Go durations (`10s`, `90m`, `36h`) or whole days
  (`7d`).
- **Paths** must be absolute.

## node

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `node.state_dir` | `/var/lib/obie` | restart | Directory of the persistent state: the identity key `node.key`, the event store `db/` and the format marker `FORMAT`. Created with mode 0700. Back it up (see [Operations](operations.md#back-up-the-node-key)). |
| `node.mode` | `observe` | reload | `observe`: decide, log and show decisions, block nothing. `enforce`: also apply the blocks with `enforce.backend`. Switching back to `observe` removes every block. |
| `node.shutdown_timeout` | `10s` | restart | Longest time a graceful shutdown may take before `obied` stops waiting for its subsystems and exits non-zero. Must be greater than 0. |

## admin

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `admin.socket` | `/run/obie/obie.sock` | restart | Unix socket of the local admin API that `obiectl` talks to (`obiectl --socket`). Its directory must exist. |
| `admin.socket_group` | `obie` | restart | Group that owns the socket; root, the user `obied` runs as and members of this group may use `obiectl`. Under the shipped systemd unit it must equal the unit's `Group=` (see [install.md](install.md#the-service-sandbox)). Must not be empty. |

## mesh

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `mesh.listen` | `[/ip4/0.0.0.0/tcp/4001, /ip4/0.0.0.0/udp/4001/quic-v1, /ip6/::/tcp/4001, /ip6/::/udp/4001/quic-v1]` | restart | libp2p multiaddrs to listen on (TCP and QUIC on port 4001). They must not contain `/p2p/<peer-id>`. Open these ports in your firewall for the peers you federate with. |
| `mesh.bootstrap` | `[]` | restart | Peers to connect to and keep connected, each a transport address ending in the peer's ID, e.g. `/dns4/obie.example.org/tcp/4001/p2p/12D3KooW…`. The IPs of these peers are never blocked. See [Federation](federation.md). |
| `mesh.rate_limit.publisher.events_per_second` | `10` | restart | Events accepted per second, on average, that were signed by one publisher. Greater than 0. |
| `mesh.rate_limit.publisher.burst` | `50` | restart | Events one publisher may send at once. At least 1. |
| `mesh.rate_limit.peer.events_per_second` | `50` | restart | Events accepted per second, on average, that one directly connected peer forwards (for every publisher together). Keep it well above the publisher limit. Greater than 0. |
| `mesh.rate_limit.peer.burst` | `250` | restart | Events one connected peer may forward at once. At least 1. |
| `mesh.trace_path` | `""` | restart | Per-event trace for simulations and short diagnostics; empty (the default) switches it off. An absolute file path in an existing directory: the node appends one JSON line for every copy of an event it receives and every event it publishes — its peer ID (`node`), the event ID (`event`), the peer that forwarded it (`from`; the node itself for its own), when (`at`, to the nanosecond) and what became of it (`outcome`: `published`, a validation outcome such as `accepted` or `duplicate`, or a reason GossipSub dropped it such as `queue_full`). Joined across nodes, the files show the hops and path each event took ([ADR 0032](../adr/0032-gossip-instrumentation-and-attribution.md)). About 230 bytes per copy; the node never rotates or reads the file. It names peers, not addresses. |

Events beyond a rate limit are dropped and not relayed, and counted as
`obie_events_received_total{outcome="rate_limited"}`. The peer that
forwarded them is not penalised. An event counts against both limits only
if both admit it.

## store

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `store.max_indicators` | `1000000` | restart | Most verdicts the event store holds, one per publisher and indicator, so a flood of unique indicators — even from a trusted peer — cannot fill the disk. When the store is full, the verdict that expires first makes room (counted in `obie_store_evictions_total`); a new verdict that would expire before all stored ones is refused instead. This node's own verdicts are never evicted. Each verdict takes about 0.7 KiB of disk and 3 KiB of memory: at the default, about 0.6 GiB of disk and 2.5 GiB of memory, 3.6 GiB at the peak ([measured](performance.md#resource-usage-of-one-node)). The store also keeps verdicts that were revoked or expired for `store.ended_retention`: of other publishers at most a tenth of `store.max_indicators` (at least 1,000) revoked ones and as many expired ones, and all of this node's own. At least 1. See [Monitoring](monitoring.md). |
| `store.ended_retention` | `30d` | restart | How long a verdict is kept after it was revoked or expired, counted from its expiry, so that the blocks it caused can still be traced back to it: the console's [verdicts view](console.md#the-verdicts-view) shows it, and the audit log's `obie.contributors` name it. Each ended verdict takes about 1 KiB of disk. Other publishers' ended verdicts stay within the cap above, at most about 200 MiB at the default `store.max_indicators`, whatever the retention; a longer retention only fills that cap sooner — at the defaults, once more than about 3,300 verdicts a day end in one state. Once it is full, newer ones are not kept until older ones are forgotten (the store logs a warning), so the verdicts view then shows the ones that ended first rather than the latest; the audit log's `obie.contributors` name them either way. This node's own are never capped: a node that reports 1,000 verdicts a day keeps about 30,000 of them at the default, about 30 MiB (1 MiB with `1d`). Entries written with an earlier retention keep theirs. From `1h` to `365d`. |

## trust

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `trust.publishers` | `[]` | reload | The publishers whose verdicts count, each `{peer_id, name, weight}`: `peer_id` is the publisher's peer ID (unique in the list), `name` a label for you (not empty), `weight` between 0 (ignored) and 1 (fully trusted). All three are required. |
| `trust.default_weight` | `0` | reload | Weight of publishers not listed in `trust.publishers`, 0 to 1. Keep 0: any stranger can create keys. |
| `trust.local_weight` | `1.0` | reload | Weight of this node's own verdicts, 0 to 1. 0 means the node's own reports only count for its peers. |

## decision

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `decision.threshold` | `1.8` | reload | An address is blocked when its score reaches this value. The score is the sum of weight × confidence over the latest active verdict of each distinct publisher, where only `ban` verdicts count. Greater than 0. |
| `decision.quorum` | `2` | reload | …and at least this many distinct publishers with a weight above 0 reported it. At least 1. |
| `decision.local_autoblock` | `true` | reload | `true`: this node's own `ban` verdicts block without threshold and quorum, so a local detection protects this host at once (only while `trust.local_weight` > 0). |
| `decision.max_ttl` | `30d` | reload | Longest a block may last from the moment it is decided, whatever the verdicts request. Also caps the TTL of the verdicts this node reports (`obiectl report`, Fail2Ban); that cap only changes on a restart. Greater than 0. |
| `decision.default_ttl` | `7d` | restart | TTL of the verdicts this node reports without one: `obiectl report` without `--ttl`, permanent Fail2Ban bans. A reload does not apply it, and logs a change as needing a restart. Greater than 0 and at most `decision.max_ttl`. |

`watch` verdicts are shown by `obiectl explain` but never count. The
allow-list always wins over the score. How to choose these values for a
federation is described in [Federation](federation.md#choose-trust-weights-and-quorum).

## allowlist

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `allowlist.cidrs` | `[]` | reload | Networks never blocked, in CIDR notation without host bits, e.g. `[192.0.2.0/24, 2001:db8::/32]`. Add your management networks, monitoring, DNS resolvers, upstream gateways and this node's public address if it is not on an interface (NAT). `obiectl report` refuses to report these networks, but checks the list loaded at start until a restart. |
| `allowlist.files` | `[]` | reload | Absolute paths of files with one address or CIDR range per line (blank lines and `#` comments allowed). Re-read on every reload; a missing file or invalid line stops `obied` from starting, and a reload with one is rejected. Unlike `allowlist.cidrs`, these entries do not stop `obiectl report` from reporting an address to your peers. |

Always allowed, whatever the configuration: loopback, private (RFC 1918),
CGNAT (100.64.0.0/10), link-local, ULA (fc00::/7), multicast, unspecified
and documentation ranges, this node's listen and interface addresses and
the IPs of the `mesh.bootstrap` peers. `obiectl allow` beats everything;
`obiectl block` beats the entries above but never the built-in, own or
bootstrap addresses.

## enforce

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `enforce.backend` | `dryrun` | restart | Used only in `node.mode: enforce`. `dryrun` keeps the blocks in memory and logs every change, blocking nothing. `nftables` drops blocked sources through the table `inet obie` (Linux, needs `CAP_NET_ADMIN`); see the [nftables guide](../guides/nftables.md). |
| `enforce.max_entries` | `100000` | restart | Most addresses and ranges blocked at once. Beyond it the blocks with the lowest score are left out, logged and counted in `obie_enforcer_skipped_total{reason="max_entries"}`. At least 1. |
| `enforce.reconcile_interval` | `10s` | restart | How often the backend is compared with the decisions and corrected, besides right after every change; also the longest retry delay after a failed apply. Greater than 0. |
| `enforce.nftables.forward` | `false` | restart | Also drop blocked sources in a `forward` chain (routers, container hosts), not only in `input`. |
| `enforce.nftables.teardown_on_stop` | `false` | restart | Remove the table `inet obie` when the systemd unit stops (`ExecStopPost` runs `obied teardown-firewall --on-stop`). `false` keeps the blocks across restarts until they expire. |

## metrics

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `metrics.listen` | `127.0.0.1:9464` | restart | `ip:port` of `/metrics` (Prometheus, namespace `obie_`), `/healthz` and `/readyz`. Use an IP address, not a host name; `:9464` listens on every interface. See [Monitoring](monitoring.md). |

## console

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `console.enabled` | `false` | reload | Serve the local web console, a view of the node for its operator. Off by default; a reload starts or stops it without touching anything else. Sign in with the token `obiectl console` shows. |
| `console.listen` | `127.0.0.1:9465` | reload | Loopback `ip:port` of the console: `127.0.0.1` (or another `127.0.0.0/8` address) or `::1`. Any other address — a host name, `localhost`, `0.0.0.0`, an empty host, an interface address — is refused, because it would expose the console to the network. From another machine, forward the port over SSH: `ssh -L 9465:127.0.0.1:9465 <this host>`. A reload moves the console to the new address. |
| `console.actions` | `true` | reload | Let the console carry out what `obiectl allow`, `block`, `unoverride`, `report` and `revoke` do — each after a confirmation that says what will happen, under the same rules, and recorded in the audit log with `obie.origin: console`. `false` makes the console strictly read-only: it shows no action and refuses them. It adds no right: whoever may sign in may run `obiectl` too. See [Act from the console](console.md#act-from-the-console). |

The console admits only root, the user `obied` runs as and members of
`admin.socket_group`, and only with the token; its security model is
[ADR 0019](../adr/0019-local-web-console.md). If it cannot start, for
example because its port is taken, the node runs without it and logs why.
How to sign in and reach it from another machine: [Web console](console.md).

## audit

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `audit.path` | `""` | restart | JSON-lines audit log of every decision change, peer connection, reload and mode change with ECS field names; empty disables it. An absolute file path in an existing directory, e.g. `/var/log/obie/audit.jsonl` (the unit creates `/var/log/obie`). Every reload reopens the file, for logrotate. The console's [activity timeline](console.md#the-activity-timeline) reads it back; without it, the timeline shows only the last 10,000 entries since `obied` started. See [Monitoring](monitoring.md#audit-log). |

## log

| Key | Default | Applied on | Meaning |
|-----|---------|------------|---------|
| `log.level` | `info` | restart | Minimum level of the JSON logs on stderr (the journal under systemd): `debug`, `info`, `warn` or `error`. |
