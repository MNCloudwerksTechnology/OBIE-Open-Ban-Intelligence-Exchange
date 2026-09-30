# How do I find out why an address is blocked?

When someone cannot reach your server, find out whether your OBIE
[node](../glossary.md#node) blocks their address, which rule decided it,
who reported the address and since when. Then you know whether the block
is right, and whom to ask if it is not.

## Before you start

- Your node runs in [enforce mode](../glossary.md#enforce-mode), as
  [Get started](../getting-started.md#10-switch-to-enforcement-optional)
  leaves it after step 10. In [observe mode](../glossary.md#observe-mode),
  the same steps tell you why the node would block an address.
- You know the address. The person who cannot reach your server can look
  it up, for example on a web page that shows "what is my IP".
- For the console's way: the [web console](../operations/console.md) is
  switched on ([how](../operations/console.md#switch-it-on)) and open in
  your browser.

## Steps

`85.10.0.7` stands for the address. Ask the node about it:

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

Read it from the top:

- `Decision` says whether the node blocks the address, and until when.
- `Reason` names the rule that decided:
  - `local autoblock`: your own server reported it, here Fail2Ban's
    `sshd` jail; your own detections block at once.
  - `consensus`: enough trusted [peers](../glossary.md#peer) agree; their
    [verdicts](../glossary.md#verdict) reach the
    [threshold](../glossary.md#threshold) and the
    [quorum](../glossary.md#quorum).
  - `operator force-block`: you or another administrator blocked it by
    hand, with an [override](../glossary.md#override).
- Each row below is one publisher's verdict: `(this node)` or the name
  of a peer, with the [trust weight](../glossary.md#trust-weight) you
  gave it and whether its verdict counts.

**In the console:** the address's explanation,
<http://127.0.0.1:9465/decisions/85.10.0.7>, says
**Blocked by this node's own verdict (local autoblock)**, shows every
verdict with its **Trust weight**, and says under Firewall
**The firewall's own entry for it drops its traffic.**

To see what was reported, how often and when, list the verdicts on the
address. Each has an event ID, which a peer's operator can look up:

```sh
sudo obiectl show 85.10.0.7
```

```text
Indicator:        ipv4:85.10.0.7
Active verdicts:  1

PUBLISHER    ACTION  CONFIDENCE  EVENTS  PROTOCOL  REASON      ISSUED                EXPIRES               EVENT ID
(this node)  ban     0.8         5       ssh       bruteforce  2026-10-14T09:12:05Z  2026-10-14T09:22:05Z  01a0ef55-20f8-7627-97e6-0a7cd4dd1560
```

Here Fail2Ban counted 5 failed logins. To see since when the node blocks
the address, and what else happened to it, look at the node's history:

**In the console:** the **Activity** page, filtered to the address,
<http://127.0.0.1:9465/activity?address=85.10.0.7>, lists
**Reported by this node** and **Block added**, newest first.

On the command line, the same history is in the
[audit log](../operations/monitoring.md#audit-log), one JSON line per
entry:

```sh
sudo grep '"ip":"85.10.0.7"' /var/log/obie/audit.jsonl
```

```text
{"@timestamp":"2026-10-14T09:12:05.256Z","event":{"kind":"event","module":"obie","dataset":"obie.audit","action":"local-report",…
{"@timestamp":"2026-10-14T09:12:05.256Z","event":{"kind":"event","module":"obie","dataset":"obie.audit","action":"block-added",…
…
```

`action` says what happened, and `reason` why.

## Check that it worked

Make sure that it is OBIE's firewall table that blocks the address. It
is, if the address is among the blocks the node applies:

```sh
sudo obiectl enforced
```

```text
Entries applied: 1

PREFIX        EXPIRES               REMAINING
85.10.0.7/32  2026-10-14T09:22:05Z  5m38s
```

Fail2Ban blocks the addresses it bans in its own firewall rules too, and
that block stays when OBIE lets the address through. Ask Fail2Ban's jail:

```sh
sudo fail2ban-client status sshd
```

```text
Status for the jail: sshd
…
   `- Banned IP list:	85.10.0.7
```

The `sshd` jail bans it too. If `obiectl explain` says `Decision: none`
or `allowed`, and no jail lists the address, something other than OBIE
and Fail2Ban blocks it: your own firewall rules, or the network on the
way.

## Undo

There is nothing to undo: these commands and the console's pages only
read, and change nothing. If the address should not be blocked, see
[How do I unblock an address I trust?](unblock-an-address.md).
