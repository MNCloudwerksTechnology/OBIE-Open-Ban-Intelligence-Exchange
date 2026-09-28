# Quick start

From nothing to a node that turns Fail2Ban bans into signed verdicts and,
once you trust what it shows you, blocks attackers with nftables. Plan on
half an hour. You need:

- a Linux host (amd64 or arm64) with systemd, nftables and root access;
- Fail2Ban 0.10 or newer, already banning something (an `sshd` jail will
  do);
- a console to the host that does not depend on its network (provider
  console, IPMI, KVM), in case you lock yourself out in step 5.

The steps are: [install](#1-install), [start in observe
mode](#2-start-in-observe-mode), [connect Fail2Ban](#3-connect-fail2ban),
[verify](#4-verify), [enforce](#5-enforce). Connecting to other nodes comes
afterwards, in [Federation](federation.md).

## 1. Install

Download the release for your architecture (`arm64` instead of `amd64`
for ARM), check it and run the installer:

```sh
curl -fL -O https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v0.1.0/obie-0.1.0-linux-amd64.tar.gz
curl -fL -O https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v0.1.0/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf obie-0.1.0-linux-amd64.tar.gz
sudo ./obie-0.1.0-linux-amd64/install.sh
```

`sha256sum` must print `obie-0.1.0-linux-amd64.tar.gz: OK`. The installer
creates the user `obie`, puts `obied` and `obiectl` into `/usr/local/bin`,
the configuration into `/etc/obie/obie.yaml`, the systemd unit and, as
Fail2Ban is installed, the Fail2Ban action. It starts nothing. What it does
in detail: [Installing and upgrading](install.md).

## 2. Start in observe mode

The installed configuration is the annotated
[example](../examples/obie.yaml): every key at its default, `node.mode:
observe`. In observe mode the node decides and shows what it would block,
but blocks nothing. Keep it that way for now; you only need to change
something if port 4001 or 9464 is taken (`mesh.listen`, `metrics.listen`,
see the [configuration reference](configuration.md)).

It is worth turning on the audit log, which records every decision. Edit
`/etc/obie/obie.yaml`:

```yaml
audit:
  path: /var/log/obie/audit.jsonl
```

Check the file, start the node and ask it how it is:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl enable --now obied
sudo obiectl status
```

`--check-config` prints `obied: configuration /etc/obie/obie.yaml is
valid`. `obiectl status` shows `Mode: OBSERVE`, `Ready: yes` and every
subsystem `running`; `mesh` is `degraded: 0 peers connected` until you
federate, which is fine. On its first start the node created its identity,
an Ed25519 key in `/var/lib/obie/node.key`. Its peer ID is how other nodes
will know it:

```sh
sudo obiectl identity
curl -s http://127.0.0.1:9464/readyz
```

`/readyz` answers `{"ready":true}`. Point Prometheus at
`http://127.0.0.1:9464/metrics` when you are ready for it
([Monitoring](monitoring.md)).

Instead of `sudo obiectl`, members of the group `obie` may run `obiectl`
(`sudo usermod -aG obie "$USER"`, then log in again).

## 3. Connect Fail2Ban

The installer placed the action in `/etc/fail2ban/action.d/obie.conf`. Add
it to every jail that should report, next to the jail's own ban action, in
`/etc/fail2ban/jail.local`:

```ini
[sshd]
enabled = true
action  = %(action_)s
          obie
```

Check the Fail2Ban configuration and load it:

```sh
sudo fail2ban-client -t
sudo fail2ban-client reload
```

From now on every ban of the jail is also reported to `obied` and becomes
a signed verdict. Matched log lines are hashed on the host; only the hash
and the failure count are published. The [Fail2Ban guide](../guides/fail2ban.md)
explains the jail parameters (reason, confidence, revoke on unban).

## 4. Verify

Test the path from Fail2Ban to `obied` without publishing anything: ban an
address from a documentation range, which `obied` always refuses, and look
for the refusal in the log:

```sh
sudo fail2ban-client set sshd banip 203.0.113.7
journalctl -t obie-fail2ban -n 5
sudo fail2ban-client set sshd unbanip 203.0.113.7
```

The log line `could not report 203.0.113.7 of jail sshd to OBIE (obiectl
exit code 1)` names the refusal, so the action reached `obied`. If it says
`obied is not running` or `context deadline exceeded` instead, see
[Troubleshooting](troubleshooting.md#fail2ban-reports-do-not-arrive).

After the next real ban, the address shows up as a verdict of this node
and as a decision:

```sh
sudo obiectl indicators --mine
sudo obiectl explain 85.10.0.7
sudo obiectl decisions --state block
curl -s http://127.0.0.1:9464/metrics | grep '^obie_decisions'
```

(`85.10.0.7` stands for the banned address.) `explain` shows `Decision:
block until …` with the reason `local autoblock: this node's own ban
verdict`: the node's own verdicts block at once, without waiting for other
nodes (`decision.local_autoblock`). In observe mode that block is only
recorded: `obiectl enforced` answers `No entries applied: the node is in
observe mode`. Let the node
observe for a few days and read what it would have blocked, with
`obiectl decisions` or in the audit log.

## 5. Enforce

Before the node may touch the firewall, tell it what it must never block.
Loopback, private and link-local ranges, the node's own addresses and its
bootstrap peers are always safe. Add everything else you cannot afford to
lose: the networks you administer from, monitoring, your DNS resolvers and
gateways, and the host's public address if it sits behind NAT. In
`/etc/obie/obie.yaml`:

```yaml
node:
  mode: enforce
allowlist:
  cidrs:
    - 198.51.100.0/24   # office and VPN
    - 192.0.2.53/32     # resolver
enforce:
  backend: nftables
```

The backend is chosen at start, so restart the node:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl restart obied
sudo obiectl status
sudo obiectl enforced
sudo nft list table inet obie
```

`obiectl status` now shows `Mode: ENFORCE`. `obiectl enforced` lists the
addresses in the firewall, and `nft list table inet obie` shows them in the
sets `obie_v4` and `obie_v6` of OBIE's own table. No other table is ever
changed ([nftables guide](../guides/nftables.md)).

To stop enforcing, set `node.mode: observe` again and reload; the node
removes every block at once:

```sh
sudo systemctl reload obied
```

If the node is not running or you are locked out, remove the table with
every block from the console:

```sh
sudo obied teardown-firewall
```

[Troubleshooting](troubleshooting.md#locked-out) covers getting back in.

## Next steps

- [Federate](federation.md) with a node you trust: exchange peer IDs,
  choose trust weights and a quorum.
- [Operate](operations.md): metrics, audit log, upgrades, backing up the
  node key, uninstalling.
- Read the [threat model](../../SECURITY.md#threat-model) to know what
  OBIE protects against and what it does not.

## How these steps are tested

Every command above is exercised by an automated test, the compose lab's
smoke test or a CI step. Commands that act on the host itself (`systemctl`,
`fail2ban-client`, `nft`) are covered through what they drive: the unit
file, the Fail2Ban action, the nftables backend. `TestQuickstartCommandsAreTested` (in
`test/docs`) fails if a command in this page has no row here or a row names
a test, file or CI step that does not exist.

| Command | Exercised by |
|---------|--------------|
| `curl -fL -O https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/` | `make release` and `TestPublishRelease` (the published asset names), CI step `quickstart steps on the release tarball` |
| `sha256sum -c --ignore-missing SHA256SUMS` | CI step `quickstart steps on the release tarball` |
| `tar -xzf obie-` | CI step `quickstart steps on the release tarball` |
| `./obie-0.1.0-linux-amd64/install.sh` | `TestInstallIsIdempotent`, CI step `quickstart steps on the release tarball` |
| `obied --config /etc/obie/obie.yaml --check-config` | `TestRunDaemonCheckConfig`, `TestExampleConfigPassesCheckConfig`, CI step `quickstart steps on the release tarball` |
| `systemctl enable --now obied` | `make check-unit` (the unit passes `systemd-analyze verify`), `TestUnitSandbox`, `TestRunDaemonPersistsIdentity` |
| `systemctl restart obied` | `make check-unit`, `TestRunDaemonGracefulShutdown`, `TestRunDaemonStopsOnSIGTERM` |
| `systemctl reload obied` | `TestUnitSandbox` (`ExecReload` sends SIGHUP), `TestReloadSignals`, `TestSovereigntyAgainstInProcessDaemon` |
| `obiectl status` | `TestObiectlStatusAgainstInProcessDaemon`, `Dockerfile` (the image's health check, which `make lab-smoke` waits for) |
| `obiectl identity` | `TestObiectlIdentityMatchesKeyFile` |
| `curl -s http://127.0.0.1:9464/readyz` | `TestReadyz` |
| `curl -s http://127.0.0.1:9464/metrics` | `TestObservabilityAgainstInProcessDaemon` |
| `fail2ban-client -t` | `TestFail2BanAcceptsAction` |
| `fail2ban-client reload` | `TestFail2BanAcceptsAction` (the configuration it loads) |
| `fail2ban-client set sshd banip 203.0.113.7` | `TestActionBan`, `TestActionWithRealObiectl` |
| `journalctl -t obie-fail2ban` | `TestActionWithRealObiectl`, `TestActionDaemonDown` (the logged message and tag) |
| `fail2ban-client set sshd unbanip 203.0.113.7` | `TestActionUnban` |
| `obiectl indicators --mine` | `TestObiectlVerdictsAgainstInProcessDaemon` |
| `obiectl explain` | `TestObiectlExplainAgainstInProcessDaemon`, `packaging/compose/smoke-test.sh` |
| `obiectl decisions --state block` | `TestObiectlExplainAgainstInProcessDaemon` |
| `obiectl enforced` | `TestObiectlEnforcedAgainstInProcessDaemon`, `packaging/compose/smoke-test.sh` |
| `nft list table inet obie` | `TestSignalToEnforcementNFTables` (`make test-privileged`, the CI's manual privileged run) |
| `obied teardown-firewall` | `TestTeardownFirewall` |
