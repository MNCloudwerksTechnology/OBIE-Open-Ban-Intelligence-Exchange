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
*Shutting down*. The views of the node's peers, decisions, verdicts and
configuration arrive with later releases; a view appears in the
navigation once it exists.

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
visible. When a reload changes the mode or a subsystem's health, the page
follows. When the console is switched off or moved to another address, or
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
