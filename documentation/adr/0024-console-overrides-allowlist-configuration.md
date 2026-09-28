# ADR 0024: Console overrides, allow-list and configuration views

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1687](https://openproject.niew.dev/work_packages/1687)
  (epic [#1680](https://openproject.niew.dev/work_packages/1680)); extends
  [ADR 0008](0008-local-event-store.md),
  [ADR 0013](0013-local-sovereignty.md),
  [ADR 0019](0019-local-web-console.md) and
  [ADR 0023](0023-console-verdicts.md)

## Context

The operator must be able to confirm that the node behaves as intended:
every override they set, every address the node never blocks and why, and
the configuration the node actually runs with — including the defaults
they never wrote down, when it was loaded, whether the last reload worked
and what is still waiting for a reload or a restart. Four things need a
decision:

- **The store forgets expired overrides** at their expiry (Badger's TTL and
  the sweep, ADR 0008), but the view must show them on request.
- **The node does not remember its configuration file**: `obied` decodes it
  once and keeps a `config.Config`. Which keys the file set, what each key
  means and whether a reload applies it live only in the configuration
  reference, and the reloader copies whole sections by hand.
- **Allow-list files are read once per load** (ADR 0013). A file that later
  disappears or gains a bad line goes unnoticed until the next reload is
  rejected, or the next start fails.
- **The console reads, it never edits** (ADR 0019): editing the
  configuration or the allow-list is out of scope.

## Decision

### Three views

- **`/overrides`** (navigation: *Overrides*): the overrides in effect —
  *Always allow* (force-allow) or *Always block* (force-block) — with the
  address or network (linking to its explanation), the note, when it was
  set and when it ends (*Never* without TTL). A force-block that has no
  effect says why: an overlapping force-allow or a protected allow-list
  entry beats it (the rule of `sovereignty.Judge`). Filters in a `GET` form:
  kind (allow or block) and an address or network (overlapping). The tab
  *Expired* lists the overrides that expired in the last 7 days, the most
  recent first. At most 1,000 rows are listed; the page says how many more
  match.
- **`/allowlist`** (navigation: *Allow-list*): every entry the node never
  blocks, grouped by origin, in the order of their precedence: built-in
  ranges (grouped by class: loopback, private, link-local, documentation…),
  this node's addresses, the bootstrap peers' addresses, `allowlist.cidrs`
  and each file of `allowlist.files`. A file shows how many entries were
  loaded and when (the running configuration's load), and what is on disk
  now (below). A group lists at most 1,000 entries; the lookup finds every
  one. On top, the lookup **Is this address protected?** (`?address=`).
- **`/configuration`** (navigation: *Configuration*): the load status, then
  every setting grouped by section with its running value, a *Default* mark
  for a key the file did not set, when a change takes effect (*reload* or
  *restart*) and a one-line explanation.
- All three are read when the page opens, like the verdicts view: no
  refreshing region. The overview's *Active overrides* number links to
  `/overrides`, its configuration line to `/configuration`.

### Expired overrides in the store

- An override that reaches its expiry is moved by the sweep from `o/` to
  `h/o/<indicator>`, kept until **7 days after its expiry**
  (`store.OverrideRetention`) by Badger's TTL; nothing else deletes it. A
  later override on the same indicator that expires replaces the entry.
- So that the sweep still moves an override that expired while `obied` was
  down, the `o/` entry's Badger TTL is its expiry plus the retention. Every
  read of `o/` already ignores overrides that are not active; `SetOverride`
  moves an expired, not yet swept override to `h/o/` before it replaces it,
  and `DeleteOverride` of one reports that there is none, as before, and
  leaves it to the sweep.
- Removed overrides (`obiectl unoverride`) are not kept: they did not
  expire, and the audit log records them. Overrides are created only by
  operators through the admin socket, so their number is not bounded by a
  cap; `ExpiredOverrides` walks `h/o/` like `Overrides` walks `o/`, and the
  walk of `o/` — which the engine repeats on every override change — does not
  grow with the expired ones.
- **No state format change** (ADR 0017): an older `obied` ignores `h/o/`
  and reads an `o/` entry past its expiry as not active; its sweep deletes it
  as before if its index entry is still due.

### The configuration reference in code

- `internal/config` lists every key: its one-line summary, whether a reload
  applies it (`reload`) or only a restart (`restart`), and whether its value
  is a secret. A test keeps the list equal to the keys of `Config` and its
  *reload/restart* column equal to the
  [configuration reference](../operations/configuration.md); another fails
  for a key whose name suggests a secret (`token`, `secret`, `password`,
  `credential`, `private`) that is not marked as one.
- No key holds a secret today. A secret key's value would be shown as
  *set* or *not set*, never itself. The node's private key and the console
  token are not part of the configuration and are never shown.
- `config.LoadFile` returns, besides the configuration, the path and the key
  paths the file set (the decoder records them already). `obied` passes it to
  the daemon; a successful reload replaces the keys it applies and their
  *set* marks, exactly the keys the reference marks `reload`.
- `decision.default_ttl` is marked `restart`, but the reloader copied it with
  the rest of `decision`. It now keeps the running value, and a changed
  `decision.default_ttl` is reported as needing a restart like every other
  restart key.

### Load status and the file on disk

- The load record of the reloader (ADR 0020) keeps the running
  configuration, the keys set and the file's path, beside when it was loaded
  and how the last reload went.
- The view shows when the running configuration was loaded — at start or
  by a reload — and whether the last reload succeeded. After a rejected one
  it shows the error and that the configuration loaded at … is still active.
- When the page opens, the daemon reads and validates the file on disk and
  compares it with the running configuration key by key: a key whose value
  differs is *changed on disk*, *not active until a reload* or *waiting for a
  restart*. A file that cannot be read or is invalid, or that names an
  allow-list file that cannot be loaded (checked with `sovereignty.CheckFile`,
  as a reload would load it), is shown with the error: a reload would be
  rejected and the running configuration kept. A file that changed only in
  comments or layout changes nothing.

### Allow-list files and warnings

- The allow-list keeps, for each file, its path, the entries it loaded and
  the SHA-256 of what it read, and the addresses it could not determine:
  bootstrap peers whose DNS name did not resolve and interface addresses
  that could not be listed (both only logged so far).
- When the page opens, the daemon reads each file again with
  `sovereignty.CheckFile`, which reports every line it rejects (at most 20)
  instead of stopping at the first. The view warns when a file is missing or
  unreadable, or has lines the node rejects — the entries loaded stay in
  effect, but the next reload is rejected and `obied` would not start — and
  notes when it changed since it was loaded (a reload applies it).
- Loading stays strict (ADR 0013): a file with a bad line is never loaded
  in part.

### The lookup

- The lookup accepts any address or network, also a private or
  special-purpose one, and judges it with `sovereignty.Judge` against the
  running allow-list and the overrides in the store, as the decision engine
  does. `Judge` weighs only a force-block on the range itself; the firewall
  also blocks every address in a network whose force-block takes effect, so
  unless a protected entry or a force-allow decides, the lookup also looks
  for such a force-block around the range. It names the rule that decides — a
  protected entry (built-in, own or bootstrap; not even a force-block beats
  it), a force-allow override, an allow-list entry of the operator, a
  force-block on the range or around it that beats such an entry, or none —
  with the matching entry or override, lists the other entries that overlap
  it (at most 1,000), and links to the decision's explanation.

## Alternatives considered

- **Keeping expired overrides in `o/`** until the retention ends: the engine
  walks `o/` on every override change and the overview every 5 seconds.
  Rejected for a keyspace of their own.
- **24 hours like ended verdicts (ADR 0023)**: overrides are few and set by
  hand; a week covers "the block I set last Friday". Their size is no
  concern.
- **Parsing the configuration reference Markdown at run time** for the
  explanations: brittle, and the Markdown cells are paragraphs with links.
  A Go list tested against it keeps both honest.
- **Loading allow-list files leniently**, skipping bad lines: a typo would
  silently leave an address unprotected. Rejected; the view tells the
  operator before the next reload does.
- **A refreshing region for the load status**: it would re-read and parse
  the file every 5 seconds while the page is open; reloading the page is
  enough to follow a reload.

## Consequences

- `store.DB` gains `ExpiredOverrides`; an `o/` entry lives until 7 days after
  its expiry.
- `config` gains `Settings`, `LoadFile` and per-key values; `daemon.Options`
  gains the file the configuration was read from, and `LoadConfig` returns
  one.
- `sovereignty.Allowlist` gains `Files`, `Warnings` and `Overlapping`;
  `CheckFile` reads a file without loading it.
- The console reads a new `RuleSource`; the navigation grows to eight views.
