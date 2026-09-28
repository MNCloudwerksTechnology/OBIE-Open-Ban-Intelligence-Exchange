# What is OBIE?

OBIE, the Open Ban Intelligence Exchange, lets servers warn each other
about attackers. Each server still decides for itself what it blocks. This
page explains in about five minutes what that means, how it works and why
it cannot lock you out of your own server. You need no technical
background to follow it.

## The problem: every server fights alone

Every server on the internet is attacked all day. Automated programs try
thousands of passwords and look for weak spots. The same attacking
addresses hit thousands of servers, one after the other.

Yet each server has to learn about an attacker the hard way: by being
attacked itself. A tool such as [Fail2Ban](glossary.md#fail2ban) notices
the failed logins and shuts the attacker out, but only on that one server.
The next server starts from nothing.

The usual shortcut is a blocklist: a list of bad addresses that one
provider collects and hands out, often for a fee. You have to trust that
provider completely. You cannot see why an address is on the list. And
when the provider makes a mistake, everyone who uses the list is affected
at once.

## The idea: servers warn each other

OBIE lets servers warn each other directly. Each server runs a small OBIE
program, called a [node](glossary.md#node). When a server is attacked, its
node writes a short warning: "this address attacked me, block it for a
week". OBIE calls this warning a [verdict](glossary.md#verdict).

Every verdict carries a digital [signature](glossary.md#signature), a kind
of seal. It proves which node wrote the verdict and that nobody changed it
on the way. A forged or altered verdict is thrown away.

A node sends its verdicts only to its [peers](glossary.md#peer): other
nodes whose operators know each other and chose to connect. A peer can be
a friend's server, a partner organisation or another server of your own.
There is no central service, no account and no company in the middle that
decides for you. Nobody can switch OBIE off for everyone.

## What happens on your server

A verdict is advice, never an order. Your node alone decides what to do
with it.

For each peer, you set a [trust weight](glossary.md#trust-weight): a
number from 0 to 1 that says how much you trust its judgement. A friend
you know well might get 0.8. By default, nodes you have not listed count
for nothing, so strangers cannot vote.

For every reported address, your node adds up what the verdicts are worth.
Each verdict counts with the trust you gave its sender. Your node blocks
the address only when two conditions are met:

- The total reaches a limit you set, the
  [threshold](glossary.md#threshold).
- Enough different nodes agree, the [quorum](glossary.md#quorum).

With the default settings, one peer alone can never get an address blocked
on your server. With the recommended trust of 0.8, it takes at least three
peers who agree.

Your server's own detections are different. When your own Fail2Ban bans an
address, your node blocks it at once, because you trust your own server
fully.

Every block lasts a limited time and then ends on its own, so a mistake
does not last forever. You can ask your node at any time why an address is
or is not blocked. And you can overrule your peers for any address: always
allow it, or always block it. OBIE calls this an
[override](glossary.md#override).

## What can never happen

OBIE is built so that you keep control of your server, whatever other nodes
say. This principle is called [sovereignty](glossary.md#sovereignty).

- **Your server's own addresses and the protected addresses are never
  blocked.** Your node never blocks its own addresses or the peers you told
  it to connect to. The same holds for internal addresses that the internet
  cannot reach. Nothing can override this, not even a manual block. The
  addresses you work from, such as your office's internet address, go on
  your [allow-list](glossary.md#allow-list). Then no peer can get them
  blocked.
- **Nothing is enforced until you switch it on.** A new node starts in
  [observe mode](glossary.md#observe-mode). It decides and shows you what
  it would block, but it does not touch your
  [firewall](glossary.md#firewall), the part of your server that lets
  connections in or keeps them out. It only blocks once you switch it to
  [enforce mode](glossary.md#enforce-mode) yourself. Even then it only
  changes its own part of the firewall, never your own rules. Removing that
  part removes every OBIE block at once.
- **Raw logs never leave your server.** Your log files stay where they
  are. A verdict only carries a fingerprint of the log lines, which cannot
  be turned back into the lines, and the number of failed attempts. User
  names, passwords and the contents of your logs are never shared.

Your peers do learn which address attacked your server, on which service
and when. That tells them a little about which services you run. Like any
connection, it also shows them your server's internet address. The
[FAQ](faq.md#what-does-obie-share-about-me) lists exactly what is shared.

## One attack, start to finish

![Diagram of one attack in five numbered steps, from left to right: detection on server A, a signed verdict, sent to the peers, a trust-weighted decision on your server, and a block in your firewall. The list below describes each step in words.](images/one-attack.svg)

The same journey in words:

1. **Detection.** An attacker tries to guess passwords on server A.
   Fail2Ban on server A notices the failed logins and bans the attacker
   there.
2. **Signed verdict.** Server A's node writes a verdict: "block this
   address for a week". It signs the verdict, so nobody can fake or change
   it. The log lines stay on server A.
3. **Peers.** The verdict goes to the peers that server A's operator chose,
   your server among them. Servers B and C saw the same attacker and send
   their own verdicts.
4. **Trust-weighted decision.** Your node weighs each verdict by the trust
   you gave its sender. Three trusted peers agree, which is enough. The
   address is not on your allow-list.
5. **Firewall.** In enforce mode, your firewall now keeps the attacker out.
   The block ends by itself when the verdicts expire. In observe mode, your
   node only shows that it would block.

## Is OBIE for you?

OBIE may be for you if:

- You run one or more Linux servers that are reachable from the internet.
- You already use Fail2Ban, or would like to.
- You know other server operators you trust enough to exchange warnings
  with, or you run several servers yourself.

OBIE is not for you yet if:

- Your servers run Windows or BSD.
- You want to join a ready-made public network. In this first version, you
  connect to each peer by hand.
- You need protection beyond attacking addresses, for example against
  harmful websites or files.
- You need a mature product with support. OBIE is at version 0.1, its
  first release.

OBIE is free and open source. There is no account and no subscription. It
runs next to the tools you already use and does not replace them.

## Where to go next

- [Frequently asked questions](faq.md): can a peer lock you out, what is
  shared, what if OBIE crashes, and how it differs from blocklists.
- [Glossary](glossary.md): every OBIE term in one or two sentences.
- [What version 0.1 does](../README.md#what-v01-does), and what it does
  not do yet.
- [Try it on a laptop](../packaging/compose/README.md): three nodes that
  block nothing real, in a few minutes.
- [Quick start](operations/quickstart.md): a first node, safely in observe
  mode, in about half an hour.
- [Threat model](../SECURITY.md#threat-model): the risks OBIE protects
  against, and the ones that remain.

Was anything on this page unclear? It is written for newcomers, so please
[tell us in an issue](https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/issues).
