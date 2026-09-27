# ADR 0012: Reporting, revoking and inspecting local verdicts

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1657](https://openproject.niew.dev/work_packages/1657)

## Context

Operators and detectors such as Fail2Ban must turn a local detection into a
signed `indicator.verdict` with one call, withdraw it again and inspect what
the node holds — without any raw log content leaving the host (spec §13,
[PRV-1]) and without the control interface being reachable by anyone but
local root and the `obie` group. The admin API (ADR 0003) is the only local
control interface; the store (ADR 0008) and `Mesh.Publish` (ADR 0009)
already hold and spread events.

## Decision

- **Package:** `internal/verdicts` owns this node's own events. It builds,
  signs (through the node identity) and publishes verdicts and revocations,
  and lists the active verdicts from the store. It depends on the store, a
  `Publisher` interface (implemented by the mesh) and an
  `obieproto.Signer` (implemented by `identity.Key`), so the private key
  still never leaves `internal/identity`. `obieproto.SignWith` signs with
  such a signer and verifies the result; `obieproto.NewID` generates the
  UUIDv7 event IDs (standard library only, no new dependency).
- **Report defaults:** confidence 0.8, action `ban`, TTL
  `decision.default_ttl`; a longer TTL is capped at `decision.max_ttl`, a
  TTL under 60 s is a validation error. The TTL is accepted as seconds (a
  JSON number) or as a duration string (`"12h"`, `"7d"`).
- **Evidence privacy:** `evidence_lines` are hashed on receipt —
  `sha256` over the lines joined with `\n` — into `evidence.log_hash` and
  then dropped. They are never stored, published or logged; only the hash
  and the counts are shared.
- **Refusals:** an indicator that overlaps `allowlist.cidrs` or lies in
  non-public space (as `obieproto` defines it, documentation ranges
  included) is refused with HTTP 422 and an explanation; no event is issued.
- **Refresh and coalescing:** a report on an indicator with an active
  verdict of this node issues a refreshed verdict (new ID, new expiry,
  cumulative `evidence.events`) that supersedes the old one — never a
  second verdict. At most one verdict per indicator is issued per 60 s,
  measured from the active verdict's `issued_at` (plus one second, because
  `issued_at` is truncated to whole seconds): a report inside that window
  is *coalesced* — it issues nothing, only its event count is kept for the
  next refresh (its action, confidence, TTL, MITRE IDs and evidence hash
  are dropped), and the response carries the current verdict with
  `coalesced: true`. Pending counts live in memory until the verdict they
  belong to expires or is refreshed or revoked.
- **Publishing** is not interrupted by a client that disconnects: the
  event may already be stored locally when it is sent to the mesh.
- **Revocations** name a verdict ID or an indicator and apply only to this
  node's own *active* verdicts; nothing to revoke is HTTP 404.
- **Endpoints:** `POST /v1/reports`, `POST /v1/revocations`,
  `GET /v1/indicators?active=true&publisher=&limit=&cursor=` (cursor is the
  opaque key returned as `next_cursor`; only active verdicts are kept, so
  `active` accepts only `true`) and `GET /v1/indicators/{indicator}`.
  Validation errors are HTTP 400 with the offending field named.
- **Peer credentials:** every admin API request is authorized by the
  connecting process's `SO_PEERCRED` credentials, on top of the socket's
  file mode: root, the user obied runs as, and members (primary or
  supplementary) of `admin.socket_group` are allowed; anyone else gets
  HTTP 403. A connection whose credentials cannot be read is refused. On
  platforms without `SO_PEERCRED` only the file mode applies.
- **CLI:** `obiectl report`, `revoke`, `indicators` and `show` wrap the
  endpoints with human-readable output and `--json`.

## Consequences

- Fail2Ban (WP #1658) only needs `obiectl report`; a node that is down
  gives a clear error and a non-zero exit code, never a hang.
- Coalesced counts are lost if obied restarts inside a window; the verdict
  itself is durable in the store.
- A client in the socket group can publish verdicts under the node's key;
  that is the intended trust boundary.
