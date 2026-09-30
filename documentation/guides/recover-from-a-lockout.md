# How do I recover after locking myself out?

You cannot reach your server any more, and suspect that OBIE blocks your
address. Get back in, lift the block, find out why it happened, and make
sure that your [node](../glossary.md#node) never blocks the address
again.

> **Warning:** Removing OBIE's firewall table lifts every block of OBIE
> at once, those on attackers included, until the node blocks again in
> [enforce mode](../glossary.md#enforce-mode). Your own firewall rules and
> Fail2Ban's bans stay.

## Before you start

- Your node runs in enforce mode, as
  [Get started](../getting-started.md#10-switch-to-enforcement-optional)
  leaves it after step 10.
- You can get into the server in a way that does not depend on the
  address that is blocked: your provider's web console, IPMI or KVM, or a
  host on the same private network, since OBIE never blocks private
  addresses. Run every command below there.
- You know the address you were locked out from. `85.10.3.30` stands for
  it here: your address at home, say.

## Undo

The steps leave your node in [observe mode](../glossary.md#observe-mode),
in which it blocks nothing. Once the address is protected, switch
enforcement on again; the node puts back every block it decides:

```sh
sudo sed -i 's/^  mode: observe$/  mode: enforce/' /etc/obie/obie.yaml
sudo systemctl restart obied
```

```sh
sudo obiectl enforced
```

```text
Entries applied: 1

PREFIX        EXPIRES               REMAINING
85.10.0.7/32  2026-10-14T09:22:05Z  5m38s
```

The list names the attackers the node blocks again, `85.10.0.7` here, and
not your address.

## Steps

Stop the node, and remove its firewall table with every block in it. Stop
it first: in enforce mode, a running node puts its blocks back.

```sh
sudo systemctl stop obied
```

```sh
sudo obied teardown-firewall
```

```text
obied teardown-firewall: table inet obie removed; nothing is blocked by OBIE anymore
```

If `obied` is not installed any more, `sudo nft delete table inet obie`
does the same. Fail2Ban blocks the addresses it bans in rules of its own;
see whether its jail banned you:

```sh
sudo fail2ban-client status sshd
```

```text
Status for the jail: sshd
…
   `- Banned IP list:	85.10.0.7 85.10.3.30
```

It did. Lift its ban; `1` means that it lifted one:

```sh
sudo fail2ban-client set sshd unbanip 85.10.3.30
```

```text
1
```

Now try to log in from your address again. Then find out why the node
blocked it. Start the node in observe mode, which blocks nothing, and ask
it:

```sh
sudo sed -i 's/^  mode: enforce$/  mode: observe/' /etc/obie/obie.yaml
sudo systemctl start obied
```

```sh
sudo obiectl explain 85.10.3.30
```

```text
Indicator:             ipv4:85.10.3.30
Decision:              block until 2026-10-14T10:12:05Z
Reason:                local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
…
```

`local autoblock` means that your own Fail2Ban banned the address, for
example after you mistyped your password. If the reason names
`consensus`, your [peers](../glossary.md#peer) reported it: ask their
operators why.

Protect the address with an [override](../glossary.md#override), which
works at once and beats every other rule:

```sh
sudo obiectl allow 85.10.3.30 --note "my address at home"
```

```text
Override set: force_allow on ipv4:85.10.3.30, until removed (note: "my address at home").
Decision now: allowed — operator force-allow override on ipv4:85.10.3.30 (note: "my address at home"); verdicts: local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
```

Your node also sent its [verdict](../glossary.md#verdict) on your address
to your peers. Withdraw it, so that they do not block you either:

```sh
sudo obiectl revoke 85.10.3.30
```

```text
Revoked verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 on ipv4:85.10.3.30 (revocation 01a0ef5b-7d51-7e12-8c07-2c1f4c3e9a10, reason false_positive).
```

To protect the address for good, also add it to the
[allow-list](../glossary.md#allow-list) and to Fail2Ban's `ignoreip`, as
[How do I unblock an address I trust?](unblock-an-address.md) shows.

## Check that it worked

The node never blocks your address:

```sh
sudo obiectl explain 85.10.3.30
```

```text
Indicator:             ipv4:85.10.3.30
Decision:              allowed
Reason:                operator force-allow override on ipv4:85.10.3.30 (note: "my address at home"); verdicts: no active verdicts
…
```

To lift every block with a plain `sudo systemctl stop obied` next time,
set `enforce.nftables.teardown_on_stop: true`
([configuration](../operations/configuration.md#enforce)). Keep a way into
the server that does not depend on its network.
