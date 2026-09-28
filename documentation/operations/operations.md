# Operating a node

Day-2 tasks for a node installed with `install.sh` and the systemd unit
([quick start](quickstart.md)). For the container image, see
[Installing and upgrading](install.md#run-the-container-image).

## Metrics and health

`obied` serves Prometheus metrics on `metrics.listen` (default
`127.0.0.1:9464`, reachable only from the host): `/metrics`, and
`/healthz` and `/readyz` for health checks. [Monitoring](monitoring.md)
lists every metric and ships a Grafana dashboard. Alert on at least:

| Condition | PromQL | Means |
|-----------|--------|-------|
| Node down | `up{job="obie"} == 0` | `obied` is not running or not reachable. |
| Isolated | `obie_peers_connected == 0` for 15m | No peer is connected ([no peers](troubleshooting.md#no-peers)). |
| Enforcement failing | `rate(obie_enforcer_apply_total{result="error"}[5m]) > 0` | The firewall is not in line with the decisions. |
| Blocks dropped | `increase(obie_enforcer_skipped_total{reason="max_entries"}[1h]) > 0` | More blocks than `enforce.max_entries`; the lowest scores are left out. |
| Peer sends garbage | `rate(obie_events_received_total{outcome=~"invalid_.*\|too_large"}[5m]) > 0` | A connected peer forwards forged or broken events. |
| Flood | `rate(obie_events_received_total{outcome="rate_limited"}[5m]) > 0` | A publisher or peer exceeds `mesh.rate_limit`. |

To scrape from another host, set `metrics.listen` to an address on a
management network (not `0.0.0.0` on a public interface) and restart.

`sudo obiectl status` gives the same picture interactively: the mode, and
per subsystem whether it runs and what it is doing.

## Web console

For an overview of the node in a browser — its health, key numbers,
what needs your attention and the peers it knows with the trust placed in
them — switch on the opt-in
[web console](console.md): `console.enabled: true` and a reload. It
listens on `127.0.0.1:9465` only; `sudo obiectl console` shows its address
and the token to sign in with, and from another machine you forward the
port over SSH.

## Audit log

With `audit.path: /var/log/obie/audit.jsonl` every decision change is one
JSON line with Elastic Common Schema fields: blocks added, updated and
removed with their score and reason, allow-list hits, overrides, local
reports and revocations. [Monitoring](monitoring.md#audit-log) describes
the fields, log rotation and how to ship it to Loki or Elasticsearch.

The audit log answers "why was this address blocked at 03:12?". For the
current state, ask the node:

```sh
sudo obiectl decisions --state block
sudo obiectl explain 85.10.0.7
sudo obiectl overrides
```

The process log (start, stop, reloads, peers, errors) goes to the journal:
`sudo journalctl -u obied`.

## Change the configuration

Edit `/etc/obie/obie.yaml`, check it, and reload or restart as the
[configuration reference](configuration.md) says for the key:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl reload obied     # node.mode, trust, decision, allowlist (with exceptions)
sudo systemctl restart obied    # everything else
```

A reload with an invalid file is rejected and logged, and the node keeps
running with its previous configuration. Keys that need a restart are
logged as `configuration changes that need a restart were not applied`.

## Override the mesh

You have the last word over every address:

```sh
sudo obiectl allow 192.0.2.10 --note "partner monitoring"
sudo obiectl block 85.10.0.7 --ttl 24h --note "incident 42"
sudo obiectl unoverride 85.10.0.7
```

`allow` beats everything, including the allow-list and force-blocks;
`block` beats the mesh but never the built-in, own and bootstrap
addresses. Overrides survive restarts; `obiectl overrides` lists them.

## Upgrade

Back up the state directory, install the new release over the old one and
restart:

```sh
sudo systemctl stop obied
sudo tar -C /var/lib -czf /root/obie-state-before-upgrade.tar.gz obie
sudo ./obie-0.1.1-linux-amd64/install.sh
sudo systemctl start obied
sudo obiectl status
```

`install.sh` replaces the binaries, the unit and the Fail2Ban action and
keeps `/etc/obie/obie.yaml`; compare it with the new
`/etc/obie/obie.yaml.example` and the [changelog](../../CHANGELOG.md). A
newer `obied` may migrate the state directory; an older one refuses to
start on it, so the backup is how you roll back ([state directory
format](install.md#state-directory-format)). Blocks in the firewall stay in
place while the node is stopped, unless `enforce.nftables.teardown_on_stop`
is set.

## Back up the node key

`/var/lib/obie/node.key` is the node's identity: its peer ID and the key
its verdicts are signed with. Peers trust that key. Lose it and your node
becomes a stranger to them; leak it and someone else can publish verdicts
in your name. Back it up once, offline, readable only by root:

```sh
sudo install -d -m 0700 /root/obie-backup
sudo install -m 0600 /var/lib/obie/node.key /root/obie-backup/node.key
sudo obied identity --state-dir /root/obie-backup
```

`obied identity` must print the same peer ID as `obiectl identity`
(`--state-dir` reads the file `node.key` in that directory). Move the copy
off the host, e.g. into your password manager or an
encrypted backup. The event store (`/var/lib/obie/db`) holds verdicts
that expire within `decision.max_ttl` and your overrides; losing it costs
the verdicts received so far (v0.1 has no catch-up, the node only learns of
new ones) and the overrides, so note down `obiectl overrides` if you rely
on them.

To **restore** the key on a new host, before the first start (or with
`obied` stopped):

```sh
sudo install -d -o obie -g obie -m 0700 /var/lib/obie
sudo install -o obie -g obie -m 0600 obie-backup/node.key /var/lib/obie/node.key
```

`obied` refuses a key file that another user owns or that the group or
others may read, and says how to fix it.

If the key may have been **stolen**, replace it: stop the node, move the
key away, and start it again; it creates a new key. Then give your peers
the new peer ID and ask them to remove the old one from
`trust.publishers`. v0.1 has no way to revoke a key on the mesh.

```sh
sudo systemctl stop obied
sudo mv /var/lib/obie/node.key /root/obie-node.key.stolen
sudo systemctl start obied
sudo obiectl identity
```

## Uninstall

First delete the `obie` lines from your Fail2Ban jails' `action` options
and run `sudo fail2ban-client reload`. Then stop the node, remove its
firewall table and its files:

```sh
sudo systemctl disable --now obied
sudo obied teardown-firewall
sudo rm /etc/systemd/system/obied.service /usr/local/bin/obied /usr/local/bin/obiectl
sudo rm -f /etc/fail2ban/action.d/obie.conf
sudo systemctl daemon-reload
```

`obied teardown-firewall` deletes the nftables table `inet obie` with
every block; it does nothing if there is none. Skip it and the blocks stay
in the kernel until their timeouts run out (up to `decision.max_ttl`).

To also remove the identity, the event store, the configuration, the logs
and the user (back up the key first if you may come back):

```sh
sudo rm -r /var/lib/obie /etc/obie /var/log/obie
sudo userdel obie
sudo groupdel obie 2>/dev/null || true
```

Tell your peers to remove your peer ID from `mesh.bootstrap` and
`trust.publishers` ([leaving a federation](federation.md#leave-a-federation)).
