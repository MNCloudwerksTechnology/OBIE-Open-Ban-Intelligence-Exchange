# ADR 0022: Console decisions, explanations and firewall view

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1685](https://openproject.niew.dev/work_packages/1685)
  (epic [#1680](https://openproject.niew.dev/work_packages/1680)); extends
  [ADR 0019](0019-local-web-console.md),
  [ADR 0020](0020-console-overview.md) and
  [ADR 0021](0021-console-peers.md)

## Context

The operator must be able to browse every decision the node holds, see
exactly why an address is or is not blocked, and see whether the firewall
does what the decisions say. Five things need a decision:

- **A list that stays fast with 1,000,000 decisions.** The decisions live
  in the decision engine's memory; filtering, searching, sorting and
  paging must happen on the node without copying them.
- **What a decision is "about".** The list shows a reason category (for
  example `password_bruteforce (ssh)`) and filters by reason and by
  publisher, but a kept decision drops its verdicts.
- **Whether the firewall applies a decision, and why not.** The
  reconciler knows it after every pass, but only as counts.
- **Addresses and networks in URLs**: an explanation must have a link, and
  a network contains a slash.
- **A list that changes while the operator reads it.**

## Decision

### Data from the decision engine

- **Reason categories.** A held verdict (ADR 0021) now also keeps its
  category — the evidence reason and the protocol, interned like the
  publisher — and the engine counts the decisions per category. A
  contribution in an explanation carries its reason and protocol; the
  admin API's explanation (`reason`, `protocol`) and `obiectl explain`
  (column `REASON`) show them too, so the console and the CLI explain the
  same facts.
- **Browsing.** `Engine.Browse` makes one pass over the kept decisions
  under the engine's read lock: it applies the filters (state, category,
  publisher, an address or network, and a predicate the caller passes for
  the firewall state), counts the matches by state for the filter links,
  and keeps only the best page (at most 51 decisions) in a bounded
  selection. Time is O(n log page), memory O(page); nothing is copied but
  the page. Paging is by keyset: an opaque cursor holds the sort value
  and the indicator key of the last (or first) decision of a page, so a
  page is as cheap on page 1 as on page 20,000, new decisions do not
  shift the page being read, and the position ("51–100 of 1,000,000") is
  counted in the same pass. The lock is held as long as the engine's own
  pass over its decisions after every evaluation (for its metrics).
- **Search.** An address finds its own decision and those of the networks
  that contain it, by looking up the keys of its covering prefixes (at
  most 17 for IPv4, 97 for IPv6: CIDR indicators are /16 to /31 and /32
  to /127) — no scan. A network finds everything that overlaps it, by
  scanning.
- **Sort orders:** last decided first (default), address (IPv4 before
  IPv6, numerically, a network before the addresses in it), state (block,
  allowed, none), score, contributing publishers (most first), expiry
  (soonest first). Ties fall back to the indicator key.
- **Generation.** The engine counts its re-evaluations of kept decisions;
  the list page shows whether the count moved since the list was read.
- `Engine.Decision(key)` reads one kept decision, `Engine.Policy` the
  policy in effect.

### Firewall state

- After every successful pass the reconciler keeps a snapshot: the mode,
  when the pass ended, what the backend holds after it (the listed entries
  minus the removed plus the added ones, sorted and disjoint) and the
  prefixes it skipped with the reason (allow-list, `enforce.max_entries`).
  The snapshot is immutable and replaced by the next pass; a sequence
  number changes whenever the entries, the skipped prefixes or the mode
  change.
- `Lookup(prefix)` answers in O(log entries): applied by its own entry,
  applied through the wider entry that contains it, skipped and why, or
  not part of the last pass (decided after it, expiring within a second,
  or deferred). This drives the list's firewall column and filter, the
  explanation and the firewall view. A decision that is not a block is
  still reported as applied when an entry of a wider block covers it: that
  entry wins, and the operator must see it.
- In observe mode nothing is applied; every view says that this is by
  design, not a failure. After a failed pass the snapshot of the last
  successful one is shown with the failure.

### Views

- **`/decisions`** — the list: 50 decisions per page; state links with
  counts; a search field and reason, publisher and firewall filters in a
  `GET` form; column headings that sort; *First*, *Previous*, *Next* and
  *Last* pages. The list is read when the page opens, not every
  5 seconds (a pass over 1,000,000 decisions per refresh per open page is
  not negligible load). Its refreshing region is only a status line that
  says when the list was read and, once the engine's generation moved,
  that the decisions changed since, with a link that reloads the same
  view. A searched address or network that is a valid indicator is
  explained in one line above the list — also one the node knows nothing
  about — with a link to its explanation.
- **`/decisions/{address or network}`** — the explanation, for any valid
  indicator (an unknown one has no verdicts and is not blocked, and the
  page says whether it would be protected). The item route takes the rest
  of the path (`{id...}`), so `/decisions/198.51.100.0/24` and
  `/decisions/2001:db8::1` work as typed. The explanation re-evaluates the
  indicator from the store like `obiectl explain` (one indicator, the
  overrides and the allow-list: cheap) and refreshes every 5 seconds: a
  decision that changes while it is open is shown changed. It lists the
  kept decisions of the networks that contain the indicator, with their
  state, and names the firewall entry that covers it, so the operator
  sees which entry wins; a network links to the list searched by it for
  the decisions inside it.
- **`/enforcement`** (navigation: *Firewall*) — a refreshing summary
  (mode, backend, the last pass, applied entries, decided blocks,
  covered, refused, capped, failures) and, read when the page opens, the
  differences and the backend's entries: the entries the backend lists
  right now (`Reconciler.Entries`), each with the decision behind it;
  entries without a decided block or with another expiry; and the first
  page of the decided blocks that are not applied, with why, linking to
  the filtered decisions list for all of them.
- **Copy and share.** Every view's state is in its URL, so a link works
  for anyone who may sign in on the same host (sign-in returns to it).
  The script adds a *Copy* button to every element marked `data-copy` and
  a *Copy link* button to the page; it uses the Clipboard API (loopback
  origins are secure contexts) and selects the text where that fails.
  Without the script, addresses are plain selectable text. The script
  ignores and restores its buttons when it swaps a refreshed region, so
  they never make a region look changed.

## Alternatives considered

- **Copying the decisions out of the engine per request** and sorting
  them in the console: hundreds of megabytes per page at 1,000,000
  decisions. Rejected.
- **Offset paging** (`page=20000`): a bounded selection of offset + 50
  decisions, i.e. a full sort for deep pages. Rejected for keyset
  cursors.
- **Sorted indexes kept by the engine** (per sort order): fast pages at
  any size, but every evaluation pays for every index, and the memory
  per decision grows by a multiple. Rejected while one pass per page
  opened is fast enough (see
  [performance](../operations/performance.md#console)).
- **Refreshing the list every 5 seconds** like the peers view: one pass
  over every decision per open page every 5 seconds, and rows that move
  under the reader. Rejected for the out-of-date line.
- **Recomputing the firewall state per request** from the gate's blocks
  and the backend's list: a copy of every block and a backend call per
  page. Rejected for the reconciler's snapshot.
- **Encoding the network in the path** (`198.51.100.0%2F24`) with the
  one-segment item route: links would not work as typed. Rejected.

## Consequences

- The engine's memory per held verdict grows by 8 bytes (the interned
  category).
- `decision.Contribution` gains `Reason` and `Protocol`; the admin API's
  explanation gains `reason` and `protocol`; `obiectl explain` gains a
  column.
- The reconciler keeps the snapshot of its last pass (the entries it
  already lists, plus the skipped prefixes it already keeps).
- A view may serve item pages whose ID is the rest of the path, and may
  declare a refreshing region with its own, cheaper data.
- The overview's decision numbers and *Firewall entries* link to the new
  views (ADR 0020).
