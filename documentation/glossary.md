# Glossary

Every OBIE term you meet in this documentation, each explained in one or
two sentences. The pages link a term here the first time they use it. New
to OBIE? Start with [What is OBIE?](introduction.md)

## Allow-list

The list of addresses your node never blocks, whatever its peers report.
It holds the [protected addresses](#protected-addresses) and every network
you add yourself (`allowlist.cidrs`), such as your office, your VPN or
your monitoring.

## Bootstrap peer

A [peer](#peer) that your node connects to at start and keeps connected,
because you listed it in the configuration (`mesh.bootstrap`). Its
addresses are [protected](#protected-addresses), so your node never cuts
itself off from it.

## Confidence

How sure the [publisher](#publisher) of a [verdict](#verdict) is, from 0
to 1; a [Fail2Ban](#fail2ban) ban is reported with 0.8 unless you set
another value. Your node multiplies it by the publisher's
[trust weight](#trust-weight) to get the verdict's share of the
[score](#score).

## Enforce mode

The mode in which your node applies its decisions: it puts the addresses
it decided to block into its own [firewall](#firewall) table. You switch
it on yourself (`node.mode: enforce` with `enforce.backend: nftables`); a
new node starts in [observe mode](#observe-mode).

## Evidence hash

A fingerprint of the log lines behind a verdict, made on your server; it
cannot be turned back into the lines. Only the fingerprint and the number
of events are shared, the log lines themselves never leave your server.

## Fail2Ban

A widely used program that watches log files and bans an address after
repeated failed logins. With one extra line in its configuration, every
ban also becomes a [verdict](#verdict) of your node.

## Federation

Two or more [operators](#operator) connecting their nodes and choosing to
trust each other's verdicts. Each operator decides for their own node whom
it connects to and how much it trusts them.

## Firewall

The part of a server that decides which network connections to let in.
OBIE blocks addresses through the Linux firewall [nftables](#nftables) and
only ever changes its own table there, never your own rules.

## Indicator

What a [verdict](#verdict) is about: in OBIE v0.1 always one internet
address (IPv4 or IPv6) or a range of such addresses. Private and internal
addresses are never accepted as indicators.

## Lifetime (TTL)

How long a verdict counts before it ends on its own; the protocol allows
at most 30 days. A block ends when the verdicts behind it expire, unless
their publishers renew them, and each decision is capped at
`decision.max_ttl` (30 days by default).

## Local autoblock

Your node's own detections block on your server at once, without waiting
for other nodes to agree (`decision.local_autoblock`, on by default).
Verdicts of other nodes always have to pass the [threshold](#threshold)
and the [quorum](#quorum).

## Mesh

All the nodes that are connected with each other and pass verdicts along.
It has no central server, and in v0.1 there is no public mesh to join:
you connect to the peers you list yourself.

## nftables

The standard firewall of current Linux systems. In
[enforce mode](#enforce-mode), OBIE blocks addresses in its own nftables
table `inet obie` and leaves every other table alone.

## Node

One running copy of OBIE on one server: the program `obied`, with its own
identity, its own [trust weights](#trust-weight) and its own decisions.
You control it with the command `obiectl` or, if you switch it on, its
local [web console](operations/console.md).

## Observe mode

The mode every node starts in: it decides and shows you what it would
block, but blocks nothing, while its own verdicts are still shared with its
peers. It lets you check OBIE's judgement before you let it touch your
[firewall](#firewall).

## Operator

The person who runs a node, so for your node: you. Only the operator
decides whom the node trusts, what it must never block and whether it
blocks at all.

## Override

Your own decision about one address, which beats what the mesh says:
always allow it (`obiectl allow`) or always block it (`obiectl block`). An
always-allow wins over everything, an always-block over everything except
an always-allow and the [protected addresses](#protected-addresses).

## Peer

Another OBIE [node](#node) that your node is connected to. You choose which
nodes it connects to and how much it trusts each; nodes that connect on
their own count for nothing unless you trust them.

## Peer ID

The unique name of a node, derived from its public key, for example
`12D3KooWKrKn…`. The node signs its verdicts with the same key, so the
peer ID tells you who wrote a verdict.

## Protected addresses

Addresses that no verdict and no override can ever block: loopback,
private and other special-purpose networks, your node's own addresses and
the addresses of its [bootstrap peers](#bootstrap-peer). Networks you add
to the [allow-list](#allow-list) are safe from your peers' verdicts too.

## Publisher

The node that wrote and signed a [verdict](#verdict). Your node counts
each publisher at most once per address, with the
[trust weight](#trust-weight) you gave it.

## Quorum

The least number of different publishers, each with a trust weight above
0, that must report an address before your node blocks it
(`decision.quorum`, 2 by default). With the default, no single peer can
get anything blocked on its own.

## Revocation

A signed message in which a publisher withdraws one of its own verdicts,
for example after a false alarm (`obiectl revoke`). Only the publisher of
a verdict can revoke it.

## Score

The sum of [trust weight](#trust-weight) × [confidence](#confidence) over
all publishers that currently report an address. Your node blocks the
address when the score reaches the [threshold](#threshold) and enough
publishers agree ([quorum](#quorum)).

## Signature

A digital seal that a publisher's secret key puts on every verdict. With
it anyone can check who wrote the verdict and that nobody changed it, and
nobody without the key can forge it.

## Sovereignty

The rule that your node always has the last word over your firewall:
verdicts from others are advice, never orders. Your
[allow-list](#allow-list), your [overrides](#override) and the mode you
chose always win over the mesh.

## Threshold

The [score](#score) an address must reach before your node blocks it
(`decision.threshold`, 1.8 by default). With the default settings and a
trust weight of 0.8, at least three peers have to agree.

## Trust weight

How much you trust a publisher, from 0 (ignored) to 1 (as much as your own
node), set per peer in `trust.publishers`. Publishers you have not listed
get `trust.default_weight`, which is 0, so strangers never count.

## Verdict

A short signed report that says "this address attacked me, block it for
this long", with a [confidence](#confidence) and an
[evidence hash](#evidence-hash). Your node treats every verdict it
receives as advice and decides for itself what to do with it.
