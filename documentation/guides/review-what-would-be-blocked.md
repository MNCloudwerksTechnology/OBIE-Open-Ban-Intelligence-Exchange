# How do I review what my node would block before enforcing?

In [observe mode](../glossary.md#observe-mode), your
[node](../glossary.md#node) decides what it would block and blocks
nothing. Look at those decisions before you switch to
[enforce mode](../glossary.md#enforce-mode): every address the node would
block should be an attacker, and none of your own addresses may be among
them. Do it every day or two during the first week.

## Before you start

- Your node runs in observe mode and is connected to a
  [peer](../glossary.md#peer), as
  [Get started](../getting-started.md#8-connect-to-a-peer) leaves it after
  step 8. Without a peer, the review works the same, with your own
  detections only.
- For the console's way: the [web console](../operations/console.md) is
  switched on ([how](../operations/console.md#switch-it-on)) and open in
  your browser; from your workstation, through an SSH port forward
  ([how](../operations/console.md#reach-it-from-another-machine)).

## Steps

List the addresses the node would block:

```sh
sudo obiectl decisions --state block
```

```text
Decisions: 1 (1 block, 0 allowed, 0 none)

INDICATOR       STATE  SCORE  PUBLISHERS  EXPIRES               REASON
ipv4:85.10.0.7  block  0.8    1           2026-10-14T09:22:05Z  local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
```

The first line counts the decisions listed. `85.10.0.7` stands
for an address that your Fail2Ban banned: the node would block it at
once, because it trusts this server's own detections
([local autoblock](../glossary.md#local-autoblock)).

**In the console:** the **Decisions** page,
<http://127.0.0.1:9465/decisions?state=block>, lists the same addresses,
with **local autoblock** as what decided, and says in its Firewall column
**observe mode: nothing is applied, by design**.

Then list every decision, also those that would not block:

```sh
sudo obiectl decisions
```

```text
Decisions: 2 (1 block, 0 allowed, 1 none)

INDICATOR        STATE  SCORE  PUBLISHERS  EXPIRES               REASON
ipv4:85.10.0.7   block  0.8    1           2026-10-14T09:22:05Z  local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
ipv4:85.10.0.66  none   0.64   1           -                     below consensus: score 0.64 < threshold 1.8, 1 < quorum 2
```

`85.10.0.66` stands for an address that your peer reported. The state
`none` means that too few trusted nodes agree yet: the score is below the
[threshold](../glossary.md#threshold), or fewer nodes than the
[quorum](../glossary.md#quorum) reported it. When more of your peers
report it, the decision can turn into `block`. A decision `allowed` means
that the [allow-list](../glossary.md#allow-list) or an
[override](../glossary.md#override) protects the address.

For each address in the block list, ask the node why: which
[verdicts](../glossary.md#verdict) it holds on the address, and the
[trust weight](../glossary.md#trust-weight) of each node that sent one:

```sh
sudo obiectl explain 85.10.0.7
```

```text
Indicator:             ipv4:85.10.0.7
Decision:              block until 2026-10-14T09:22:05Z
Reason:                local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
Score:                 0.8 (threshold 1.8)
Publishers:            1 (quorum 2)
Local autoblock:       yes
Allow-list/overrides:  none apply
Evaluated:             2026-10-14T09:15:40Z

PUBLISHER    PEER ID                                               ACTION  WEIGHT  CONFIDENCE  SCORE  COUNTS  PROTOCOL  REASON      ISSUED                EXPIRES
(this node)  12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD  ban     1       0.8         0.8    yes     ssh       bruteforce  2026-10-14T09:12:05Z  2026-10-14T09:22:05Z
```

**In the console:** choose the address in the list. Its explanation,
<http://127.0.0.1:9465/decisions/85.10.0.7>, says
**Blocked by this node's own verdict (local autoblock)** and shows one row
per publisher, with **Trust weight** and **Counts**.

An address that should not be on the list is a false positive: see
[How do I unblock an address I trust, now and for good?](unblock-an-address.md). If one peer
keeps reporting addresses you trust, see
[How do I stop trusting a peer?](stop-trusting-a-peer.md).

## Check that it worked

Your review is complete when you know why each address in the block list
is there, and when every address you administer the server from is
safe. Ask the node about each of them. `85.10.3.20` stands for the
address of your SSH session:

```sh
sudo obiectl explain 85.10.3.20
```

```text
Indicator:             ipv4:85.10.3.20
Decision:              allowed
Reason:                allow-listed: allowlist.cidrs entry 85.10.3.20/32; verdicts: no active verdicts
…
```

`Decision: allowed` means that the node never blocks the address. For any
address that says something else, protect it before you enforce, as
[step 10 of Get started](../getting-started.md#protect-your-own-access)
shows. Nothing is blocked while the node observes:

```sh
sudo obiectl enforced
```

```text
No entries applied: the node is in observe mode.
```

## Undo

There is nothing to undo: these commands and the console's pages only
read, and change nothing. The lists show what is active now; the
[audit log](../operations/monitoring.md#audit-log) and the console's
*Activity* page keep what the node decided before.
