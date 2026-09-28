# Frequently asked questions

Honest answers to the questions people ask before they let OBIE near their
firewall. Each answer states what OBIE v0.1 protects against and what risk
remains. New to OBIE? Start with [What is OBIE?](introduction.md) Every
term is explained in the [glossary](glossary.md).

## Can a peer lock me out?

Not on its own: with the default settings, no single
[peer](glossary.md#peer) can get any address blocked on your server.

- **It takes several peers who agree.** A [verdict](glossary.md#verdict)
  from another [node](glossary.md#node) counts only with the
  [trust weight](glossary.md#trust-weight) you gave that node. Your node
  blocks an address only when the combined score reaches the
  [threshold](glossary.md#threshold) and enough different nodes agree, the
  [quorum](glossary.md#quorum). With the recommended weight of 0.8, at least
  three peers have to agree, and nodes you have not listed count for nothing.
- **Some addresses can never be blocked.** Your node's own addresses, the
  peers you told it to connect to and internal networks are
  [protected addresses](glossary.md#protected-addresses): no verdict and no
  [override](glossary.md#override) can block them.
- **Your [allow-list](glossary.md#allow-list) beats every peer.** Before
  you let OBIE block anything, put on it what you cannot afford to lose:
  the networks you administer from, your monitoring, DNS resolvers and
  gateways, and your server's public address if it sits behind a router
  (NAT). `obiectl allow <address>` exempts an address at once.
- **Nothing is blocked until you say so.** A new node starts in
  [observe mode](glossary.md#observe-mode) and only shows what it would
  block; it does share its own verdicts from the start. It blocks only in
  [enforce mode](glossary.md#enforce-mode), which you switch on yourself.
- **You can see why, and every block ends.** `obiectl explain <address>`
  shows which peers reported an address and why it is or is not blocked. A
  block ends on its own when the verdicts behind it expire, after 30 days
  at most unless their publishers renew them.

What remains: peers you trust can still agree on a wrong address, and you
can loosen the defaults, for example to a quorum of 1, so that a single peer
is enough. The most likely way to lock yourself out is not a peer at all,
but your own Fail2Ban banning your own address: your node's own detections
block at once. Put your own addresses into Fail2Ban's `ignoreip` as well as
on the allow-list, and keep a console that does not depend on the network;
[Locked out](operations/troubleshooting.md#locked-out) shows how to get back
in and remove every OBIE block with one command.

## What does OBIE share about me?

Only the verdicts your node publishes, and they only travel through your
[mesh](glossary.md#mesh): to your peers and on to theirs. Your node shares
them in observe mode too.

A verdict your node publishes holds:

- the attacking address, which is never an internal one;
- the attacked service, for example `ssh` (for a Fail2Ban jail OBIE does
  not recognise, the jail's name), and a reason code such as `bruteforce`;
- how many attempts were seen, whether a honeypot saw them (always no in
  v0.1), the suggested action, a [confidence](glossary.md#confidence) and
  how long it should last;
- optionally, attack technique codes such as the MITRE ATT&CK ID `T1110`;
- an [evidence hash](glossary.md#evidence-hash): a fingerprint of the
  matched log lines that cannot be turned back into them;
- a unique ID, the time, your node's [peer ID](glossary.md#peer-id) and a
  [signature](glossary.md#signature).

A [revocation](glossary.md#revocation), which withdraws a verdict, names
the verdict and a reason code such as `false_positive`.

Never shared: log lines, user names, passwords, host names or anything else
about your users. The format has no field for them.

What peers can still learn:

- that your server was attacked, from which address, on which service and
  when, and so which services you run;
- your server's internet address, every address its node listens on
  (with the default settings, internal ones too) and its OBIE version;
- whether a guess about your logs is right: the fingerprint is not salted,
  so a peer that guesses the exact log lines (a common user name, a
  well-known attack) can confirm its guess.

Connections between nodes are encrypted, but every node that passes a
verdict on can read it. Attacker addresses count as personal data in many
jurisdictions, for example under the GDPR in the EU; sharing them is your
responsibility as operator. The threat model has the details under
[privacy leakage](../SECURITY.md#privacy-leakage).

## What if a peer is malicious?

It can waste some of your bandwidth and disk, but it cannot take over your
firewall unless you trust it too much.

- **Forged and broken verdicts are dropped.** Every verdict is checked
  against its signature before it is stored or passed on. A peer that keeps
  passing on invalid verdicts is scored down and eventually ignored.
- **It counts only as much as you trust it.** Its verdicts count with the
  weight you gave it, and under the default quorum never on their own.
  Nodes you have not listed count for nothing, so creating many fake nodes
  cannot get anything blocked.
- **Floods are capped.** Your node accepts only a limited number of
  verdicts per second from each publisher and from each connected peer, and
  keeps a limited number in total. When its store is full, the verdicts
  that expire first make room, whoever sent them, so a flood from many
  fake nodes can push out verdicts of peers you trust.
- **You can see its influence and remove it.** `obiectl explain` and, if you
  turned it on, the audit log show which publishers caused each block.
  Remove the peer from `trust.publishers` and reload: its verdicts stop
  counting at once, and the blocks that relied on them are lifted.

What remains: trusted peers that work together, or whose secret keys were
stolen, can get any public address, or a whole range of addresses, blocked
on your server until you notice. Trust is a number you set; OBIE v0.1 does
not yet learn from a peer's track record. Any node that reaches your OBIE
port can connect and use bandwidth, CPU and disk, so open that port only to
your peers. The threat model covers
[poisoning by a trusted peer](../SECURITY.md#poisoning-by-a-trusted-peer)
and [fake peers](../SECURITY.md#sybil-peers).

## What happens if OBIE crashes?

Your server keeps running and traffic keeps flowing; OBIE never blocks
everything when it fails.

- **Traffic keeps flowing.** The OBIE program is not in the path of your
  traffic. Its firewall table only drops the addresses it lists and lets
  everything else through, whether OBIE runs or not.
- **Existing blocks run out on time.** Every block in the firewall carries
  its own end time, and the Linux kernel removes it on time even while OBIE
  is down. No new blocks are added in the meantime.
- **Fail2Ban carries on.** It keeps banning as before. Bans it reports
  while OBIE is down are written to the system log and not shared; the ban
  itself is not affected.
- **OBIE restarts by itself.** The shipped systemd service starts it again
  five seconds after a crash. The node reloads its stored verdicts,
  rebuilds its decisions and brings its firewall table back in line.
- **What you miss:** verdicts and withdrawals your peers publish while your
  node is down do not arrive later, so a block a peer withdrew in the
  meantime stays until it expires; v0.1 has no catch-up.

In observe mode a crash does not touch your firewall at all, because OBIE
has not changed it. To remove every OBIE block whenever the service stops,
set `enforce.nftables.teardown_on_stop: true`
([nftables guide](guides/nftables.md)).

## How is this different from blocklists or CrowdSec?

OBIE has no provider in the middle: every warning comes signed from a
server you chose, and your server weighs it itself.

**Blocklists** are lists of bad addresses that one provider collects and
publishes, some free, some paid. You trust the provider as a whole: you
often cannot see why an address is listed, and a mistake or an outage at the
provider reaches every subscriber at once. With OBIE, every entry is a
verdict signed by a node you chose, it counts only as far as you trust that
node, and `obiectl explain` shows who reported an address and why it is or
is not blocked.

**CrowdSec** is a mature open-source security engine. It detects attacks
in your logs and blocks them through remediation components (bouncers). Its
[community blocklist](https://docs.crowdsec.net/docs/central_api/community_blocklist/)
is built centrally: participating engines send their signals to CrowdSec's
central service, which cross-checks them and sends a curated list back.
OBIE has no central service: nodes exchange signed verdicts directly with
the peers their operators chose, and each node weighs them with its own
trust settings. CrowdSec is far more mature and covers many more attacks
and integrations; OBIE v0.1 is a first release that handles attacking IP
addresses reported by Fail2Ban and blocks them with nftables.

**You can use them together.** OBIE only manages its own firewall table,
so it runs alongside blocklists, CrowdSec or your own rules.

Your question is not answered here?
[Ask it in an issue](https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/issues).
