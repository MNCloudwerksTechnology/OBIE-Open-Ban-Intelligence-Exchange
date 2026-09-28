# ADR 0025: Console activity timeline — the audit trail as its source, a live feed

- **Status:** Accepted
- **Date:** 2026-09-29
- **Work package:** [#1688](https://openproject.niew.dev/work_packages/1688)
  (epic [#1680](https://openproject.niew.dev/work_packages/1680)); extends
  [ADR 0015](0015-metrics-and-audit-log.md),
  [ADR 0019](0019-local-web-console.md) and
  [ADR 0020](0020-console-overview.md)

## Context

The operator wants to watch what the node does right now and look back
over what it did: blocks added, updated and removed, addresses spared by
the allow-list, overrides, local reports, revocations, peers connecting
and disconnecting, configuration reloads and mode changes — newest first,
live within 2 seconds, filtered by kind and address, and telling the same
story as the decision audit log a SIEM reads. Four things need a decision:

- **Where the history comes from.** The audit log (ADR 0015) records
  decisions, overrides, reports and revocations, but not peers, reloads or
  mode changes. It is a file that survives restarts; nothing in the node
  keeps its records in memory, and without `audit.path` there is none.
- **How new entries reach an open page within 2 seconds.** The refreshing
  regions of ADR 0020 poll every 5 seconds and replace the whole region.
- **Bursts.** An attack wave changes thousands of decisions per minute; a
  page that adds each of them as it happens freezes the browser.
- **The audit log may be off,** unreadable, or rotated by logrotate.

## Decision

### Every kind of activity is an audit record

- Four new actions join the audit log, written like the others, with
  `event.outcome` `success`:

  | `event.action` | When | Fields |
  |---|---|---|
  | `peer-connected` / `peer-disconnected` | The mesh connects to a peer or loses its last connection to it | `obie.peer_id`, `obie.peer_name` (its `trust.publishers` name, if any) |
  | `config-reloaded` | A reload (SIGHUP) took effect, also one that changed nothing, e.g. logrotate's | `obie.settings` (the keys it changed and applied), `obie.restart_settings` (the keys that wait for a restart) |
  | `mode-changed` | A reload switched `node.mode` | `obie.mode` (the new mode), `obie.previous_mode` |

  `event.reason` says the same in one line. A rejected reload changes
  nothing and is not recorded, like before; the overview and the
  configuration view show it.
- `obie.indicator`, `source.ip` and `rule.name` are left out of records
  that are about no address; every record that has one keeps them, so
  existing SIEM queries are unchanged.
- The reloader records the reload after applying it (and after the audit
  log was reopened, so a rotated log starts with it) and the mode change
  after the gate switched. The mesh calls a function for every change of a
  peer's connectedness from its connection watcher, where it logs it today;
  the daemon turns it into a record.

### The audit trail keeps its tail in memory

- The audit log is always created. Every record gets a sequence number and
  is kept, as the same ECS document the file receives, in a ring of the
  **last 10,000 records** — also when `audit.path` is empty, in which case
  nothing is written to a file and the subsystem is not registered, as
  before. Appending to the ring and to the file happen under one lock, so
  the ring and the file hold the same records in the same order.
- The file is opened read-write (append-only writes, as before) so that the
  node can read back what it wrote through the same descriptor, whatever
  logrotate did to the path. If it may only be opened for writing, or is
  no regular file (such as `/dev/stdout`), the node writes as before and
  the console says it cannot read it. Every file opened starts a new
  generation, which positions in the file carry; a reload that reopens
  the same file keeps the generation, so paging survives it.

### The timeline reads the audit trail

- **`/activity`** (navigation: *Activity*, after *Overview*): the entries
  newest first, 100 per page, each with its time, what happened, the
  address (linking to its decision), the peer or the setting it concerns,
  the reason and further links: an override to the overrides view, a report
  or revocation to this node's verdicts on the address, a reload to the
  configuration view, a mode change to the `node` section there.
- **Its history is the audit log file**, read backwards from its end — the
  same records a SIEM ingests, also those written before the last restart,
  for as long as the file holds them. The file is the current one: after
  logrotate reopened it, older records are in the rotated files and the
  SIEM; the page says since when the file holds records (the time of its
  first line). Lines that are not records of this format are skipped and
  counted, and so is a line longer than a page may read.
- **Paging and filtering happen on the node.** The page opens at a mark
  taken under the log's lock: the sequence number of the last record and the
  file's size. It reads the file backwards from the size, and the live feed
  continues after the sequence number, so no record is missed or shown
  twice. *Older entries* continues from the byte offset where the page
  ended. A filter — one kind of activity, and an address or network that
  overlaps the record's — is applied while reading; one page reads at most
  16 MiB of the file and then offers to search further back from there.
- **Without a readable file** — `audit.path` empty, the file only writable,
  or closed while the node shuts down — the timeline reads the memory ring
  instead and says which history is not available: before obied started,
  beyond the last 10,000 records, after the next restart, and, with no
  audit log, nothing reaches a SIEM; and how to switch the audit log on.
- **The overview** shows the last 5 entries in its refreshing region, read
  the same way, with a link to the timeline.

### The live feed

- `GET /api/activity?after=<sequence number>` with the page's filters
  answers with the rows recorded since, as escaped template output, and the
  sequence number to continue from; it answers 401 without a session like
  every endpoint under `/api/`. On the first page, while the page is visible
  and not paused, the script asks every **1 second**, parses the answer into
  an inert document (as ADR 0020) and puts the new rows on top. So an
  entry appears within about 1 second, and nothing is requested while the
  page is hidden or paused. *Pause* stops the requests and drops an answer
  still on its way; *Resume* fetches everything since the pause at once. A
  failed request is said in the live status, and a session that ended
  hides the live controls.
- **Bursts are summarised on the node.** One answer holds at most the
  newest **50** matching rows; the rest become one summary row — how many
  entries of which kinds between which times were not listed one by one,
  and a link that reloads the page to page through them. Records that
  already left the memory ring are counted as such. The page keeps at most
  **500** rows: older ones are removed and the page says so, with a link
  that reloads it. So a burst costs the browser at most 50 rows a second.
- This extends ADR 0020: besides the health indicator and the refreshing
  regions, the script now also follows the timeline's live endpoint, and
  nothing else.

## Alternatives considered

- **A separate event stream for the console**: two recorders of the same
  changes could disagree, and the console would not show what the SIEM
  sees. Rejected; the console reads the audit trail.
- **Server-sent events or WebSocket push**: no polling, but a connection
  held open per page past the server's write timeout (ADR 0017), and
  reconnection logic in the script. A request per second while the page is
  visible is negligible on the host and needs neither. Rejected.
- **The refreshing region of ADR 0020 at a shorter interval**: it replaces
  the whole list on every change, losing the reader's place and keyboard
  focus as entries arrive. Rejected for rows added on top.
- **Reading rotated files too** (`audit.jsonl.1`, compressed ones): their
  names depend on logrotate's settings, and long-term history is the SIEM's
  job, out of scope here.
- **Recording rejected reloads** with `event.outcome: failure`: the audit
  log records only changes that took effect (ADR 0015). Kept that way.

## Consequences

- The audit log gains four actions and five fields; SIEM parsers that
  expect `obie.indicator` in every record see it only in records about an
  address. The node keeps up to 10,000 audit records in memory (a few MiB),
  whether or not the audit log is on.
- `audit.New` accepts an empty path for a memory-only trail; the daemon
  always passes the console one.
- The mesh gains `Options.Connections`.
- The console's navigation grows to nine views; the overview gains a
  *Recent activity* section.
