# ADR 0020: Console overview — node data, attention conditions and self-refreshing views

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1683](https://openproject.niew.dev/work_packages/1683)
  (epic [#1680](https://openproject.niew.dev/work_packages/1680)); extends
  [ADR 0019](0019-local-web-console.md)

## Context

The console's first view answers "Is my node healthy, and what is it
doing?" within seconds: identity, mode, the readiness of every part, the
key numbers and the conditions that need the operator. Three things need
a decision:

- **Where the numbers come from.** The overview must stay cheap with
  1,000,000 held indicators and must work while subsystems start or stop,
  because the console runs longer than all of them (ADR 0019).
- **What "needs attention" means.** A node that has just started has no
  peers and no data; that is not a failure and must not look like one.
- **How the page stays current.** ADR 0019 lets the script refresh only
  the health indicator.

## Decision

### Data

- The daemon passes the console one function that reads the node's
  numbers at one moment (`console.Facts`): connected and bootstrap peers
  from the mesh; indicators, verdicts and decisions by state from the
  decision engine; applied entries, skipped blocks and failures from the
  last enforcement pass; overrides, events and verdict records from the
  store; and the configuration loads from the reloader. Each read is
  O(1) or bounded by a small list (peers, overrides): the engine keeps
  the counts it computes for its metrics after every evaluation pass, so
  a page never walks the decisions.
- **The console decides what is available,** from the lifecycle status
  of the subsystem that owns a number (mesh, decision, enforce, store). A
  number whose subsystem is not running is shown as *waiting for … to
  start* (or *stopped*), never as 0. A read that fails (e.g. the store's
  overrides) is shown as not available with the reason; the rest of the
  page is unaffected.
- **Configuration loads.** The reloader records when the running
  configuration was loaded (at start, then at every successful reload),
  the last rejected reload with its error until a later one succeeds, and
  the settings that wait for a restart.
- Times are shown in UTC, like `obiectl`.

### Detail links

A number links to the view that details it. The epic's views have fixed
paths — `/peers`, `/decisions?state=block|none|allowed`, `/verdicts`,
`/enforcement`, `/overrides` — and a number links only once the console
has a view at that path; until then it names the `obiectl` command that
shows the detail. So the overview never links to a page that does not
exist, and each later view lights its links up by being added to the view
list.

### Attention conditions

Each condition is a sentence in plain language with a suggested next
step, *warning* or *info*, warnings first:

| Condition | When |
|-----------|------|
| No peer connected / no peer configured | The mesh runs with 0 peers, after the first 2 minutes |
| No event received | The store holds no verdicts and none arrived since the start, after the first 2 minutes |
| Enforce mode with the dry-run backend | `node.mode: enforce` and `enforce.backend: dryrun` |
| Enforce mode, nothing applied | The last pass ran in enforce mode, blocks are decided and 0 entries are applied |
| Decided blocks differ from applied entries | The last pass failed, or blocks were refused by the allow-list right before apply |
| Blocks capped by the entry limit | Blocks were left out over `enforce.max_entries` |
| A part is not ready | A running subsystem reports not ready or degraded (unless a condition above explains it) |
| Configuration reload rejected | The last reload failed; the node keeps the previous configuration |
| Changes wait for a restart | The last reload found settings only a restart applies (info) |

**Startup grace.** For the first 2 minutes a node without peers or data
shows an empty state instead — what will appear (peers, verdicts,
decisions, blocks) and when — and numbers that are still zero say so in
words. After the grace, the same state is a condition to act on.

### Self-refreshing views

A view may declare a **fragment**: an endpoint under `/api/` that renders
the view's content region only, behind the same guards and headers as
every request, and answering 401 without a session like `/api/health`.
The page marks the region with the fragment's path. While the page is
visible, the script fetches the fragment with each health poll (every 5
seconds), parses it into an inert document (`DOMParser`, which runs no
script), and replaces the region's children when they changed, keeping
the focused link focused. The region says when its data was read. The
HTML is the server's escaped template output from the console's own
origin — the same bytes a reload would show — so the CSP and the escaping
of ADR 0019 are unchanged. Without the script the page is still complete;
a reload shows current data.

This extends ADR 0019's rule that the script refreshes only the health
indicator: it now also refreshes regions a view declares, and nothing
else.

## Alternatives considered

- **`<meta http-equiv="refresh">`**: no script, but it reloads the whole
  page, loses the keyboard focus and scroll position every few seconds
  and makes screen readers start over. Rejected.
- **A JSON endpoint rendered by the script**: every view would be written
  twice, in a template and in script, and tested outside Go. Rejected.
- **Counting the decisions per request**: O(n) under the engine's lock
  on every refresh of every open page. Rejected for the counts kept per
  evaluation pass.
- **Linking every number to its future path now**: the overview would
  link to pages that answer 404 until #1684–#1688 ship. Rejected.

## Consequences

- The overview replaces the placeholder home page at `/`.
- `enforce.Status` also reports how many decided blocks the last pass
  considered and how many share an entry with another block, so the
  overview can explain why decided blocks and applied entries differ.
- Later views (#1684–#1688) register at the paths above; a view that
  follows live data declares a fragment instead of adding its own
  refresh.
