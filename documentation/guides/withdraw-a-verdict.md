# How do I withdraw a verdict I published by mistake?

Withdraw a [verdict](../glossary.md#verdict) that your
[node](../glossary.md#node) published on the wrong address, for example
after a typo in `obiectl report`, so that your node and your
[peers](../glossary.md#peer) stop counting it. OBIE calls this a
[revocation](../glossary.md#revocation).

> **Warning:** In [enforce mode](../glossary.md#enforce-mode), this lifts
> your node's block on the address at once, and your peers' blocks that
> needed your verdict as soon as they receive the revocation.

## Before you start

- Your node is connected to its peers, as
  [Get started](../getting-started.md#8-connect-to-a-peer) leaves it after
  step 8. Peers receive a revocation only while they are connected, so
  withdraw a verdict as soon as you notice the mistake.
- You know the address you reported by mistake. `85.10.0.19` stands for
  it here: you meant `85.10.0.9`.
- For the console's way: the [web console](../operations/console.md) is
  switched on ([how](../operations/console.md#switch-it-on)) and open in
  your browser.

## Undo

A revocation cannot be taken back. If the verdict was right after all,
report the address again, which publishes a new verdict:

```sh
sudo obiectl report --protocol ssh --reason password_bruteforce 85.10.0.19
```

```text
Reported ipv4:85.10.0.19: verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 issued and published.
…
```

## Steps

List the verdicts your node published, and find the wrong one:

```sh
sudo obiectl indicators --mine
```

```text
INDICATOR        PUBLISHER    ACTION  CONFIDENCE  EVENTS  PROTOCOL  REASON               EXPIRES
ipv4:85.10.0.19  (this node)  ban     0.8         1       ssh       password_bruteforce  2026-10-21T09:18:08Z
ipv4:85.10.0.7   (this node)  ban     0.8         5       ssh       bruteforce           2026-10-14T09:22:05Z
```

Withdraw your node's verdicts on the address.

**In the console:** the address's explanation,
<http://127.0.0.1:9465/decisions/85.10.0.19>, offers
**Revoke my verdict…** while your node holds a verdict on it. Choose it,
keep or change the reason, choose *Review*, and confirm.

On the command line, the reason tells your peers why;
`false_positive` is the default:

```sh
sudo obiectl revoke 85.10.0.19
```

```text
Revoked verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 on ipv4:85.10.0.19 (revocation 01a0ef5b-7d51-7e12-8c07-2c1f4c3e9a10, reason false_positive).
```

To withdraw a single verdict and keep your others on the same address,
revoke it by its event ID, which `sudo obiectl show 85.10.0.19` lists.

## Check that it worked

The verdict is gone from your node's list:

```sh
sudo obiectl indicators --mine
```

```text
INDICATOR       PUBLISHER    ACTION  CONFIDENCE  EVENTS  PROTOCOL  REASON      EXPIRES
ipv4:85.10.0.7  (this node)  ban     0.8         5       ssh       bruteforce  2026-10-14T09:22:05Z
```

Your node no longer decides anything on the address:

```sh
sudo obiectl explain 85.10.0.19
```

```text
Indicator:             ipv4:85.10.0.19
Decision:              none
…
No active verdicts.
```

Your peers drop the verdict as soon as they receive the revocation. Ask
their operators to check with `sudo obiectl show 85.10.0.19`.
