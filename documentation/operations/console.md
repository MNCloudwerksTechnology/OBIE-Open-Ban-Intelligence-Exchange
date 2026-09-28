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
within seconds whether the node is healthy and what it is doing. The
views of the node's peers, decisions, verdicts and configuration arrive
with later releases; a view appears in the navigation once it exists.

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
  details it; until that view exists, it names the `obiectl` command that
  shows the same (`obiectl peers`, `indicators`,
  `decisions --state block`, `enforced`, `overrides`). *Firewall entries*
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
| *No peer is connected* | Check that the peers run and that their mesh port is reachable from this host; the `obied` log names the failed dials. `sudo obiectl peers` lists the connected peers. |
| *No event received* | The node holds no verdict and none arrived since `obied` started. Connect it to peers that publish verdicts, or report attacks yourself, for example with the [Fail2Ban action](../guides/fail2ban.md). |
| *Enforce mode, but nothing is applied to the firewall* | `enforce.backend` is `dryrun`, which only logs blocks. Set it to `nftables` and restart `obied` ([nftables](../guides/nftables.md)). |
| *Enforce mode, but nothing is applied* or *Decided blocks and applied entries may differ*: *enforcement failed* | The backend refuses the changes; the `obied` log says why (for nftables: may `obied` change the firewall?). `obied` retries on its own. |
| *The firewall may still apply blocks of an earlier enforce run* | In observe mode, withdrawing the entries failed; the `obied` log says why. `obied` retries on its own. |
| *… decided blocks are not applied: the allow-list refuses them* | `sudo obiectl explain <address>` names the allow-list entry that protects the address. If it should be blocked, remove it from the allow-list and reload. |
| *… decided blocks are not applied: the firewall holds at most … entries* | The lowest-score blocks are left out over `enforce.max_entries`. Raise it and restart `obied`, if the host can hold more entries. |
| *… is not ready* or *… is degraded*, naming a part | The `obied` log says why; `sudo obiectl status` shows every part. |
| *The configuration reload at … was rejected* | The node keeps the configuration it had. Fix the file, check it with `sudo obied --config /etc/obie/obie.yaml --check-config`, then reload again. |
| *Changes to … wait for a restart* (note) | The configuration file changes settings that only a restart applies; restart `obied` when it suits you. |

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
visible, and the overview refreshes its content with it; *Updated* says
when its data was read, and a focused link stays focused. Without
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
