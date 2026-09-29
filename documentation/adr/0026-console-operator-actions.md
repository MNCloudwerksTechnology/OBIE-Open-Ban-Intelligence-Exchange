# ADR 0026: Console operator actions — confirmation, same rules, audit origin

- **Status:** Accepted
- **Date:** 2026-09-29
- **Work package:** [#1689](https://openproject.niew.dev/work_packages/1689)
  (epic [#1680](https://openproject.niew.dev/work_packages/1680)); extends
  [ADR 0009](0009-gossip-of-events.md),
  [ADR 0012](0012-local-verdict-reporting.md),
  [ADR 0013](0013-local-sovereignty.md),
  [ADR 0015](0015-metrics-and-audit-log.md),
  [ADR 0019](0019-local-web-console.md) and
  [ADR 0025](0025-console-activity-timeline.md)

## Context

The console shows the operator what the node knows and why it decides as it
does (ADR 0019–0025), but fixing a false positive still means switching to
the terminal: `obiectl allow`, `block`, `unoverride`, `report` and `revoke`.
The operator wants to do the same from the page they are looking at, with
an explicit confirmation that says what will happen. ADR 0019 kept the
console read-only and left the actions for a design of their own. Five
things need a decision:

- **Where the rules live.** The admin API checks every request (ADR 0012,
  ADR 0013) and the daemon's services write the audit trail. A second set
  of checks in the console could drift from them.
- **How a confirmation can state the consequence** before anything
  changes: the decision after the change, whether only this node is
  affected or a signed event goes to the mesh, and whether a force-block
  takes effect at all.
- **Concurrency and sessions.** Two tabs may act on the same address; a
  session may end between opening a confirmation and confirming it.
- **Audit.** The audit log records overrides, reports and revocations, but
  not who made them or through which door.
- **Delivery.** An event published while no peer is on the topic is stored
  and counts on the node, but GossipSub sends it to nobody, and v0.1 has
  no anti-entropy: it is never delivered later (ADR 0009). The console
  must not promise what the node does not do.

## Decision

### The same rules, through the same services

- The console carries out an action through the `ActionSource` the daemon
  passes it, never through a path of its own. The daemon implements it with
  the admin API's request checks (`OverrideRequest.Check`,
  `ReportRequest.Check`, `RevocationRequest.Check` and `ParseIndicator`,
  now exported) and the very services behind the admin API: the store
  overrides and the audited verdict service. So an allow-listed or
  non-public address is refused, a missing field or a bad duration is
  explained, and an override without effect is warned about exactly as
  `obiectl` does it, in the same words.
- **Checks without effect.** So that the operator sees a refusal before
  confirming, the same checks run when the confirmation is built:
  `verdicts.Service.Check` runs every rule of `Report` short of signing and
  publishing, `store.CheckOverride` every rule of `SetOverride` short of
  writing. The action checks again when it is carried out.
- **The consequence is computed, not guessed.** `decision.Engine.ExplainWith`
  evaluates an address as `Explain` does, but with the overrides and
  this node's verdict the action would leave (a new or removed override, a
  new, refreshed or revoked own verdict). The confirmation states the
  decision now and after in plain language — "203.0.113.7 will be unblocked
  on this node only", "This will publish a signed ban verdict on
  203.0.113.7 to the 3 peers connected now", "The always-block will not
  take effect: 10.0.0.7 is a protected address" — with the mode (observe
  mode applies nothing to the firewall), the expiry and what is replaced.

### Pages and flow

