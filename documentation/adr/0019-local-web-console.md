# ADR 0019: Local web console — security model and technology

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1682](https://openproject.niew.dev/work_packages/1682)
  (epic [#1680](https://openproject.niew.dev/work_packages/1680))

## Context

Operators see their node only through `obiectl`, one command at a time.
Epic #1680 adds a web console that shows the whole node in one place; this
record fixes its foundation: how it is served, who can reach it, how a
browser proves it belongs to the operator, and how every later view plugs
in. Three constraints shape it:

- **No new door.** The node runs on internet-facing hosts. The console must
  not be reachable from the network, from other users of the host, or from
  web pages the operator happens to have open in the same browser.
- **No new dependency.** The console ships inside `obied`, works offline,
  and must not add a runtime, a build toolchain or a module to the node.
- **Never hurt the node.** A console that cannot start, or that is switched
  on and off, must not stop, restart or degrade anything else.

## Decision

### Technology

- **Server-rendered HTML from the Go standard library.** `internal/console`
  renders every page on the node with `html/template` (contextual
  escaping) and serves it with `net/http` through `internal/httpserver`,
  so the timeouts and header cap of ADR 0017 apply. One stylesheet, one
  small script and an icon are embedded in the binary with `embed`. No
  framework, no build step, no new module; pages are testable with
  `httptest` like the rest of the node.
- **Pages work without the script.** The script only refreshes the health
  indicator every 5 seconds while the page is visible and shows a banner
  when the session ended or the console became unreachable. Paging,
  filtering and searching in later views happen on the node, with
  ordinary links and forms.
- **Views plug into one layout.** A view is a path, a title and a handler
  in the console's view list. The navigation shows exactly that list, so a
  view that is not built does not appear. Every page behind sign-in shares
  the layout: header with the node, its mode and its health, navigation,
  content.

### Security model

1. **Off by default.** `console.enabled` is `false`. `console.enabled` and
   `console.listen` (default `127.0.0.1:9465`) are applied on reload
   (SIGHUP): the console starts, stops or moves, nothing else restarts.
2. **Loopback only.** `console.listen` must be a loopback IP address
   (`127.0.0.0/8` or `::1`) and a port. Any other value — a host name,
   `localhost`, an empty or unspecified host, an interface address — is a
   configuration error that explains the restriction and names the SSH
   port forward (`ssh -L 9465:127.0.0.1:9465 <host>`) as the way to reach
   the console from another machine. The console re-checks the address it
   bound and refuses to serve on anything that is not loopback.
3. **Only operators' processes are served.** Loopback TCP carries no
   credentials, so the console looks up the owner of the client socket in
   the kernel's socket table (`/proc/self/net/tcp` and `tcp6`, readable
   under the unit's `ProcSubset=pid`) and applies the admin API's policy
   (ADR 0012): root, the user `obied` runs as, and members of
   `admin.socket_group`. Everyone else gets 403 with an explanation before
   any page, including sign-in. A connection whose owner cannot be
   determined is refused — also one whose client already closed its end:
   the table then lists the socket without an owner (inode 0, UID 0),
   which must never pass for root. The decision is taken once per
   connection; at most two lookups run at once, and refusals are logged
   at most about once a second. The policy and both lookups
   (`SO_PEERCRED` for the admin socket, the socket table for the console) live in
   `internal/peercred`. On platforms without the socket table only the
   credential below protects the console, as only the file mode protects
   the admin socket there. Through an SSH port forward the connection is
   made by the operator's `sshd` session, so it is attributed to their
   login user.
4. **A credential only operators can obtain.** At start `obied` generates
   a 256-bit random token and keeps it in memory only: never on disk, in
   a log, in a page or in a URL. `obiectl console` (`GET /v1/console`)
   shows it and `obiectl console --rotate` (`POST /v1/console/token`)
   replaces it; both go through the admin socket and its `SO_PEERCRED`
   check. A restart replaces the token too.
5. **Sessions.** Signing in posts the token (rate-limited to 1 attempt
   per second, burst 5; compared in constant time; failures are logged)
   and sets the cookie `obie_console_<port>`: an expiry 12 hours ahead and
   an HMAC-SHA256 over it with a session key generated together with the
   token. It is `HttpOnly`, `SameSite=Strict` and `Path=/`, and not
   `Secure`, because the console speaks plain HTTP on loopback. Rotating
   the token replaces the session key, so every session ends at once.
   Pages without a valid session redirect to the sign-in page; the JSON
   endpoints answer 401. Signing out clears the cookie.
6. **Requests from other web pages are refused.**
   - *DNS rebinding:* the `Host` header must be a loopback IP literal or
     `localhost` (any port, so a forward to another local port works);
     anything else gets 421 before authentication.
   - *Cross-site and cross-port requests:* with Fetch Metadata, a request
     whose `Sec-Fetch-Site` is `cross-site` or `same-site` is refused
     (403) unless it is a top-level `GET` navigation, which cannot read
     the response. `same-site` is refused too, because pages on other
     ports of `127.0.0.1` are the same site. `POST` requests also need
     `Sec-Fetch-Site: same-origin` or, from browsers without Fetch
     Metadata, an `Origin` equal to the console's own; a `POST` with
     neither, or with `Origin: null`, is refused. `SameSite=Strict` keeps the cookie off cross-site requests.
   - *Framing, sniffing, leaks:* every response carries
     `Content-Security-Policy: default-src 'none'; script-src 'self';
     style-src 'self'; img-src 'self'; connect-src 'self'; form-action
     'self'; frame-ancestors 'none'; base-uri 'none'`, `X-Frame-Options:
     DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy:
     no-referrer`, `Cross-Origin-Opener-Policy: same-origin`,
     `Cross-Origin-Resource-Policy: same-origin` and `Cache-Control:
     no-store` (assets: `no-cache`). No CORS headers are ever sent.
7. **Offline.** Every byte comes from the binary: system fonts, embedded
   assets, and a CSP that allows nothing but the console's own origin. A
   test checks that no template or asset refers to another origin.
8. **Read-only.** The console changes nothing on the node; the only
   requests with an effect are sign-in and sign-out. Operator actions
   (#1689) will need their own confirmation and audit design. *Amended
   by [ADR 0026](0026-console-operator-actions.md): the console carries
   out the operator's actions after a confirmation, unless
   `console.actions` is `false`.*

### Lifecycle and health

- **The console never stops the node.** It is the subsystem `console`,
  registered first, so it starts before and stops after every other
  subsystem and can show the node starting and shutting down. Its `Start`
  never fails: an error such as a port in use is logged (`console not
  started; the node runs without it`) and shown in its status detail
  (`not serving: …`). Its readiness is always ready, so it never turns
  `/readyz` to 503. Because it runs longer than the others, its views must
  treat every other subsystem as possibly not running.
- **Moves.** A reload that changes `console.listen` starts the console at
  the new address before it leaves the old one: a move to a taken port
  keeps it serving where it was and says why. A reload with unchanged
  settings retries a console that could not listen or stopped serving.
- **Status detail:** `disabled`, `serving at http://127.0.0.1:9465/`,
  `serving at <old URL>, not at <console.listen>: <reason>` or
  `not serving: <reason>` in `obiectl status` and `obiectl console`.
- **Health on every page.** The header shows the node's health derived
  from the lifecycle statuses: *shutting down* when a subsystem is
  stopping or stopped, *starting* while one is pending or starting,
  *degraded* when a running subsystem is not ready or reports a detail
  starting with `degraded` (naming it), else *ready*; plus the mode.
  The sign-in page shows nothing about the node.
- **Reload with a browser open.** Within 5 seconds the open page follows
  the new mode and health, asks to sign in again (401 after a rotation or
  a restart), or says the console is no longer reachable — switched off,
  moved or stopping — and how to reconnect; it clears the banner when the
  console answers again.

### Threats considered

| Threat | Defence | Remaining risk |
|--------|---------|----------------|
| Attacker on the network connects to the console | Off by default; loopback-only address, validated and re-checked after binding | None while the host's loopback is not forwarded to the network by other software |
| Another user of the host connects directly | Socket-owner check (403 before any page), also for a client that closed its end; token required anyway | On platforms without the socket table only the token protects |
| Another user of the host floods the console with connections | One owner lookup per connection, at most two at once; refusal and sign-in warnings rate-limited | Reading the socket table costs obied some CPU per connection |
| Another user of the host obtains the token | Token only in `obied`'s memory and behind the admin socket's `SO_PEERCRED` policy | Root and members of `admin.socket_group` are operators by definition |
| Another user's web server on another loopback port receives the session cookie (cookies are not isolated by port) | The stolen cookie is useless from their processes: the socket-owner check refuses them | See the SSH forward row |
| Web page in the operator's browser sends a forged request (CSRF) | Fetch Metadata and `Origin` checks, `SameSite=Strict`, read-only console | Browsers without Fetch Metadata rely on `Origin` and `SameSite` |
| DNS rebinding | `Host` allow-list | — |
| Framing, cross-origin reads, XS-leaks | CSP `frame-ancestors`, `X-Frame-Options`, CORP, COOP, refusal of cross-site subresource requests, no CORS | — |
| Script injection (XSS) | `html/template` escaping, no inline script or style, strict CSP | — |
| Guessing the token | 256 bits, constant-time comparison, rate-limited sign-in, logged failures | — |
| Token or session leaked or lost | `obiectl console --rotate` ends every session; sessions end after 12 hours; a restart replaces the token | Until rotated, a leaked token works from an operator's account |
| Token leaks through a URL, history or `Referer` | Token only in a `POST` body; `Referrer-Policy: no-referrer` | — |
| Other users of the operator's workstation, through an SSH port forward | Token and session required | The forwarded port is open to every user of the workstation and arrives as the operator's login user; cookies on the workstation reach its other loopback servers. Forward from a workstation you control; sign out and rotate after use |
| Console overloaded or failing degrades the node | Own subsystem that never fails start or readiness; request timeouts and header cap; rate-limited sign-in; health polled only while a page is visible | — |
| Console loads code or data from outside | Embedded assets, CSP `'self'` only, test | — |

### Alternatives considered

- **A single-page application** (for example Angular, like the website):
  needs a JavaScript toolchain in the node's build and a large bundle,
  moves rendering of million-row views into the browser, and would be
  tested outside Go. Rejected.
- **A hand-written script client with a bearer token in `sessionStorage`**:
  isolates the credential per port and rules out CSRF by construction, but
  every view becomes client-side code without deep links or Go tests. The
  socket-owner check closes the cookie's port gap on the host instead.
- **HTTP Basic authentication**: scoped per port, but browsers cannot sign
  out, show a native prompt and still attach the credential to
  cross-site requests. Rejected.
- **TLS**: adds certificates and browser warnings for loopback traffic;
  remote use goes through SSH, which already encrypts. Rejected.
- **A Unix socket like the admin API**: browsers cannot connect to one.
- **A token persisted in the state directory**: survives restarts, but
  puts a second secret on disk and in backups for little gain. Rejected;
  the state directory format (ADR 0017) is unchanged.
- **Refusing a non-loopback `console.listen` at run time instead of in
  validation**: would let `--check-config` pass a setting the console then
  refuses. A configuration error is the clearest refusal; like every
  configuration error it is caught by `--check-config` and rejected on
  reload while the running configuration is kept.

## Consequences

- An operator enables the console with one key and a reload, runs
  `obiectl console` for the address and token, and signs in; from another
  machine they forward the port over SSH first. The operator's login user
  must be root or in `admin.socket_group` (usually `obie`), as for
  `obiectl` without `sudo`.
- Every restart signs every browser out; that is the price of never
  storing the token.
- Later views (#1683–#1688) add a view to the list, read the node through
  functions the daemon passes in, and must cope with subsystems that are
  not running yet or any more.
- `internal/peercred` is shared by the admin API and the console, so both
  admit exactly the same local users.
