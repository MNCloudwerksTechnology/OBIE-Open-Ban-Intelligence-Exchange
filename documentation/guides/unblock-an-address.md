# How do I unblock an address I trust, now and for good?

When your [node](../glossary.md#node) blocks an address you trust, such
as a customer who mistyped their password, lift the block at once. Then
make sure that neither your node, nor Fail2Ban, nor your
[peers](../glossary.md#peer) block it again because of you.

> **Warning:** This lets the address through your firewall at once, and
> for good. Unblock only addresses you trust: an attacker you unblock can
> try again, and your node will not stop it.

## Before you start

- Your node runs in [enforce mode](../glossary.md#enforce-mode), as
  [Get started](../getting-started.md#10-switch-to-enforcement-optional)
  leaves it after step 10.
- You know the address, and why the node blocks it
  ([How do I find out why an address is blocked?](why-is-an-address-blocked.md)).
  `85.10.4.12` stands for it here: your own Fail2Ban banned it.
- For the console's way: the [web console](../operations/console.md) is
  switched on ([how](../operations/console.md#switch-it-on)) and open in
  your browser.

## Undo

To let your node, Fail2Ban and the [verdicts](../glossary.md#verdict)
decide on the address again, take back each change of the steps. Delete
the address from the [allow-list](../glossary.md#allow-list) and from
Fail2Ban's `ignoreip`, and apply both:

```sh
sudo sed -i '/^    - "85.10.4.12\/32"$/d' /etc/obie/obie.yaml
sudo systemctl reload obied
sudo sed -i 's/ 85\.10\.4\.12\b//' /etc/fail2ban/jail.d/trusted-addresses.local
sudo systemctl restart fail2ban
```

Then remove the [override](../glossary.md#override):

```sh
sudo obiectl unoverride 85.10.4.12
```

```text
Override on 85.10.4.12 removed.
Decision now: none — no active verdicts
```

The address is blocked again only when it is reported again. A
[revocation](../glossary.md#revocation) cannot be taken back.

## Steps

Allow the address on your node with an
[override](../glossary.md#override), which works at once and stays until
you remove it.

**In the console:** the address's explanation,
<http://127.0.0.1:9465/decisions/85.10.4.12>, says
**Blocked by this node's own verdict (local autoblock)** and offers
**Always allow…**. Choose it, give a note, choose *Review*, and confirm.

On the command line:

```sh
sudo obiectl allow 85.10.4.12 --note "customer, mistyped password"
```

```text
Override set: force_allow on ipv4:85.10.4.12, until removed (note: "customer, mistyped password").
Decision now: allowed — operator force-allow override on ipv4:85.10.4.12 (note: "customer, mistyped password"); verdicts: local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
```

Fail2Ban blocks the addresses it bans in its own firewall rules, apart
from OBIE. Lift its ban too; `1` means that it lifted one:

```sh
sudo fail2ban-client set sshd unbanip 85.10.4.12
```

```text
1
```

Your node also published a verdict on the address, which your peers
count. Withdraw it:

```sh
sudo obiectl revoke 85.10.4.12
```

```text
Revoked verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 on ipv4:85.10.4.12 (revocation 01a0ef5b-7d51-7e12-8c07-2c1f4c3e9a10, reason false_positive).
```

The override lasts, but it lives in the node's state. Add the address to
the allow-list in the configuration too,
since the node never reports an address on its allow-list. This command
adds a line under `allowlist.cidrs`, as the setup assistant writes it; in
an editor, make the same change:

```sh
sudo sed -i -e 's/^  cidrs: \[\]$/  cidrs:/' -e '/^  cidrs:$/a\    - "85.10.4.12/32"' /etc/obie/obie.yaml
```

```sh
sudo grep -A 2 '^  cidrs:' /etc/obie/obie.yaml
```

```text
  cidrs:
    - "85.10.4.12/32"
    - "85.10.3.20/32"
```

The list must name the address. Check the file, and reload the node:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl reload obied
```

Last, tell Fail2Ban never to ban the address again, in a file of
addresses that all jails ignore. The first command creates the file,
unless it is there from an earlier time; the second adds the address to
it. If `/etc/fail2ban/jail.local` sets `ignoreip` already, add the
address there instead:

```sh
sudo test -f /etc/fail2ban/jail.d/trusted-addresses.local || printf '[DEFAULT]\nignoreip = 127.0.0.1/8 ::1\n' | sudo tee /etc/fail2ban/jail.d/trusted-addresses.local
sudo sed -i 's/^ignoreip = .*/& 85.10.4.12/' /etc/fail2ban/jail.d/trusted-addresses.local
```

```sh
sudo cat /etc/fail2ban/jail.d/trusted-addresses.local
```

```text
[DEFAULT]
ignoreip = 127.0.0.1/8 ::1 85.10.4.12
```

```sh
sudo systemctl restart fail2ban
```

## Check that it worked

The node allows the address:

```sh
sudo obiectl explain 85.10.4.12
```

```text
Indicator:             ipv4:85.10.4.12
Decision:              allowed
Reason:                operator force-allow override on ipv4:85.10.4.12 (note: "customer, mistyped password"); verdicts: no active verdicts
…
```

**In the console:** the explanation,
<http://127.0.0.1:9465/decisions/85.10.4.12>, now says
**Allowed: never blocked, whatever the verdicts**.

The firewall no longer blocks it; the list of blocks does not name it:

```sh
sudo obiectl enforced
```

```text
Entries applied: 1

PREFIX        EXPIRES               REMAINING
85.10.0.7/32  2026-10-14T09:22:05Z  5m38s
```

`85.10.0.7` stands for an attacker that stays blocked. Fail2Ban ignores
the address; it lists the addresses in no particular order:

```sh
sudo fail2ban-client get sshd ignoreip
```

```text
These IP addresses/networks are ignored:
…
…- 85.10.4.12
…
```