- **`/actions/{allow,block,unoverride,report,revoke}`**, one page per
  action, reached from the decision view (every action on the address), the
  verdicts view (report; revoke on this node's active verdicts) and the
  overrides view (allow and block any address; remove on each override in
  effect). They are not in the navigation.
- **Two steps, no script needed.** `GET` shows the form (address, and the
  details: expiry and note; protocol, reason, events, confidence, lifetime
  and action; the revocation's reason, `false_positive` by default), then,
  with `review=1`, the confirmation: the consequence, the details, *Confirm*
  (a `POST` form), *Change* and *Cancel*. Removing an override and
  revoking open the confirmation at once. Input is checked on the node; an
  error shows the form again with the reason (400, 404 or 422 like the
  admin API). `GET` never changes anything.
- **`POST` carries it out** and answers `303` to the page the operator came
  from (a path on the console, else the address's decision) with an
  outcome: what was done, the decision now, a warning, whether the event
  was sent or held. The outcome is kept in memory under a random ID for 15
  minutes (at most 64 of them) and shown by the page it returns to; the URL
  carries only the ID, so a link cannot put words on a console page.
- **Views show the new state at once.** Every view reads the node when it
  opens. After the change, the action calls `decision.Engine.Flush`, which
  evaluates the changed addresses right away instead of on the worker, so
  the decisions and verdicts lists — read from the engine's memory — show
  the change on the page the operator returns to.

### Concurrency and sessions

- **No silent overwrite.** The confirmation carries a fingerprint of the
  address's state: its override (action, times, note) and this node's
  active verdict (event ID). The `POST` compares it with the current state
  before acting; if another tab, `obiectl` or the mesh changed it, nothing is
  carried out and the confirmation is shown again with the current state
  and a warning (409). Console actions run one at a time, so the check and
  the change are atomic among browser tabs; an `obiectl` command in the
  microseconds between them is the remaining race.
- **A session that ended changes nothing.** A `POST` without a valid session
  (expired, signed out, token rotated, obied restarted) is not carried out:
  the browser goes to the sign-in page, which says so, and returns to the
  confirmation afterwards to confirm again with the state of then. The
  action waits on the node under a random ID, kept like the outcomes (15
  minutes, at most 64, not across a restart), so neither a long note nor a
  link carries it through the sign-in; an ID no longer kept asks to enter
  the action again.
- **Cross-site requests** are refused as before (ADR 0019): a `POST` needs
  `Sec-Fetch-Site: same-origin` or a matching `Origin`, and the cookie is
  `SameSite=Strict`. In addition, an action page opened by a navigation from
  another site (`Sec-Fetch-Site: cross-site` or `same-site`) is refused —
  before the session check, since such a navigation carries no
  `SameSite=Strict` cookie and would otherwise be sent to sign in and on
  to the page — and the sign-in page reached that way forgets an action
  page as the page to return to, and does not say that an action waits.
  So a link elsewhere cannot present a prefilled confirmation.

### Switching them off

- **`console.actions`** (default `true`, applied on reload) switches the
  actions off: the views show no action controls, say that the console is
  read-only and name `obiectl`, and the action pages answer 403 with the
  reason. The console itself stays opt-in (`console.enabled`, default off);
  anyone it admits may run `obiectl` anyway, so the actions add no right,
  only a door, which an operator can keep shut.

### The audit trail says who and through which door

- An operator action — setting or removing an override, a report, a
  revocation — carries its **origin** in the request's context
  (`audit.WithOrigin`), and its audit record gets three fields:

  | Field | Value |
  |---|---|
  | `obie.origin` | `console` or `admin-api` (`obiectl` and every other client of the admin socket) |
  | `user.id` | the local user's UID: from `SO_PEERCRED` for the admin socket, from the socket table for the console |
  | `user.name` | that user's name, if the host knows it |

  Records of the decision engine, the mesh and the reloader have none. The
  console's activity timeline shows who acted and through which door.

### Delivery once a peer is reachable

- `gossip.Publish` stores the node's own event first, as before. If no peer
  is on the topic, it **holds** the event instead of publishing it to
  nobody, and publishes every held event, in order, as soon as a peer joins
  the topic; an event that has expired by then is dropped. At most 10,000
  events are held (the oldest is dropped, with a warning); they are held in
  memory only, so a restart before a peer is reachable does not send them —
  they still count on this node. `Mesh.Held` tells whether an event waits.
- The console's outcome says so: "No peer is reachable now. The verdict is
  stored and counts on this node; it will be sent as soon as a peer is
  reachable (unless obied restarts before)." `obiectl report` benefits
  alike.

### Threats considered

| Threat | Defence | Remaining risk |
|--------|---------|----------------|
| A web page in the operator's browser triggers an action (CSRF) | `POST` only; Fetch Metadata and `Origin` checks; `SameSite=Strict` cookie; the confirmation's fingerprint | Browsers without Fetch Metadata rely on `Origin` and `SameSite` |
| A link elsewhere opens a prefilled confirmation for the operator to click, directly or through the sign-in page | Action pages refuse cross-site and same-site navigations before the session check; the sign-in page reached so drops an action page as its next; a pending action is named by a random ID only; the confirmation states the consequence | A browser without Fetch Metadata shows the page; the operator still reads what it does |
| Framing the confirmation to trick a click (clickjacking) | `frame-ancestors 'none'`, `X-Frame-Options: DENY` | — |
| Words injected into a console page through a URL | Outcomes are kept on the node under a random ID; notes and input are escaped by `html/template` | — |
| A second tab or `obiectl` overwrites a change unseen | Fingerprint check under the console's action lock; 409 with the current state | An `obiectl` command between check and change |
| An action carried out after the session ended | Session checked before anything is read from the form; nothing is carried out | — |
| The console as a door someone would rather keep shut | `console.actions: false`; `console.enabled` stays off by default | — |
| An action that cannot be traced | Audit records with `obie.origin`, `user.id` and `user.name`, for the console and the admin socket | A shared account (root) names no person |
| A verdict reported while isolated never reaches the mesh | Held and published when a peer joins | Held in memory only; lost with a restart before then |

## Alternatives considered

- **The console as a client of the admin socket**: one code path for sure,
  but the console runs inside `obied`; talking to its own socket needs the
  socket's group and file mode inside the process and a JSON round trip per
  action. Calling the same checks and services in process gives the same
  rules without it.
- **A modal dialog in the script**: less navigation, but ADR 0019 keeps
  every page working without the script, and a confirmation page is
  keyboard- and screen-reader-friendly by construction.
- **A flash message in the redirect URL or a cookie**: a URL anyone can link
  to would put arbitrary words on a console page; a signed cookie is more
  machinery than an in-memory map of 64 outcomes.
- **A per-form CSRF token**: the Fetch Metadata and `Origin` checks and the
  `SameSite=Strict` cookie already refuse cross-site `POST`s (ADR 0019); the
  fingerprint adds the protection a token would add against stale forms.
- **Refusing a force-block that would not take effect**: `obiectl block`
  sets it and warns; the console does the same, and says so before.
- **Persisting held events**: they are this node's own active verdicts in
  the store already; re-announcing every own verdict to every peer that
  connects is anti-entropy, which v0.1 leaves out (ADR 0009). Holding what
  was never sent covers the isolated node without it.
- **Actions off by default**: the console is already off by default and
  admits only users who may run `obiectl`; a second switch that is off would
  make the feature invisible to most operators without adding a right.

## Consequences

- The console is no longer read-only by default; `console.actions: false`
  restores that. ADR 0019's "Read-only" point is replaced by this record.
- The admin package exports its request checks; `admin.Overrides.Set` and
  `Delete` take a context, which carries the origin. `verdicts.Service`
  gains `Check`, `store` gains `CheckOverride`, `decision.Engine` gains
  `ExplainWith` and `Flush`, `gossip.Gossip` and `mesh.Mesh` gain `Held` and
  `TopicPeers`.
- Audit records of operator actions gain `obie.origin`, `user.id` and
  `user.name`; existing SIEM queries are unaffected.
- `console.actions` joins the configuration (reload).
