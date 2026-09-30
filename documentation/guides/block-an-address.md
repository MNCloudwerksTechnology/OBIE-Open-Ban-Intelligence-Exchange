# How do I block an address manually?

Block an address that your [node](../glossary.md#node) does not block on
its own, for example a scanner you found in your web server's log. The
block holds on your node only. To warn your [peers](../glossary.md#peer)
too, report the address as well.

> **Warning:** In [enforce mode](../glossary.md#enforce-mode), this drops
> all traffic from the address at once. A manual block overrules your
> [allow-list](../glossary.md#allow-list): only the
> [protected addresses](../glossary.md#protected-addresses) are never
> blocked. Make sure that the address, or the range you block, holds none
> of yours: not your office, your monitoring or a customer.

## Before you start

- Your node runs in enforce mode, as
  [Get started](../getting-started.md#10-switch-to-enforcement-optional)
  leaves it after step 10. In [observe mode](../glossary.md#observe-mode),
  the node only shows that it would block the address.
- You know the address. `85.10.0.9` stands for it here.
- For the console's way: the [web console](../operations/console.md) is
  switched on ([how](../operations/console.md#switch-it-on)) and open in
  your browser.

## Undo

If you reported the address, withdraw your
[verdict](../glossary.md#verdict) first, so that your node and your peers
stop counting it:

```sh
sudo obiectl revoke 85.10.0.9
```

```text
Revoked verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 on ipv4:85.10.0.9 (revocation 01a0ef5b-7d51-7e12-8c07-2c1f4c3e9a10, reason false_positive).
```

Then remove the block, which the node lifts at once:

```sh
sudo obiectl unoverride 85.10.0.9
```

```text
Override on 85.10.0.9 removed.
Decision now: none — no active verdicts
```

## Steps

Block the address with an [override](../glossary.md#override).

**In the console:** open the address's explanation,
<http://127.0.0.1:9465/decisions/85.10.0.9>, even if the node knows
nothing about it yet. Choose **Always block…**, set when the block ends
and a note, choose *Review*, and confirm.

On the command line, `--ttl` ends the block on its own, here after 7
days; without it, the block stays until you remove it. The note tells you
later why you set it:

```sh
sudo obiectl block 85.10.0.9 --ttl 7d --note "scans the web server"
```

```text
Override set: force_block on ipv4:85.10.0.9, until 2026-10-21T09:18:08Z (note: "scans the web server").
Decision now: block — operator force-block override on ipv4:85.10.0.9 until 2026-10-21T09:18:08Z (note: "scans the web server"); verdicts: no active verdicts
```

To warn your peers as well, report what the address did. Your node signs
a verdict and sends it to them; their nodes count it with the
[trust weight](../glossary.md#trust-weight) they gave yours. Report only
what attacked your own server:

```sh
sudo obiectl report --protocol http --reason scanning 85.10.0.9
```

```text
Reported ipv4:85.10.0.9: verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 issued and published.

Indicator:   ipv4:85.10.0.9
Action:      ban
Confidence:  0.8
Protocol:    http
Reason:      scanning
Events:      1
Log hash:    -
Issued:      2026-10-14T09:18:08Z
Expires:     2026-10-21T09:18:08Z (7d)
Publisher:   12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD
```

## Check that it worked

The node blocks the address because of your override:

```sh
sudo obiectl explain 85.10.0.9
```

```text
Indicator:             ipv4:85.10.0.9
Decision:              block until 2026-10-21T09:18:08Z
Reason:                operator force-block override on ipv4:85.10.0.9 until 2026-10-21T09:18:08Z (note: "scans the web server"); verdicts: local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
…
```

The firewall applies the block:

```sh
sudo obiectl enforced
```

```text
Entries applied: 2

PREFIX        EXPIRES               REMAINING
85.10.0.7/32  2026-10-14T09:22:05Z  5m38s
85.10.0.9/32  2026-10-21T09:18:08Z  167h59m52s
```

`sudo obiectl overrides` lists every override you set, with its note.
