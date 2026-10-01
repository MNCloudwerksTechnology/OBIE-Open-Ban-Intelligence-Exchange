# ADR 0023: Console verdicts view — ended verdicts, a verdict index in the engine

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1686](https://openproject.niew.dev/work_packages/1686)
  (epic [#1680](https://openproject.niew.dev/work_packages/1680)); extends
  [ADR 0008](0008-local-event-store.md),
  [ADR 0019](0019-local-web-console.md),
  [ADR 0021](0021-console-peers.md) and
  [ADR 0022](0022-console-decisions-and-firewall.md)

## Context

The operator must see what the node told the mesh and what the mesh told
the node: every verdict this node published, whether it was revoked and
why; every verdict held from other publishers, filtered by publisher,
reason, address and state (*active*, *revoked*, *expired*); all verdicts
on one address, as `obiectl show` prints them; and totals per publisher.
Expired verdicts are shown only on request, and the list must stay fast
with 1,000,000 held indicators. Three things need a decision:

- **The store forgets ended verdicts.** A revoked verdict is kept only as
  a flag on its record, without the revocation's reason; an expired one
  is gone from every read at its expiry and deleted by the next sweep
  (ADR 0008).
- **The store has no index by publisher or reason.** Walking and decoding
  1,000,000 verdict records per page takes seconds.
- **Where the view links.** The peers view (ADR 0021) and the overview
  (ADR 0020) already link to `/verdicts?publisher=…`, `/verdicts?address=…`
  and `/verdicts` once the view exists.

## Decision

### Ended verdicts in the store

- A new keyspace keeps the verdicts that ended, for **24 hours after the
  verdict's expiry** (`store.EndedRetention`; *since
  [ADR 0032](0032-gossip-instrumentation-and-attribution.md) configurable
  in `store.ended_retention`, by default `store.DefaultEndedRetention`,
  30 days*):
  `h/r/<indicator>\x00<publisher>\x00<category>` for revoked and
  `h/x/<indicator>\x00<publisher>\x00<category>` for expired verdicts, the
  category being the evidence reason and the protocol as the decision
  engine names it (`password_bruteforce/ssh`). The value is the verdict
  record — the signed event — and, for a revoked one, the revocation: its
  event ID, reason and time. Badger's TTL removes an entry at the verdict's
  expiry plus the retention; nothing else deletes it. A later verdict of
  the same publisher on the same indicator and category that ends the same
  way replaces the entry.
- **Bounded.** Any publisher's verdicts are stored, so a flood of
  short-lived verdicts would otherwise keep a day's worth of them on disk,
  past `store.max_indicators` (ADR 0017). The store keeps, of other
  publishers, at most a tenth of `store.max_indicators` (at least 1,000;
  100,000 by default) revoked verdicts and as many expired ones — each
  state its own share, so that a flood of expiries does not crowd out the
  rarer revocations, which carry the why. Beyond that it keeps no more of
  them until older ones are forgotten, and logs a warning once; this
  node's own are always kept, as eviction never touches them either, and
  do not count. The store counts the ended verdicts by publisher and state
  at start, adds every one it keeps (a replaced entry is no new one), and
  recounts them after every sweep, because Badger's TTL forgets them
  silently. The gauge `obie_store_ended_verdicts{state}` shows the counts,
  and the view says when a state's share is full.
- **Revoked** entries are written when the revocation applies, also for a
  revocation that arrived before its verdict: its ID, reason and time are
  then kept under `h/p/<verdict id>\x00<publisher>` for as long as the
  revocation marker (`r/`), so the reason is known when the verdict comes.
  The verdict record in `v/` stays revoked until its expiry, as before.
- **Expired** entries are written by the sweep when it removes an expired,
  unrevoked record, and when a record is replaced or evicted after its
  expiry but before the sweep. For that, a verdict record's Badger TTL is
  now its expiry plus the retention: every read already ignores inactive
  records, and `Put` treats a record past its expiry as absent when it
  decides whether a verdict is newer, exactly as before, when Badger hid
  it. After a downtime of up to 24 hours the first sweep still archives
  what expired meanwhile. The sweep handles 250 expiry entries per
  transaction instead of 1,000, so a batch of archived events of the
  maximum size stays below Badger's transaction limit (15 % of the 16 MiB
  memtable).
- **Reads.** `EndedVerdicts` walks the keys of the ended verdicts only —
  the indicator, the publisher and the category are in the key, so every
  filter is applied without reading a value — counts the matches of both
  states and the matches before the cursor, and decodes only the page.
  `EndedCounts` returns the counts by publisher and state the store keeps
  in memory, without reading the database.
- **Size.** An entry holds about 1 KB: at most about 200 MB of other
  publishers' at the default caps. With 1,000,000 indicators and the default verdict lifetime
  of 7 days about 150,000 verdicts expire a day, more than the cap: the
  view then lacks some of the other publishers' verdicts that ended last.
  Pushing the oldest out instead would need an index of the ended verdicts
  by time, and would let a flood push out what the operator wants to see.
  A walk over the keys of 100,000
  takes about 41 ms (see
  [performance](../operations/performance.md#console)). Evicted verdicts
  (store full) and verdicts superseded by a newer one of the same
  publisher are not kept: they did not end.
- **No state format change** (ADR 0017): an older `obied` ignores the `h/`
  keyspace and the new field of a record, and Badger's TTL removes the
  ended verdicts. After a rollback, a record past its expiry that the
  sweep has not removed yet (at most a sweep interval, or a downtime) is
  read as inactive, as every read ignores inactive records, and the older
  sweep deletes it as before.

### Active verdicts from the decision engine

- The engine keeps the publisher and category of every active verdict per
  decision (ADR 0021, ADR 0022). `Engine.Verdicts` makes one pass over
  them under the read lock: it keeps the verdicts of one publisher, of
  every publisher but one (this node: *received*), of one category and of
  one indicator (looked up, not scanned), orders them by indicator key and
  publisher — the order of `obiectl indicators` and of the store's keys, so
  the cursor means the same for ended verdicts — counts the matches and
  those before the cursor, and keeps the page in a bounded selection:
  O(n log page) time, O(page) memory. Each verdict comes with whether it
  counts and the state of its decision. A page over the 2,000,000 active
  verdicts of 1,000,000 decisions takes about 22 ms.
- The daemon reads the page's events from the store, one indicator at a
  time (at most 50), and the publishers' trust weights from the engine's
  policy.

### The view

- **`/verdicts`** (navigation: *Verdicts*), 50 verdicts per page, read
  when the page opens — no refreshing region: like the decisions list, a
  refresh would re-read a page every 5 seconds, and a verdict page has no
  cheap generation to watch.
- **Whose:** *All publishers* (default), *This node* and *Received* (every
  publisher but this node), or one publisher (`publisher=<peer ID>`,
  offered in the filter form with the counts per publisher).
- **State tabs:** *Active* (default), *Revoked* and *Expired*, each with how
  many verdicts match the other filters. Expired verdicts appear only
  under their tab and are marked *Expired* with when; revoked ones show
  when, why and the revocation's ID.
- **Filters** in a `GET` form: address or network (`address=`, the exact
  indicator, which makes the page the address's verdicts from every
  publisher, as `obiectl show` gives them, with a link to its explanation),
  reason (the categories of the active verdicts) and publisher.
- **A row:** the address (linking to its verdicts), the publisher (linking
  to its peer page; *This node* for its own) with its trust weight or *No
  weight*, action and confidence, reason and protocol, the event count and
  the evidence's log hash — the evidence itself never left the reporting
  node — the event ID, issued and expires, the state, whether an active
  verdict counts in its decision, and the decision's state linking to the
  explanation.
- **Totals** at the top: this node's active, revoked and expired verdicts;
  the verdicts received per publisher (the 20 with the most active ones),
  with their weight, each number linking to the list.
- The peer page's verdicts, the overview's *Indicators held* and every
  explanation link into the view.

## Alternatives considered

- **Keeping expired verdicts in `v/`** until the retention ends: listing
  or counting ended verdicts would decode every verdict record. Rejected
  for a keyspace of their own.
- **Counting ended verdicts incrementally in memory**: Badger's TTL
  removes them silently, so counts would drift; an expiry index for them
  would add a second sweep. Rejected while a walk over their keys is fast.
- **Listing active verdicts from the store** (`ListIndicators`,
  `PublisherVerdicts`): a reason filter decodes every record. Rejected for
  the engine's in-memory verdicts.
- **A page per verdict** (`/verdicts/{event ID}`): an ended verdict is not
  found by its ID; the row holds every field `obiectl show` prints.
  Rejected.
- **A configurable retention** (`store.keep_ended`): no operator need yet;
  a key can be added later without a format change.

## Consequences

- `store.DB` gains `EndedVerdicts` and `EndedCounts`; the store holds ended
  verdicts for 24 hours and a verdict record until 24 hours after its
  expiry; `DB.Verdicts` and `store.max_indicators` still count the verdict
  records in `v/` only.
- `decision.Engine` gains `Verdicts`.
- The console reads a new `VerdictSource`; the links the peers view and
  the overview reserved for the view now point to it.
