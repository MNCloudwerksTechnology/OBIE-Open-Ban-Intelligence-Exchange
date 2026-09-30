# How do I uninstall OBIE completely, firewall rules included?

Remove OBIE from your server: its [node](../glossary.md#node), its
firewall table with every block in it, its Fail2Ban action, its files and
its user. Your own firewall rules and Fail2Ban stay as they are.

> **Warning:** This lifts every block of OBIE at once, and your server no
> longer learns of attackers from your [peers](../glossary.md#peer).
> Fail2Ban keeps banning on its own.

## Before you start

- OBIE was installed from the release archive, as
  [Get started](../getting-started.md#10-switch-to-enforcement-optional)
  leaves it after step 10. The steps work the same after any earlier
  step.
- If you installed with another `PREFIX`, the programs, the manual pages
  and the completions are below it instead of `/usr/local`.

## Undo

There is no undo. To come back, install OBIE again as in
[Get started](../getting-started.md#2-install-obie). To keep your node's
peer ID, restore the key you back up below before the node's first
start, as
[How do I back up and restore the node identity?](back-up-the-identity.md)
shows.

## Steps

While your node is still connected, withdraw the
[verdicts](../glossary.md#verdict) you do not want your peers to keep
counting until they expire. List them, and
[revoke](../glossary.md#revocation) each address:

```sh
sudo obiectl indicators --mine
```

```text
INDICATOR       PUBLISHER    ACTION  CONFIDENCE  EVENTS  PROTOCOL  REASON      EXPIRES
ipv4:85.10.0.7  (this node)  ban     0.8         5       ssh       bruteforce  2026-10-14T09:22:05Z
```

```sh
sudo obiectl revoke 85.10.0.7
```

```text
Revoked verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 on ipv4:85.10.0.7 (revocation 01a0ef5b-7d51-7e12-8c07-2c1f4c3e9a10, reason false_positive).
```

If you may come back, back up the node's key, which the steps below
delete:

```sh
sudo install -d -m 0700 /root/obie-backup
sudo install -m 0600 /var/lib/obie/node.key /root/obie-backup/node.key
```

Disconnect Fail2Ban from OBIE: delete the file that adds OBIE's action to
the `sshd` jail, and the action, and restart Fail2Ban. If you added `obie`
to a jail's `action` elsewhere, such as in `/etc/fail2ban/jail.local`,
delete that line too, or Fail2Ban does not start:

```sh
sudo rm /etc/fail2ban/jail.d/obie.local /etc/fail2ban/action.d/obie.conf
sudo systemctl restart fail2ban
```

Stop the node for good:

```sh
sudo systemctl disable --now obied
```

```text
Removed "/etc/systemd/system/multi-user.target.wants/obied.service".
```

Remove OBIE's firewall table. Blocks stay in it after the node stops,
until their time runs out; this lifts them all at once and touches no
other rule:

```sh
sudo obied teardown-firewall
```

```text
obied teardown-firewall: table inet obie removed; nothing is blocked by OBIE anymore
```

Remove the programs, the service, the manual pages and the shell
completions:

```sh
sudo rm /etc/systemd/system/obied.service /usr/local/bin/obied /usr/local/bin/obiectl
cd /usr/local/share && sudo rm -f man/man1/obied.1 man/man1/obiectl.1 \
  bash-completion/completions/obied bash-completion/completions/obiectl \
  zsh/site-functions/_obied zsh/site-functions/_obiectl \
  fish/vendor_completions.d/obied.fish fish/vendor_completions.d/obiectl.fish
sudo systemctl daemon-reload
```

Remove the node's state, its configuration, its logs and its user. The
state holds the node's key and what it knew:

```sh
sudo rm -r /var/lib/obie /etc/obie /var/log/obie
sudo userdel obie
sudo groupdel obie 2>/dev/null || true
```

Last, delete what you downloaded in step 2 of Get started:

```sh
rm -r obie-0.1.0-linux-amd64 obie-0.1.0-linux-amd64.tar.gz SHA256SUMS
```

Ask your peers' operators to remove your node from their configuration
([leaving a federation](../operations/federation.md#leave-a-federation)),
and close port 4001 in your firewall if you opened it for OBIE.

## Check that it worked

OBIE's firewall table `inet obie` is gone. The tables left are not OBIE's,
such as Fail2Ban's:

```sh
sudo nft list tables
```

```text
table inet f2b-table
```

No file of OBIE is left; the command counts them:

```sh
sudo find /etc /usr/local /var/lib /var/log /run -name '*obie*' | wc -l
```

```text
0
```

Only your key backup in `/root/obie-backup` remains, if you made one.
