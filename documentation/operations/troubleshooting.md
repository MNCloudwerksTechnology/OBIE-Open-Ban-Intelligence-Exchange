# Troubleshooting

Start with the self-check. It looks at the configuration, the identity,
admin access, the [node](../glossary.md#node) itself, its
[peers](../glossary.md#peer), the clock,
Fail2Ban, the firewall and your SSH session, and names the next step for
everything that is not right
([Set up and check a node](setup.md#check-the-node)):

```sh
sudo obied self-check
```

Then look at the node's own view and its log:

```sh
sudo obiectl status
sudo journalctl -u obied -n 50
```

`obiectl status` names every subsystem that is not running or not ready,
with its error. The log is JSON; errors have `"level":"ERROR"`.

## Locked out

You cannot reach the host any more, and suspect OBIE blocked you.

1. **Get in another way**: the provider's console, IPMI/KVM, or from an
   address on the [allow-list](../glossary.md#allow-list) (private networks
   are never blocked, so a jump host on the same LAN works).
2. **Remove every OBIE block at once.** This deletes OBIE's nftables
   table and nothing else; your own firewall rules stay:

   ```sh
   sudo systemctl stop obied
   sudo obied teardown-firewall
   ```

   If `obied` is not installed any more, `sudo nft delete table inet obie`
   does the same.
3. **Find out why** the address was blocked, with the node still stopped:
   the audit log has a `block-added` line with the reason, score and
   publishers (`grep '"ip":"198.51.100.7"' /var/log/obie/audit.jsonl`).
   Common causes: your address is not on the allow-list and your own
   Fail2Ban banned it (a local ban blocks at once), or
   [peers](../glossary.md#peer) reported it.
4. **Protect the address** before you start the node again. Add your
   management networks to `allowlist.cidrs` in `/etc/obie/obie.yaml` and
   check the file. Then start the node and make sure:

   ```sh
   sudo obied --config /etc/obie/obie.yaml --check-config
   sudo systemctl start obied
   sudo obiectl allow 198.51.100.7 --note "admin workstation"
   sudo obiectl explain 198.51.100.7
   ```

   `explain` must show `Decision: allowed`. `obiectl allow` beats every
   other rule, even a force-block. Also add the address to Fail2Ban's
   `ignoreip`, so the local jail does not report it again.

If you cannot get in at all: OBIE blocks expire with their
[verdicts](../glossary.md#verdict), at most after `decision.max_ttl` (30
days by default), even if `obied` is not running. A reboot does not remove
them while the node runs in [enforce mode](../glossary.md#enforce-mode),
because it re-applies its decisions at start.

To prevent it: keep the allow-list complete *before* switching to
`enforce` ([quick start, step 5](quickstart.md#5-enforce)), keep a console
path, and consider `enforce.nftables.teardown_on_stop: true`, so that
stopping the service lifts every block.

## Nothing is enforced

An address is reported, but not blocked. Ask the node why:

```sh
sudo obiectl explain 85.10.0.7
```

| `explain` or `status` shows | Cause and fix |
|-----------------------------|---------------|
| `Decision: none`, `below consensus: score 0.64 < threshold 1.8` | Not enough trusted publishers agree. See [Federation](federation.md#choose-trust-weights-and-quorum) for [threshold](../glossary.md#threshold) and [quorum](../glossary.md#quorum). |
| A publisher with `WEIGHT 0` and `COUNTS no` | Its peer ID is not in `trust.publishers`, or not exactly. Compare it with `obiectl peers`. |
| `No active verdicts.` | The verdict never arrived: the publisher restarted before it had a peer to send it to, or it was dropped (see [no peers](#no-peers) and the event outcomes below). |
| `Decision: allowed` | The allow-list or a force-allow (`obiectl overrides`) covers the address. |
| `Decision: block…`, but `status` says `Mode: OBSERVE` | [Observe mode](../glossary.md#observe-mode) never blocks. Set `node.mode: enforce` and reload. |
| `enforce … enforcing via dryrun` in `status` | The `dryrun` backend only logs. Set `enforce.backend: nftables` and **restart**. |
| `enforce … N skipped over enforce.max_entries` | More blocks than `enforce.max_entries`; raise it. |
| `enforce` not ready, `nftables access denied: obied needs CAP_NET_ADMIN` | `obied` runs without the capability. Use the shipped unit, or grant `CAP_NET_ADMIN`. |

`obiectl enforced` lists what the backend applies, and `sudo nft list
table inet obie` what the kernel has. If the two differ, the node
corrects it within `enforce.reconcile_interval`.

Events can also be dropped on the way. Check the outcomes of received
events:

```sh
curl -s http://127.0.0.1:9464/metrics | grep '^obie_events_received_total'
```

- `expired` growing: the clocks differ. A verdict issued more than five
  minutes in the future, or already expired, is dropped. Run NTP (e.g.
  `systemd-timesyncd`) on every node.
- `rate_limited` growing: a publisher or peer sends more than
  `mesh.rate_limit` allows.
- `invalid_signature`, `invalid_schema`, `too_large` growing: a peer
  forwards broken or forged events. They are rejected and the peer is
  scored down; if it persists, stop trusting and connecting to it.

## No peers

`obiectl peers` says `No peers connected.`, or `status` shows `mesh …
degraded: 0 peers connected`.

- **Is the other node running and reachable?** From your host:
  `nc -vz obie.friend.example 4001`. Both sides must allow TCP (and UDP
  for QUIC) port 4001 from each other; cloud security groups count too.
- **Is the multiaddr right?** It must end in `/p2p/<peer ID>` with the
  peer ID the other node shows in `obiectl identity`. A wrong peer ID makes
  every connection fail after the handshake. `sudo journalctl -u obied | grep
  'bootstrap peer'` shows the dial errors (`bootstrap peer unreachable`).
- **Did you restart?** `mesh.bootstrap` is only read at start; a reload
  logs `configuration changes that need a restart were not applied` with
  `mesh.bootstrap`.
- **Is the node listening where you think?** `sudo journalctl -u obied | grep
  'mesh listening'` shows the addresses. Behind NAT, forward port 4001 and
  give peers the public address.
- **Is the address resolvable?** A `/dns4/` name is resolved at every
  dial; check it with `getent hosts obie.friend.example`.

After a failed dial the node retries with a backoff of up to five minutes,
so a fixed problem can take that long to heal; `sudo systemctl restart
obied` dials at once.

What this node reports or [revokes](../glossary.md#revocation) while no
peer is connected is not lost: it counts on this node at once, and the
node holds it in memory and sends it, in order, as soon as a peer joins —
16 events every 2 seconds, so that peers do not drop a long backlog
(`obied` logs `no peer is on the topic; the event is held and sent when
one joins`, and later `the held events were sent`). A restart before then
drops what is held; the verdicts still count on this node, and a new
report sends them again.
Verdicts about to expire within a minute are not sent any more.

## Fail2Ban reports do not arrive

Fail2Ban bans, but `obiectl indicators --mine` stays empty. The action
logs every failure with the tag `obie-fail2ban`:

```sh
sudo journalctl -t obie-fail2ban -n 20
```

| Message contains | Fix |
|------------------|-----|
| `obied is not running` | Start `obied`, or pass the right socket: `obie[socket=/path/to/obie.sock]` (`admin.socket`). |
| `obied did not answer in time` | `obied` does not answer within 5 s; check `obiectl status` and the node's log. |
| `obiectl exit code 127` (`not found`) | Fail2Ban cannot find `obiectl` on its `PATH`; set `obie[obiectl=/usr/local/bin/obiectl]`. |
| `overlaps the allow-listed network` or `is not a public address` | Working as intended: `obied` never reports allow-listed or non-public addresses. |

No message at all: check that the jail lists `obie` among its actions
(`sudo fail2ban-client -d | grep "'obie'"`) and that Fail2Ban was
reloaded. The [Fail2Ban guide](../guides/fail2ban.md#verify) has a
step-by-step check.

## The web console does not open

`sudo obiectl console` says whether the console is switched on and
serving, and why not. It never keeps the node from running: a console that
cannot listen (port taken) is logged as `console not started; the node
runs without it`. A refusal in the browser names its cause — your user is
not in the group `obie`, or the console was opened under another name than
`127.0.0.1` or `localhost`. [Web console](console.md#when-it-does-not-work)
lists every message and its fix.

## obied does not start

`sudo systemctl status obied` and `sudo journalctl -u obied -n 20` show the reason
in the `obied failed` line. Common ones:

| Error | Fix |
|-------|-----|
| `invalid configuration:` followed by key paths | Fix that key; `obied --config /etc/obie/obie.yaml --check-config` lists every problem. |
| `… node.key has mode 0644 and is accessible by group or others` / `is owned by root` | The key was copied with the wrong mode or owner: `sudo chown obie:obie /var/lib/obie/node.key; sudo chmod 600 /var/lib/obie/node.key`. |
| `chgrp admin socket to … operation not permitted` | `admin.socket_group` differs from the unit's `Group=`; make them equal ([install.md](install.md#the-service-sandbox)). |
| `state directory has a newer format` | A newer `obied` used the state directory. Run that version again, or restore the backup taken before the upgrade. |
| `address already in use` | Another process has port 4001 or 9464; change `mesh.listen` or `metrics.listen`. |
