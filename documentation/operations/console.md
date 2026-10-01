# Web console

The web console is a window onto a running [node](../glossary.md#node),
in your browser, from which you can also [act](#act-from-the-console) on
what you see: allow, block, remove an [override](../glossary.md#override),
report or [revoke](../glossary.md#revocation), each after a confirmation.
It is built into `obied`, loads nothing from outside the node and
works offline. It is **off by default** and, when on, reachable **only
from the node's own host**, only by the local users who may run `obiectl`,
and only with a token that `obied` keeps in memory. Its security model and
the threats it was designed against are in
[ADR 0019](../adr/0019-local-web-console.md).

Every page shows the node, whether it runs in
[observe mode](../glossary.md#observe-mode) or
[enforce mode](../glossary.md#enforce-mode), and its health: *Starting*,
*Ready*, *Degraded* (naming the subsystem and why) or *Shutting down*. The
first page, the [overview](#the-overview), tells you within seconds
whether the node is healthy and what it is doing. The other pages show:

- what the node did and does, live: the
  [activity timeline](#the-activity-timeline);
- every [peer](../glossary.md#peer) the node knows and the trust placed in
  it: the [peers view](#the-peers-view);
- every address the node decided on, and
  [why](#why-an-address-is-or-is-not-blocked) it is or is not blocked: the
  [decisions view](#the-decisions-view);
- what the firewall applies: the [firewall view](#the-firewall-view);
- the [verdicts](../glossary.md#verdict) this node told the mesh and those
  the mesh told it: the [verdicts view](#the-verdicts-view);
- every override and [allow-list](../glossary.md#allow-list) entry, and
  the configuration the node runs with: the
  [overrides view](#the-overrides-view), the
  [allow-list view](#the-allow-list-view) and the
  [configuration view](#the-configuration-view).

[Act from the console](#act-from-the-console) says how to fix a false
positive in seconds, and how to keep the console strictly read-only.

## Switch it on

Set `console.enabled` in `/etc/obie/obie.yaml` and reload the node. The
reload starts the console; nothing else restarts:

```yaml
console:
  enabled: true
  listen: 127.0.0.1:9465
```

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl reload obied
```

`console.listen` must be a loopback address: `127.0.0.1` (or another
`127.0.0.0/8` address) or `::1`, with a port. Any other value — `0.0.0.0`,
an empty host, an interface address, a host name, even `localhost` — is a
configuration error, because it would expose the console to the network.
Set `console.enabled: false` and reload to switch it off again. A reload
with another `console.listen` moves the console; if the new address is
taken, it keeps serving at the old one and says why. See the
[configuration reference](configuration.md#console).

## Sign in

The console admits root, the user `obied` runs as (`obie`) and members of
`admin.socket_group` (`obie`) — the same users as `obiectl` — and asks
them for the console token. Show it, with the console's address:

```sh
sudo obiectl console
```

```text
Console:  serving at http://127.0.0.1:9465/
Token:    <43 random letters, digits, - and _>
```

Open the address in a browser **on the node's host** and paste the token.
The session lasts 12 hours or until the browser closes. *Sign out* removes
it from this browser; `sudo obiectl console --rotate` ends every session.

If your own user is not in the group, the console refuses the connection
before the sign-in page and says so. Add yourself, as for using `obiectl`
without `sudo`:

```sh
sudo usermod -aG obie "$USER"
```

## The overview

The overview answers "Is my node healthy, and what is it doing?" from top
to bottom. Times are in UTC, like `obiectl`'s. How the page reads its
numbers and decides what needs attention is recorded in
[ADR 0020](../adr/0020-console-overview.md).

- **Summary.** One line: *Healthy* (every part ready, nothing to do),
  *Needs attention* (how many conditions below), *Just started*,
  *Starting* or *Shutting down*, and when the data was read.
- **Needs attention.** Each condition in plain words with a next step,
  warnings first, then notes (see the table below). Without conditions the
  section is not shown.
- **Key numbers.** Connected peers (and how many of the configured
  `mesh.bootstrap` peers), held [indicators](../glossary.md#indicator)
  with their active verdicts,
  decisions by state (`block`, `none`, `allowed`), the entries the firewall
  applies, and active overrides. Each number links to the view that
  details it — *Peers connected* to the [peers view](#the-peers-view),
  *Indicators held* to the [verdicts view](#the-verdicts-view), the
  decisions to the [decisions view](#the-decisions-view) in that
  state, *Firewall entries* to the [firewall view](#the-firewall-view),
  *Active overrides* to the [overrides view](#the-overrides-view).
  *Firewall entries* also says why they differ from the decided blocks:
  blocks that share an
  entry with another block (the same range, or one inside a wider range),
  that the allow-list refuses, or that are over `enforce.max_entries`.
- **Recent activity.** The last 5 entries of the
  [activity timeline](#the-activity-timeline), with a link to all of
  them.
- **Parts of the node.** The readiness of the mesh, the store, the decision
  engine, enforcement, the admin interface and the other parts, with what
  each reports.
- **This node.** The mode and what it means, the peer ID, the key
  fingerprint, the version, the uptime and when the running configuration
  was loaded (at start or by a reload), with a link to the
  [configuration view](#the-configuration-view).

A part that is not running yet, or any more, is shown as such, and its
numbers say *Waiting* for it (or *Stopped*) instead of 0; the rest of the
page works as usual.

**Right after a start.** For the first 2 minutes, a node without peers or
data shows *This node has just started* instead of conditions: what will
appear — peers, verdicts, decisions, blocks — and when. Numbers that are
still zero read *None yet*. After 2 minutes, a node still without peers or
events shows that as a condition.

| Condition | What to do |
|-----------|------------|
| *No peer is configured* | Add the peers of your mesh to `mesh.bootstrap` and restart `obied` ([federation](federation.md)). |
| *No peer is connected* | Check that the peers run and that their mesh port is reachable from this host. The [peers view](#the-peers-view) shows why the last dial of each bootstrap peer failed; the `obied` log names every failed dial. |
| *No event received* | The node holds no verdict and has accepted none since `obied` started. Connect it to peers that publish verdicts, or report attacks yourself, for example with the [Fail2Ban action](../guides/fail2ban.md). |
| *Enforce mode, but nothing is applied to the firewall* | `enforce.backend` is `dryrun`, which only logs blocks. Set it to `nftables` and restart `obied` ([nftables](../guides/nftables.md)). |
| *Enforce mode, but nothing is applied* or *Decided blocks and applied entries may differ*: *enforcement failed* | The backend refuses the changes; the `obied` log says why (for nftables: may `obied` change the firewall?). `obied` retries on its own. |
| *The firewall may still apply blocks of an earlier enforce run* | In observe mode, withdrawing the entries failed; the `obied` log says why. `obied` retries on its own. |
| *… decided blocks are not applied: the allow-list refuses them* | `sudo obiectl explain <address>` names the allow-list entry that protects the address. If it should be blocked, remove it from the allow-list and reload. |
| *… decided blocks are not applied: the firewall holds at most … entries* | The lowest-score blocks are left out over `enforce.max_entries`. Raise it and restart `obied`, if the host can hold more entries. |
| *… is not ready* or *… is degraded*, naming a part | The `obied` log says why; `sudo obiectl status` shows every part. |
| *The configuration reload at … was rejected* | The node keeps the configuration it had. Fix the file, check it with `sudo obied --config /etc/obie/obie.yaml --check-config`, then reload again. |
| *Changes to … wait for a restart* (note) | The configuration file changes settings that only a restart applies; restart `obied` when it suits you. |

## The activity timeline

*Activity* answers "What is my node doing right now, and what did it do
when?". It lists what happened, newest first:

| Entry | When |
|-------|------|
| *Block added*, *Block updated*, *Block removed* | The node decided to block an address or network, changed a block's expiry, score or rule, or stopped blocking it. |
| *Spared by the allow-list* (or *by an always-allow override*) | An address with verdicts is not blocked because the allow-list or a force-allow protects it. |
| *Always allow set*, *Always block set*, *… removed* | You set or removed an override (`obiectl allow`, `block`, `unoverride`, or [from the console](#act-from-the-console)). |
| *Reported by this node* | This node issued a verdict (`obiectl report`, Fail2Ban, or from the console). |
| *Verdict revoked* | This node revoked one of its verdicts (`obiectl revoke`, or from the console). |
| *Peer connected*, *Peer disconnected* | The mesh connected to a peer, or lost its last connection to it. |
| *Configuration reloaded* | A reload took effect, naming the settings it changed and those that wait for a restart. |
| *Mode changed to Enforce* or *Observe* | A reload switched `node.mode`. |

Each entry shows when it happened (UTC), what happened, what it is about
and the reason the node recorded, with a few facts — who set an override,
reported or revoked, and through which door (*By alice (uid 1000) in the
console*, or *with obiectl or another client of the admin socket*), until
when a block or override lasts, your note, what triggered a decision, and
that a block decided in observe mode was not applied. It links to what it is about: an
address to [its explanation](#why-an-address-is-or-is-not-blocked), a peer
to [its page](#the-peers-view), a reload to the
[configuration view](#the-configuration-view), a mode change to the
`node` settings there; an override also to the
[overrides](#the-overrides-view) on the address, a report or revocation to
this node's [verdicts](#the-verdicts-view) on it.

**Filters.** *Kind* shows one kind of entry (blocks, the allow-list,
overrides, reports, revocations, peers, reloads or mode changes);
*Address or network* shows the entries about any address or network
that overlaps it. The filters are in the link, so *Copy link* shares them.

**Live.** The first page follows the node: new entries appear on top
within about a second, without reloading, while the page is visible.
*Pause live updates* stops them, for example to read an entry in peace;
*Resume live updates* shows everything that happened meanwhile. During a
burst — an attack wave that changes thousands of decisions a minute —
the page lists at most 50 new entries a second and sums up the rest in a
row that says how many of which kind arrived between which times, and
keeps at most 500 entries, saying so when it took older ones off. Reload
the page to page through all of them.

**The same story as your SIEM.** Every entry is a record of the node's
[audit log](monitoring.md#audit-log) (`audit.path`), and the timeline
reads that file: what the console shows is what your SIEM ingests, also
from before `obied` last restarted. *Older entries* pages back through
the file, 100 entries at a time; with a filter, one page searches up to
16 MiB of the file and offers to *Search further back*. The timeline reads
only the current file: after logrotate moved the log away, older entries
are in the rotated files and your SIEM. [ADR 0025](../adr/0025-console-activity-timeline.md)
records how the timeline and the audit log fit together.

**Without the audit log** (`audit.path` empty), the node keeps the last
10,000 entries in memory: the timeline shows them and follows the node
live as usual, and says what it cannot show — what happened before
`obied` started, beyond the last 10,000 entries, and after the next
restart — and that no SIEM receives them. Set `audit.path` and restart
`obied` to keep the history. If `obied` may write the audit log but not
read it back, or `audit.path` is no regular file (such as `/dev/stdout`),
the timeline says so and shows the entries in memory too.

## The peers view

*Peers* answers "Who is influencing my decisions, and is any peer broken
or misbehaving?". It lists every peer the node knows: the bootstrap peers
in `mesh.bootstrap`, the trusted publishers in `trust.publishers`, and
every peer that is connected, including those that connected on their
own — never the node itself. How the view reads its data is recorded in
[ADR 0021](../adr/0021-console-peers.md). For each peer:

- **Peer.** Its name from `trust.publishers` (or *Unnamed peer*), its
  shortened peer ID (the full ID is on its page), how it is configured —
  *Bootstrap peer*, *Trusted publisher*, both, or *Not configured* — and
  its addresses: those of the open connections, or its `mesh.bootstrap`
  addresses while it is not connected.
- **Connection.** *Connected since* when, or *Disconnected* with when it
  was *last seen*. A configured peer that has not been connected since
  `obied` started says so; for a bootstrap peer, the view also says when
  the last dial failed, and the peer's page says why. The node dials only
  bootstrap peers: a trusted publisher that is not in `mesh.bootstrap`
  connects only if it dials this node.
- **[Trust weight](../glossary.md#trust-weight).** The weight its
  verdicts carry in decisions: its
  `trust.publishers` weight, or the *default weight*
  (`trust.default_weight`, 0 unless set) for a peer that is not listed.
  A weight of 0 is marked *No influence on decisions*: the node holds the
  peer's verdicts but never counts them.
- **Verdicts held.** How many active verdicts of the peer the node holds,
  and how many of them count in decisions — ban verdicts of a peer whose
  weight is above 0.
- **Events, last hour.** How many events the peer sent this node in the
  last hour were accepted, and how many were rejected and why: invalid
  signature, not a valid obie/0.1 event, too large, expired or dated in
  the future, or over the rate limit (`mesh.rate_limit`). Events count for
  the peer that sent them: a publisher's verdicts that other peers relay
  count for those peers, and show up as its verdicts held. A peer that
  keeps sending rejected events is misbehaving or broken — its clock, for
  expired events; events with an invalid signature or format also lower
  its standing in the mesh (peer scoring).
- **Gossip score.** The peer's GossipSub score, read every 10 seconds: it
  rises with time in this node's mesh and with the first deliveries of
  valid events, and falls with invalid messages. A badge names the
  lowest score limit it is below: *Below the gossip limit* (-50: no
  gossip with it), *Below the publish limit* (-100: it gets none of this
  node's events) or *Graylisted* (-200: its messages are ignored). *Not
  scored* means no score was read for the peer yet — the first reading
  comes up to 10 seconds after it connects — or it left more than an
  hour ago. The score is about how the peer behaves on the wire, not
  whether its verdicts are right; trust stays the weight you configure
  ([ADR 0032](../adr/0032-gossip-instrumentation-and-attribution.md)).

The filters above the list show *All*, *Connected*, *Disconnected* or
*Untrusted* peers (weight 0), each with how many peers it lists; a
column heading sorts by that column (peer name, connection, trust weight
most first, verdicts most first, rejected events most first, gossip
score lowest first). The list shows 50 peers per page. Filter, order and
page are part of the address, so a view can be bookmarked. Verdicts from
publishers that are neither configured nor connected are summed up under
the list, with the weight they carry.

**A peer's page** (choose its name) shows the same in full — the peer ID,
the round-trip time, the error of the last failed dial, the rejection
reasons, the duplicates, and the gossip score with when it was read, the
score limits it is below and its components: time in this node's mesh,
first deliveries, invalid messages, behavior penalty, IP colocation
factor and application score — and the verdicts the node holds from the
peer, 50 at a time and by address: action, confidence, reason and
protocol, expiry, and whether each counts in decisions. The verdicts are
read when the page opens; reload it to read them again. An address leads
to every verdict on it, and *All its verdicts in the verdict view* to the
peer's verdicts in the [verdicts view](#the-verdicts-view), also the
revoked and expired ones.

Trust is configured, not set in the console: change `trust.publishers` or
`trust.default_weight` in `/etc/obie/obie.yaml` and reload `obied`; the
view follows. Adding a bootstrap peer needs a restart.

## The decisions view

*Decisions* answers "What does my node block, and what not?". It lists
every address and network the node holds a decision on — every one with
an active verdict, and the operator's force-blocks — 50 per page, the
last decided first. How the view reads them fast, even with 1,000,000
decisions, is recorded in
[ADR 0022](../adr/0022-console-decisions-and-firewall.md) and measured in
[performance](performance.md#console). For each decision:

- **Address.** The IPv4 or IPv6 address or network; choose it for the
  [explanation](#why-an-address-is-or-is-not-blocked).
- **State.** *Block*, *Allowed* or *None*, and what decided it:
  *consensus*, *local autoblock* (this node's own verdict), *below
  consensus*, the *allow-list*, a *protected address* (built-in, this
  node's own or a bootstrap peer's), or the operator's *force-block* or
  *force-allow*.
- **Score** against the [threshold](../glossary.md#threshold)
  (`decision.threshold`) and **Publishers** (those whose verdict counts)
  against the [quorum](../glossary.md#quorum) (`decision.quorum`), each
  with whether it is reached.
- **Reason.** What the verdicts are about — the evidence reason and the
  attacked protocol, for example *password_bruteforce (ssh)* — the most
  counting first, and how many active verdicts there are.
- **Decided** and **Expires**: when the node last evaluated it and when a
  block ends.
- **Firewall.** *Applied* (by its own entry, or through the entry of a
  wider network that contains it), or not and why: *observe mode*
  (nothing is applied, by design), *Refused* by the allow-list right
  before apply, *Left out* over `enforce.max_entries`, *Waiting* until an
  entry it overlaps expires, or *Not applied yet* (decided after the last
  pass; the next one applies it). A decision that is not a block but lies
  inside a blocked network says *Applied*: that network's entry wins.

The tabs above the list show *All*, *Block*, *Allowed* or *None*, each with
how many decisions match the other filters. The form filters by
**reason**, by **publisher** (the peers the node holds verdicts of, this
node included) and by **firewall** (*Applied by the firewall* or *Not
applied*), and searches by **address or network**: an address finds its
own decision and those of the networks that contain it, a network finds
everything inside it and around it. The address is also explained in one
line above the list — whether it is blocked and whether it is protected —
even when the node knows nothing about it. A column heading sorts by that
column; *First*, *Previous*, *Next* and *Last page* move through the list.
Search, filters, order and page are part of the address, so a view can be
[shared](#copy-and-share).

The list is read when the page opens and does not move under you. The
line above it says when it was read and, checked every 5 seconds, when
the decisions or the firewall changed since, with a link that reads the
same view again.

## Why an address is or is not blocked

Choose an address in any view, or open `/decisions/<address or network>`
— for example `/decisions/203.0.113.7`, `/decisions/2001:db8::1` or
`/decisions/198.51.100.0/24` — for the full explanation. It shows the same
facts as `sudo obiectl explain <address>`, evaluated again from the
verdicts, the overrides and the allow-list every 5 seconds, so a decision
that changes while the page is open shows the change:

- **Verdicts and score.** Every active verdict, one per publisher: the
  publisher (this node, a named peer or an unnamed one, linking to its
  peer page), ban or watch, its trust weight and where the weight comes
  from (`trust.publishers`, `trust.default_weight` or
  `trust.local_weight`), its confidence, what it adds to the score
  (weight × confidence), whether it counts and if not why (a watch
  verdict, weight 0), its reason and when it expires. Below: the score
  against the threshold and the counting publishers against the quorum,
  each *reached* or *not reached*, whether local autoblock decides, and
  the decision engine's summary.
- **Allow-list and overrides.** The rule that decides, if any, and which
  one: a protected address (never blocked, not even by a force-block), an
  allow-list entry from `allowlist.cidrs` or `allowlist.files`, or the
  operator's force-allow or force-block — with the matching range or
  override, its label or note, and when an override ends. *Protection*
  says whether the address would be protected.
- **Firewall.** Whether the firewall applies it — by its own entry or the
  entry of a wider network, until when — and if not, why: observe mode,
  the allow-list, `enforce.max_entries`, an overlapping entry about to
  expire, or not applied yet. The last enforcement pass and its mode.
- **Networks around it.** The decisions the node holds on networks that
  contain the address, with their state and firewall; the network whose
  entry applies the address is marked *Its entry wins*. A network's page
  links to the decisions inside it.

An address the node knows nothing about reads *No verdicts, not blocked*
and says whether it would be protected. A range the node cannot decide on
— not an address, or a network wider than /16 (IPv4) or /32 (IPv6) — is
not found.

## The firewall view

*Firewall* answers "Does the firewall do what the decisions say?". The
summary at the top, refreshed every 5 seconds, shows the mode, the
backend (`enforce.backend`), the entry limit (`enforce.max_entries`), the
last enforcement pass, the entries the backend holds after it, and how
the decided blocks came to them: *Covered* by another block's entry (the
same range or a wider one), *Refused* by the allow-list, *Left out* over
the entry limit, *Waiting* for an overlapping entry to expire. A failing
backend is shown with its error and the next attempt.

Read when the page opens:

- **Differences.** Every difference between the decided blocks and what
  the backend applies right now: decided blocks that no entry holds, and
  why — including blocks the backend lost since the last pass applied
  them, for example to a change by hand, which the next pass adds again
  (the first 20, with a link to the blocks not applied in the decisions
  list); entries
  without a decided block on their range (left behind, added by hand, or
  no longer a block — the next pass removes them); and entries that
  expire at another time than decided (the next pass replaces them).
  *No difference* when there is none.
- **Applied entries.** Exactly what the backend lists right now, 50 per
  page: the range, when the entry expires, and the decision on that range,
  marked *Differs* where it does not match.

In observe mode the view says so: the firewall applies nothing, by
design, and the decided blocks are not compared with it.

## The verdicts view

*Verdicts* answers "What has my node told the mesh, and what has the mesh
told my node?". How the view and the node read the verdicts is recorded
in [ADR 0023](../adr/0023-console-verdicts.md).

- **Totals.** How many verdicts this node published that are active,
  revoked and expired, and the same for every other publisher — the 20
  with the most active verdicts one by one, with their trust weight, and
  every other publisher summed up. Each number opens the list of those
  verdicts.
- **Whose.** The tabs show the verdicts of *All publishers*, of *This
  node* or those *Received* from every other publisher; the *Publisher*
  filter shows one publisher's, with its trust weight and a link to its
  peer page.
- **State.** The tabs show the *Active* verdicts (the default), the
  *Revoked* ones or the *Expired* ones, each with how many match the
  other filters. Revoked and expired verdicts count in no decision; they
  are shown only under their tab, marked as such (an expired verdict is
  greyed out), and the node keeps them for `store.ended_retention` (30
  days by default) after the verdict's expiry, then forgets them — of
  other publishers at most a tenth of `store.max_indicators` in each
  state, so that a flood cannot fill the disk; the tab says when that
  many are kept.
- **Filters.** By **address or network** — the verdicts on exactly that
  address or network, from every publisher, as
  `sudo obiectl show <address>` lists the active ones, with a link to its
  decision; the explanation covers the networks around it — by
  **reason** and by **publisher**.

For each verdict the list shows the address (choose it for every verdict
on it), the publisher — *This node*, a named peer or an unnamed one,
linking to its peer page — with its trust weight, marked *No weight* if
it is 0: the node holds such a publisher's verdicts but never counts
them. Then the action (ban or watch) and confidence; the reason and the
attacked protocol; the evidence: how many events are behind the verdict
and the hash of the log lines — the lines themselves never left the node
that reported them — and the verdict's event ID; when it was issued and
when it expires; its state: whether an active verdict counts in its
decision (and if not, why: a watch verdict, weight 0), when and why a
revoked one was revoked and the revocation's event ID, when an expired
one expired; and the state of the decision on its address, linking to
the [explanation](#why-an-address-is-or-is-not-blocked).

The list shows 50 verdicts per page, ordered by address and publisher like
`obiectl indicators`, with *First page* and *Next page*. Everything is
read when the page opens, on the node, fast with 1,000,000 held
indicators (see [performance](performance.md#console)); reload it to read
again. The explanation of an address links to its verdicts too.
*Report an address…* publishes a verdict of this node, and *Revoke…* on
one of this node's active verdicts withdraws it: see [Act from the
console](#act-from-the-console).

## The overrides view

*Overrides* answers "Which rules did I set on addresses myself?". It
lists every override in effect — *Always allow* (`sudo obiectl allow`, a
force-allow) or *Always block* (`sudo obiectl block`, a force-block) —
like `obiectl overrides`. How the view and the store keep them is recorded
in [ADR 0024](../adr/0024-console-overrides-allowlist-configuration.md).
For each override:

- **Address.** The address or network it is set on; choose it for the
  [explanation](#why-an-address-is-or-is-not-blocked) of its decision.
- **Rule.** *Always allow* or *Always block*. An always-block override
  that does nothing says why: an always-allow override on an overlapping
  address or network wins over it, or it covers a protected address — a
  built-in range, this node's own address or a bootstrap peer's — which
  not even an override blocks.
- **Note** you gave it, when it was **set**, and when it **ends**:
  *Never* for an override without `--ttl`.

The newest override comes first. The form narrows the list to *Always
allow* or *Always block* and to an **address or network**: every override
on it, inside it or around it. The tab *Expired* shows the overrides that
reached their end in the last 7 days, the most recently ended first,
with when each ended: they do nothing any more, and the node forgets them
after those 7 days. An override you removed with
`sudo obiectl unoverride` is not kept; the [audit log](monitoring.md#audit-log)
records it. The list shows up to 1,000 overrides and says how many more
match; the filters narrow it.

The list is read when the page opens; reload it to read it again.
*Always allow an address…*, *Always block an address…* and *Remove…* on
each override set and remove overrides after a confirmation: see [Act
from the console](#act-from-the-console).

## The allow-list view

*Allow-list* answers "Which addresses does my node never block, and
why?". It lists every entry of the allow-list the node runs with, grouped
by where it comes from, in the order they take precedence:

- **Built-in ranges**, by class: unspecified, loopback, private
  (RFC 1918), shared address space (CGNAT), link-local, unique local,
  multicast, limited broadcast, IPv4-mapped and documentation ranges.
- **This node's addresses**: those in `mesh.listen`, or for an
  unspecified listen address (`0.0.0.0`, `::`) those of every network
  interface.
- **Bootstrap peers**: the addresses of the peers in `mesh.bootstrap`, DNS
  names resolved.
- **Configured networks**: the entries of `allowlist.cidrs`.
- **One group per allow-list file** in `allowlist.files`, each entry with
  its line number.

The first three groups are marked *Protected*: not even an always-block
override blocks them. Your own entries (`allowlist.cidrs` and the files)
are overruled by an always-block override. A group lists up to 1,000
entries; the lookup finds every one.

**Allow-list files.** Each file says how many entries were loaded from it
and when — with the running configuration, at start or by the last
successful reload — and how it looks now, read again when the page opens:

- *Unchanged since it was loaded.*
- *It changed since it was loaded*, and how many entries it holds now:
  the new entries are not active until a reload applies them.
- A **warning** when the file is missing or cannot be read, or now holds
  lines the node rejects, listed with their line number and why (the first
  20; `obied --check-config` names the first). The entries loaded stay in
  effect, but the next reload is rejected, and `obied` would not start,
  until the file is fixed.

**Not protected.** A warning at the top names the addresses the
allow-list should hold but could not determine when it was loaded: a
bootstrap peer whose DNS name did not resolve (its addresses are protected
after a reload resolves it), or the interface addresses for an
unspecified listen address that could not be listed (list this node's
public addresses in `allowlist.cidrs`).

**Is this address protected?** Type an address or network — any address,
also a private one — and choose *Look up*. The answer names the rule that
decides, as the decision engine judges it with the running allow-list and
your overrides:

| Answer | Rule |
|--------|------|
| *Yes: … is protected by …* | A built-in range, this node's address or a bootstrap peer's address covers it. It is never blocked, not even by an always-block override. |
| *Yes: the allow-list entry … covers …* | An entry of `allowlist.cidrs` or an allow-list file covers it. Only an always-block override on it, or on a network around it, would overrule the entry. |
| *Yes, by your always-allow override on …* | Your always-allow override covers it, until it ends. |
| *No: your always-block override … blocks …* | Your always-block override on it, or on a network around it, which also overrules your own allow-list entries that cover it. |
| *No: no allow-list entry or override covers …* | Nothing protects it; the verdicts decide whether it is blocked. |

The answer shows the matching entry or override with its label or note
and when an override ends, lists the other entries that overlap the
address (up to 1,000), and links to the address's decision and
[explanation](#why-an-address-is-or-is-not-blocked). A network that
contains an allow-list entry is never blocked either: blocking it would
block the entry too. The lookup is part of the address
(`/allowlist?address=203.0.113.7`), so it can be
[shared](#copy-and-share).

The allow-list is changed in the configuration file and the allow-list
files, not in the console: edit them and reload `obied`. See the
[configuration reference](configuration.md#allowlist), and
[Override the mesh](operations.md#override-the-mesh) for overrides.

## The configuration view

*Configuration* answers "Which configuration does my node actually run
with?". The first section says how it was loaded:

- **File.** The configuration file `obied` read (`--config`).
- **Running since.** When the running configuration was loaded: at start,
  or by the last successful reload.
- **Defaults.** How many settings the file leaves to their defaults.
- **Last reload.** Whether the last reload succeeded, or was rejected —
  with the error, and a clear *The configuration loaded at … is still
  active*. Fix the file, check it with
  `sudo obied --config /etc/obie/obie.yaml --check-config`, then reload
  again.
- **File on disk.** The file is read and checked again when the page
  opens, and compared with the running configuration setting by setting:
  it *matches*; or it *changed since it was loaded*, naming the settings
  that are **not active until a reload** and those **waiting for a
  restart** (after a reload, only the latter remain); or it cannot be
  loaded now — missing, unreadable or invalid, or an allow-list file it
  names is missing, unreadable or holds a line the node rejects — with
  the error: a reload would be rejected, and the running configuration
  kept. A change only to comments or layout changes nothing.

Then **every setting**, grouped by the section of the file it is in
(`node`, `admin`, `mesh`, `store`, `trust`, `decision`, `allowlist`,
`enforce`, `metrics`, `console`, `audit`, `log`), in the order of the
[configuration reference](configuration.md):

- **Setting.** Its key, e.g. `mesh.rate_limit.peer.burst`, and what it
  does in one line.
- **Value.** The value it runs with, spelled as in the file — a list with
  one entry per line — marked *Default* when the file does not set it. A
  setting whose value on disk differs shows that too, with *Not active
  until a reload* or *Waits for a restart*.
- **A change applies on** *Reload* (`sudo systemctl reload obied` applies
  it at once) or *Restart* (only a restart of `obied` applies it), as the
  configuration reference marks it.

Secrets are never shown: a setting that holds one would show only
whether it is set. No setting holds a secret today; the node's private
key and the console token are not part of the configuration and appear
nowhere in the console.

The page is read when it opens; reload it to follow a reload of `obied`.
The configuration is changed in the file, not in the console.

## Act from the console

Fixing a false positive takes seconds: open the address's
[explanation](#why-an-address-is-or-is-not-blocked) and choose what to do.
The console offers exactly what `obiectl` offers, under the same rules:

| Action | Where | Like |
|--------|-------|------|
| *Always allow…* — never block the address or network here | explanation, overrides view | `sudo obiectl allow [--ttl] [--note]` |
| *Always block…* — block it whatever its score | explanation, overrides view | `sudo obiectl block [--ttl] [--note]` |
| *Remove the override…* | explanation, each override in the overrides view | `sudo obiectl unoverride` |
| *Report…* — publish a signed verdict of this node | explanation, verdicts view | `sudo obiectl report` |
| *Revoke my verdict…* — withdraw this node's active verdict | explanation, this node's verdicts in the verdicts view | `sudo obiectl revoke` |

Every action takes two steps and changes nothing before the second:

1. **Enter the details** — for an override, when it ends (`90m`, `36h`,
   `7d`, or nothing for never) and a note; for a report, the protocol, the
   reason, the number of events, and optionally the confidence (0.8 by
   default), the lifetime (`decision.default_ttl` by default) and *watch*
   instead of *ban*; for a revocation, the reason (`false_positive` by
   default). Choose *Review*.
2. **Confirm** — the node checks the action as it checks `obiectl`'s and
   says in plain words what it will do: the decision now and after
   ("203.0.113.7 will be unblocked on this node only"), whether only this
   node is affected or a signed event goes to the mesh ("This will publish
   a signed ban verdict … to the 3 peers connected now"), what observe
   mode means for the firewall, which override it replaces, and whether an
   always-block would take effect at all. Choose the button to carry it
   out, *Change* to go back, or *Cancel*.

The browser then returns to the page you came from, which shows what was
done and the new state: the explanation, the lists and the overview read
it at once.

- **Refusals are explained.** What `obiectl` refuses, the console refuses
  with the same words — a report on an allow-listed or internal address,
  an address that is none, a note longer than 1,024 bytes, removing an
  override that is not there, revoking a verdict this node does not hold
  — and nothing changes.
- **No peer reachable.** A report or a revocation while the node has no
  peer is stored and counts on this node at once; the node sends it as
  soon as a peer is reachable, and the console says so. It waits in
  memory: if `obied` restarts before, it is not sent (the verdict still
  counts on this node). Once a peer is reachable, the node sends what
  waited in order, 16 events every 2 seconds; a report made meanwhile
  waits behind them, and the console says how many wait and roughly how
  long they take.
- **While the node starts or stops**, the console already or still serves
  but the mesh does not run: a report or revocation is refused with that
  reason until the node is ready; overrides work throughout.
- **Two tabs, or obiectl at the same time.** If the address's override or
  this node's verdict on it changed after you opened the confirmation, the
  console carries nothing out, shows the confirmation again with the state
  of now and asks you to confirm once more.
- **Your session ended** (after 12 hours, a restart of `obied` or
  `obiectl console --rotate`): nothing is carried out; sign in again and
  the confirmation opens again, to confirm with the state of then. The
  node keeps the action for 15 minutes (not across a restart); after that,
  enter it again.
- **The audit log records who acted, and through which door.** Every
  action is recorded like `obiectl`'s, with `obie.origin: console` (or
  `admin-api` for `obiectl`) and your local user in `user.id` and
  `user.name`; the [activity timeline](#the-activity-timeline) shows
  *By alice (uid 1000) in the console*. See
  [Monitoring](monitoring.md#audit-log).

### Keep the console read-only

The actions add no right: whoever may sign in may run `obiectl` too. To
run the console strictly read-only all the same, set `console.actions` to
`false` and reload:

```yaml
console:
  enabled: true
  actions: false
```

The views then offer no action, say how to act with `obiectl`, and the
action pages are refused. How the actions are designed and the threats
they were weighed against are in
[ADR 0026](../adr/0026-console-operator-actions.md).

## Copy and share

Next to every address the console shows a *Copy* button; *Copy link* at
the bottom of every page copies the link to the view you see, with its
search, filters, order and page. The link opens the same view for anyone
who may use the console on the same host: they sign in first if needed,
and the console then returns them to it. The link works only on this host
(or through the same SSH port forward); it holds nothing secret. Where the
browser does not allow copying, the console selects the text for you to
copy with Ctrl+C. Without JavaScript, addresses are plain text and the
browser's address bar holds the link.

## Reach it from another machine

The console never listens on the network. From your workstation, forward
its port over SSH and open the same address there:

```sh
ssh -L 9465:127.0.0.1:9465 you@node.example.org
```

Then open `http://127.0.0.1:9465/` on the workstation (for a
`console.listen` other than `127.0.0.1`, use its address after the first
colon, and still open `127.0.0.1` on the workstation). On the node, the
connection comes from your SSH login user, which must be root or in the
group `obie`. The forwarded port is open to every user of your
workstation: they still need the token, but use a workstation you
control, and sign out when you are done. To forward to another local
port, use `ssh -L 10000:127.0.0.1:9465 …` and open
`http://127.0.0.1:10000/`.

## Replace the token

If the token may have leaked, or you lost track of who has it, issue a new
one:

```sh
sudo obiectl console --rotate
```

The old token stops working at once and every browser is signed out. The
token never touches the disk: every restart of `obied` replaces it too, and
signs every browser out.

## While a browser is open

The open page refreshes the health indicator every 5 seconds while it is
visible, and the overview, the peers view, an explanation and the
firewall view's summary refresh their content with it; *Updated* says
when its data was read, and a focused link or button stays focused. The
activity timeline asks for new entries every second while it is visible
and not paused. The
decisions list only says when the decisions changed; the verdicts,
overrides, allow-list and configuration views are read when they open.
Without
JavaScript the page is still complete: reload it for current data. When a
reload changes the mode or a subsystem's health, the page follows. When the console is switched off or moved to another address, or
`obied` stops, the page says it cannot reach the console and keeps trying;
run `sudo obiectl console` for the current address. When the token was
replaced or `obied` restarted, the page asks you to sign in again.

## When it does not work

`sudo obiectl console` and `sudo obiectl status` show the console's state.
It never stops the node: if it cannot start, the node runs without it and
logs why.

| You see | Cause and fix |
|---------|---------------|
| `Console: disabled (console.enabled is false)` | Set `console.enabled: true` and reload. |
| `Console: not serving: listen tcp 127.0.0.1:9465: bind: address already in use` | Another program uses the port. Choose another `console.listen` port, or stop the other program, and reload. The log line is `console not started; the node runs without it`. |
| `Console: serving at http://127.0.0.1:9465/, not at 127.0.0.1:9470: …` | A reload tried to move the console to a taken port; it keeps serving at the old address. Free the port or choose another, and reload. |
| `refused: this connection comes from the local user …` | Your user is neither root nor in the group `obie`. Add it (`sudo usermod -aG obie <user>`), or forward the port as a user who is. |
| `refused: this console answers only requests addressed to 127.0.0.1, [::1] or localhost` | You opened the console under another name, for example through a proxy or a DNS name pointing at the host. Use `127.0.0.1` or `localhost`, through an SSH port forward from other machines. |
| `refused: the request came from another web site or another port of this host` | A page on another site or another local port tried to use the console. Open the console directly in the address bar. |
| `refused: a state-changing request must come from a console page` | A program other than a browser posted to the console. Sign in with a browser. |
| *This is not the console's current token* | The token was replaced or `obied` restarted. Get the current one with `sudo obiectl console`. |
| *Too many sign-in attempts* | More than a few attempts in a row; wait a few seconds. |
| *Actions are switched off on this node (console.actions: false)* | The console is read-only. Act with `obiectl`, or set `console.actions: true` and reload. |
| *Nothing was changed. The state of … changed since this confirmation was shown* | Another tab, `obiectl` or the mesh changed the address meanwhile. Check what your action does now and confirm again. |
| *Your session ended before the action was carried out* | Sign in again; the confirmation opens again. |
| `refused: an action page opens only from the console's own pages` | A link on another site or another local port led to an action page. Open the console in the address bar and choose the action there. |
