# Web console

The web console is a read-only window onto a running node, in your
browser. It is built into `obied`, loads nothing from outside the node and
works offline. It is **off by default** and, when on, reachable **only
from the node's own host**, only by the local users who may run `obiectl`,
and only with a token that `obied` keeps in memory. Its security model and
the threats it was designed against are in
[ADR 0019](../adr/0019-local-web-console.md).

Every page shows the node, its mode (observe or enforce) and its health:
*Starting*, *Ready*, *Degraded* (naming the subsystem and why) or
*Shutting down*. The first page, the [overview](#the-overview), tells you
within seconds whether the node is healthy and what it is doing; the
[peers view](#the-peers-view) shows every peer the node knows and the
trust placed in it; the [decisions view](#the-decisions-view) lists every
address the node decided on and [explains](#why-an-address-is-or-is-not-blocked)
why it is or is not blocked; the [firewall view](#the-firewall-view) shows
what the firewall applies; the [verdicts view](#the-verdicts-view) shows
what this node told the mesh and what the mesh told it. The view of the
node's overrides and configuration arrives with a later release; a view
appears in the navigation once it exists.

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
  `mesh.bootstrap` peers), held indicators with their active verdicts,
  decisions by state (`block`, `none`, `allowed`), the entries the firewall
  applies, and active overrides. Each number links to the view that
  details it — *Peers connected* to the [peers view](#the-peers-view),
  *Indicators held* to the [verdicts view](#the-verdicts-view), the
  decisions to the [decisions view](#the-decisions-view) in that
  state, *Firewall entries* to the [firewall view](#the-firewall-view);
  until a view exists, the number names the `obiectl` command that shows
  the same (`obiectl overrides`). *Firewall entries*
  also says why they differ from the decided blocks: blocks that share an
  entry with another block (the same range, or one inside a wider range),
  that the allow-list refuses, or that are over `enforce.max_entries`.
- **Parts of the node.** The readiness of the mesh, the store, the decision
  engine, enforcement, the admin interface and the other parts, with what
  each reports.
- **This node.** The mode and what it means, the peer ID, the key
  fingerprint, the version, the uptime and when the running configuration
  was loaded (at start or by a reload).

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
- **Trust weight.** The weight its verdicts carry in decisions: its
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

The filters above the list show *All*, *Connected*, *Disconnected* or
*Untrusted* peers (weight 0), each with how many peers it lists; a
column heading sorts by that column (peer name, connection, trust weight
most first, verdicts most first, rejected events most first). The list
shows 50 peers per page. Filter, order and page are part of the address,
so a view can be bookmarked. Verdicts from publishers that are neither
configured nor connected are summed up under the list, with the weight
they carry.

**A peer's page** (choose its name) shows the same in full — the peer ID,
the round-trip time, the error of the last failed dial, the rejection
reasons and the duplicates — and the verdicts the node holds from the
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
- **Score** against `decision.threshold` and **Publishers** (those whose
  verdict counts) against `decision.quorum`, each with whether it is
  reached.
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
  greyed out), and the node keeps them for 24 hours after the verdict's
  expiry, then forgets them — of other publishers at most a tenth of
  `store.max_indicators` in each state, so that a flood cannot fill the
  disk; the tab says when that many are kept.
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
Publishing and revoking verdicts stays with `obiectl report` and
`obiectl revoke`.

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
decisions list only says when the decisions changed; the verdicts view
is read when it opens. Without
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
