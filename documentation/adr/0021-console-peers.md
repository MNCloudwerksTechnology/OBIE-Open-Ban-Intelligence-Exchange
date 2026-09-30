# ADR 0021: Console peers view — peer sources, trust, verdict counts and event tallies

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1684](https://openproject.niew.dev/work_packages/1684)
  (epic [#1680](https://openproject.niew.dev/work_packages/1680)); extends
  [ADR 0019](0019-local-web-console.md) and
  [ADR 0020](0020-console-overview.md)

## Context

The peers view answers "who is influencing my decisions, and is any peer
broken or misbehaving?". For every peer it shows whether it is configured
and connected, since when, the trust weight it carries, how many of its
verdicts the node holds and counts, and how many of its events were
accepted or rejected recently. Four things need a decision:

- **Which peers.** `obiectl peers` lists the connected peers only; a
  configured peer that cannot be reached is exactly the one the operator
  needs to see.
- **Whose events.** An event carries its publisher, but an invalid event
  has no publisher anyone can trust, and GossipSub penalizes the peer that
  forwarded it.
- **Verdict counts per peer** must be cheap: the list refreshes itself
  every 5 seconds, and the node may hold 1,000,000 indicators.
- **A peer's verdicts** have to be listed from the store, which has no
  index by publisher.

## Decision

### Peers

- The list is the union of the peers in `mesh.bootstrap`, the peers in
  `trust.publishers` and the connected peers — never this node. Each is
  marked *bootstrap peer*, *trusted publisher* or *not configured*, and
  *connected* or *disconnected*.
- The mesh keeps, for configured peers only (so the memory is bounded by
  the configuration), when a peer last disconnected or the mesh stopped
  (*last seen*), and for bootstrap peers the last failed dial and its
  error, until the next connection. A configured peer that has not been connected since `obied`
  started says so. A disconnected bootstrap peer shows its configured
  addresses; a connected peer the addresses of its open connections.
- **Trust.** The weight shown is the one the decision engine applies: the
  peer's `trust.publishers` weight, else `trust.default_weight`, labelled
  as the default. A weight of 0 is labelled *no influence on decisions*,
  and the *untrusted* filter shows exactly these peers. Names and weights
  follow reloads.

### Events

- Every received event is counted for the **peer that sent it** to this
  node, by validation outcome: accepted; rejected (invalid signature, not
  a valid obie/0.1 event, too large, expired or dated in the future, over
  the rate limit); and duplicates, which are normal in gossip and shown
  apart. A publisher's events relayed by other peers count for those
  peers; its influence shows in the verdicts the node holds from it.
- The gossip validator passes the sending peer to its observer. A tally
  counts the outcomes per peer over the **last hour, in twelve 5-minute
  buckets**; peers without events in the window are dropped, and at most
  4,096 other peers are tallied at once (a peer beyond that is not counted
  until others age out). Configured peers are always tallied, so that
  throwaway peers cannot crowd them out. The tally lives in the mesh and
  survives reconnects.
- *Extended by [ADR 0032](0032-gossip-instrumentation-and-attribution.md):
  the list and the peer page show each peer's GossipSub score, the
  thresholds it is below and, on the peer page, the score's components.*

### Verdict counts

- The decision engine already reads every active verdict of an indicator
  when it decides, and knows which contribute (ban verdicts of publishers
  with weight > 0). It now keeps, per kept decision, the publishers of its
  active verdicts and whether each contributes, and per publisher the
  number of active verdicts and of contributing ones, updated in the same
  step that replaces the decision. Publisher IDs are interned
  (`unique.Handle`), so a held verdict costs 16 bytes plus a slice header
  per indicator instead of the counter it replaces; the per-publisher
  counts are read in O(publishers).
- A reload re-decides every kept indicator, so the counts follow new
  weights.
- The view shows *held* and *counting* verdicts: a peer's verdicts count
  unless they are watch verdicts or its weight is 0.
- Verdicts from publishers that are neither configured nor connected are
  summed up in one note under the list, with the weight they carry.

### A peer's verdicts

- `/peers/{peer ID}` shows one peer — any publisher's ID, so later views
  can link verdicts to their publisher — and the verdicts the node holds
  from it, 50 per page, ordered by address, with a cursor.
- The store lists one publisher's active verdicts by walking the verdict
  keys (`v/<indicator>\0<publisher>`) and decoding only the records whose
  key names that publisher. It stops once a page is full, so a publisher
  with many verdicts pages quickly; one with few costs one walk over the
  keys, and one the engine counts no active verdict of is not looked up at
  all. This list is outside the page's refreshing region: it is read when
  the page is opened, not every 5 seconds.
- Each verdict links into the verdict view for its address
  (`/verdicts?address=…`) and the page to all the peer's verdicts
  (`/verdicts?publisher=…`), once that view exists; until then they name
  `obiectl show <address>` and `obiectl indicators --publisher <peer ID>`
  (ADR 0020).

### List mechanics

- Filtering (`show=all|connected|disconnected|untrusted`), sorting
  (`sort=peer|connection|trust|verdicts|rejected`, each in its natural
  direction) and paging (`page`, 50 peers per page) happen on the node
  through ordinary links, so they work without the script. The filter
  links show how many peers each would list. The refreshing region's
  fragment carries the same query, so a refresh keeps the operator's
  filter, order and page.
- The overview's *Peers connected* number links to `/peers` from now on
  (ADR 0020).

## Alternatives considered

- **Counting events per publisher.** Rejected events cannot be attributed
  to a publisher, and a publisher's own events arrive mostly directly
  anyway; per-forwarder counts are what peer scoring acts on.
- **Counting a publisher's verdicts from the store per request**: a walk
  over every verdict key on every refresh. Rejected for the engine's
  incremental counts.
- **A store index by publisher**: exact and fast, but a new key space and
  a state format change (ADR 0017) for one view. Rejected while the key
  walk is fast enough for a page opened on demand.
- **Counting per publisher in the engine's metrics pass** without keeping
  the publishers per decision: impossible, the decision drops its
  contributions; keeping full contributions costs far more memory.

## Consequences

- `gossip.Metrics.Observe` takes the sending peer.
- `mesh.KnownPeers` and `mesh.DefaultWeight` join `mesh.Peers`, which
  `obiectl peers` keeps using unchanged.
- The engine's memory per held indicator grows by about 40 bytes.
- `store.DB.PublisherVerdicts` joins the store's read methods.
- A view may now serve item pages below its path (`/peers/{id}`), marked
  as belonging to the view in the navigation (`aria-current="true"`), with
  their own refreshing fragment (`/api/peers/{id}`). A refresh restores
  the focus to the same link even where several links of a region share
  an address (a filter and a column heading).
