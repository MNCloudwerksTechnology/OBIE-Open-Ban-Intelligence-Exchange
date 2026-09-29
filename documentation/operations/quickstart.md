# Quick start

From nothing to a [node](../glossary.md#node) that turns Fail2Ban bans
into signed [verdicts](../glossary.md#verdict) and, once you trust what it
shows you, blocks attackers with nftables. Plan on half an hour. You need:

- a Linux host (amd64 or arm64) with systemd, nftables and root access;
- Fail2Ban 0.11 or newer, already banning something (an `sshd` jail will
  do);
- a console to the host that does not depend on its network (provider
  console, IPMI, KVM), in case you lock yourself out in step 5.

The steps are: [install](#1-install), [set up and start](#2-set-up-and-start-in-observe-mode) in
[observe mode](../glossary.md#observe-mode),
[connect Fail2Ban](#3-connect-fail2ban), [verify](#4-verify),
[enforce](#5-enforce). Connecting to other nodes comes afterwards, in
[Federation](federation.md).

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

## 2. Set up and start in observe mode

Let the setup assistant write the configuration. It asks five questions,
explains each one and suggests a safe answer
([what it asks](setup.md#set-up-the-node)):

```sh
sudo obied setup
```

For this walkthrough, press Enter at every question to take the
suggestion. The node then keeps its state in `/var/lib/obie`, records
every decision in the audit log `/var/log/obie/audit.jsonl`, has no
[peers](../glossary.md#peer) yet and starts in [observe mode](../glossary.md#observe-mode): it decides
and shows what it would block, but blocks nothing. It also never blocks
the address your SSH session comes from. The configuration `install.sh`
installed is the unchanged example, so the assistant offers to replace it
and keeps it as `/etc/obie/obie.yaml.bak`. You only need to change
something by hand if port 4001 or 9464 is taken (`mesh.listen`,
`metrics.listen`, see the [configuration reference](configuration.md)).

Start the node and let it check itself:

```sh
sudo systemctl enable --now obied
sudo obied self-check
sudo obiectl status
```

The self-check reports every check as `OK`, `WARNING` or `PROBLEM`, and
says what to do next for each warning and problem
([what it checks](setup.md#check-the-node)). On a new node there must be
no problem; expect a warning for the peers, as the node works on its own
until you federate, and for Fail2Ban until step 3. `obiectl status` shows
`Mode: OBSERVE`, `Ready: yes` and every subsystem `running`; `mesh` is
`degraded: 0 peers connected` until you federate, which is fine. On its
first start the node created its identity, an Ed25519 key in
`/var/lib/obie/node.key`. Its [peer ID](../glossary.md#peer-id) is how other
nodes will know it:

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
explains the jail parameters (reason, confidence,
[revoke](../glossary.md#revocation) on unban).

## 4. Verify

Test the path from Fail2Ban to `obied` without publishing anything: ban an
address from a documentation range, which `obied` always refuses, and look
for the refusal in the log:

```sh
sudo fail2ban-client set sshd banip 203.0.113.7
sudo journalctl -t obie-fail2ban -n 5
sudo fail2ban-client set sshd unbanip 203.0.113.7
```

The log line `could not report 203.0.113.7 of jail sshd to OBIE (obiectl
exit code 1): …` must end with the refusal, `… is not a public address;
OBIE never publishes internal or special-purpose addresses …`: the action
reached `obied`. If it says `obied is not running` or `context deadline
exceeded` instead, see
[Troubleshooting](troubleshooting.md#fail2ban-reports-do-not-arrive). The
self-check reads the same log: its Fail2Ban line now names the jail and
says that the last report reached the node and was refused as intended.

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

Before the node may touch the firewall in
[enforce mode](../glossary.md#enforce-mode), tell it what it must never
block. Loopback, private and link-local ranges, the node's own addresses and
its bootstrap peers are always safe. Add everything else you cannot afford
to lose: the networks you administer from, monitoring, your DNS resolvers
and gateways, and the host's public address if it sits behind NAT. The
file the assistant wrote, `/etc/obie/obie.yaml`, has the sections `node` and
`allowlist` already: set the mode, add your networks next to the address of
your SSH session in `cidrs`, and add the section `enforce`:

```yaml
node:
  mode: enforce
allowlist:
  cidrs:
    - "85.10.3.20/32"   # your SSH session, added by obied setup
    - 198.51.100.0/24   # office and VPN
    - 192.0.2.53/32     # resolver
enforce:
  backend: nftables
```

The backend is chosen at start, so restart the node:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl restart obied
sudo obied self-check
sudo obiectl status
sudo obiectl enforced
sudo nft list table inet obie
```

The self-check's Firewall line must be `OK`. If the address of your SSH
session is not protected, it reports a problem and ends with a banner that
starts with `LOCKOUT RISK`: add the address to `allowlist.cidrs` before
you go on. `obiectl status` now shows `Mode: ENFORCE`. `obiectl enforced`
lists the addresses in the firewall, and `nft list table inet obie` shows
them in the sets `obie_v4` and `obie_v6` of OBIE's own table. No other
table is ever changed ([nftables guide](../guides/nftables.md)).

To stop enforcing, set `node.mode: observe` again and reload; the node
removes every block at once:

```sh
sudo systemctl reload obied
```

If you are locked out, stop the node and remove the table with every
block from the console. Stop it first: a running node in enforce mode
restores its table within seconds.

```sh
sudo systemctl stop obied
sudo obied teardown-firewall
```

[Troubleshooting](troubleshooting.md#locked-out) covers getting back in.

## Next steps

- [Federate](federation.md) with a node you trust: exchange peer IDs,
  choose [trust weights](../glossary.md#trust-weight) and a
  [quorum](../glossary.md#quorum).
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
| `obied setup` | `TestSetupInteractiveMatchesNonInteractive`, `TestSetupEnterTakesSafeDefaults`, CI step `quickstart steps on the release tarball` |
| `obied self-check` | `TestSelfCheckBeforeFirstStart`, `TestSelfCheckAgainstInProcessDaemon` |
| `obied --config /etc/obie/obie.yaml --check-config` | `TestRunDaemonCheckConfig`, `TestExampleConfigPassesCheckConfig`, CI step `quickstart steps on the release tarball` |
| `systemctl enable --now obied` | `make check-unit` (the unit passes `systemd-analyze verify`), `TestUnitSandbox`, `TestRunDaemonPersistsIdentity` |
| `systemctl stop obied` | `make check-unit`, `TestRunDaemonStopsOnSIGTERM` |
| `systemctl restart obied` | `make check-unit`, `TestRunDaemonGracefulShutdown`, `TestRunDaemonStopsOnSIGTERM` |
| `systemctl reload obied` | `TestUnitSandbox` (`ExecReload` sends SIGHUP), `TestReloadSignals`, `TestSovereigntyAgainstInProcessDaemon` |
| `obiectl status` | `TestObiectlStatusAgainstInProcessDaemon`, `Dockerfile` (the image's health check, which `make lab-smoke` waits for) |
| `obiectl identity` | `TestObiectlIdentityMatchesKeyFile` |
| `curl -s http://127.0.0.1:9464/readyz` | `TestReadyz` |
| `curl -s http://127.0.0.1:9464/metrics` | `TestObservabilityAgainstInProcessDaemon` |
| `fail2ban-client -t` | `TestFail2BanAcceptsAction`, which CI runs with Fail2Ban installed (CI step `Install Fail2Ban`) |
| `fail2ban-client reload` | `TestFail2BanAcceptsAction` (the configuration it loads), CI step `Install Fail2Ban` |
| `fail2ban-client set sshd banip 203.0.113.7` | `TestActionBan`, `TestActionWithRealObiectl` |
| `journalctl -t obie-fail2ban` | `TestActionWithRealObiectl`, `TestActionDaemonDown` (the logged message and tag) |
| `fail2ban-client set sshd unbanip 203.0.113.7` | `TestActionUnban` |
| `obiectl indicators --mine` | `TestObiectlVerdictsAgainstInProcessDaemon` |
| `obiectl explain` | `TestObiectlExplainAgainstInProcessDaemon`, `packaging/compose/smoke-test.sh` |
| `obiectl decisions --state block` | `TestObiectlExplainAgainstInProcessDaemon` |
| `obiectl enforced` | `TestObiectlEnforcedAgainstInProcessDaemon`, `packaging/compose/smoke-test.sh` |
| `nft list table inet obie` | `TestSignalToEnforcementNFTables` (`make test-privileged`, the CI's manual privileged run) |
| `obied teardown-firewall` | `TestTeardownFirewall` |
