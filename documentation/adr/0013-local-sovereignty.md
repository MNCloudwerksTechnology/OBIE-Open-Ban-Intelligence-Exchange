# ADR 0013: Local sovereignty — allow-list, overrides, modes and reload

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1660](https://openproject.niew.dev/work_packages/1660)

## Context

ADR 0001 fixes that the allow-list always wins and that a node starts in
`observe` mode. ADR 0008 stores operator overrides, ADR 0011 decides per
indicator but leaves the allow-list and the overrides unapplied. The operator
must always have the last word: their own infrastructure is never blocked,
nothing is enforced until they opt in, and they can overrule the mesh for
any address — without restarting the node for every change.

## Decision

- **Package:** `internal/sovereignty` holds the allow-list and the rule that
  combines it with the overrides. It is pure: it reads files and resolves
  names only when an allow-list is built, never while judging.
- **Effective allow-list** = built-in ∪ `allowlist.cidrs` ∪ this node's own
  addresses ∪ the IPs of the `mesh.bootstrap` peers ∪ `allowlist.files`.
  - *Built-in:* loopback (`127.0.0.0/8`, `::1/128`), RFC 1918, CGNAT
    (`100.64.0.0/10`), link-local (`169.254.0.0/16`, `fe80::/10`), ULA
    (`fc00::/7`), multicast (`224.0.0.0/4`, `ff00::/8`), unspecified /
    "this network" (`0.0.0.0/8`, `::/128`), limited broadcast, and the
    documentation ranges (`192.0.2.0/24`, `198.51.100.0/24`,
    `203.0.113.0/24`, `2001:db8::/32`, `3fff::/20`).
  - *Own addresses:* the IPs in `mesh.listen`; for an unspecified listen
    address (`0.0.0.0`, `::`) every interface address of that family.
  - *Bootstrap peers:* the IPs in their multiaddrs; `/dns*` names are
    resolved when the allow-list is built (at start and on every reload,
    5 s timeout). A name that does not resolve is logged and skipped, it
    does not stop the node.
  - *Files:* `allowlist.files` (absolute paths), one IP or CIDR per line,
    blank lines and `#` comments allowed. A missing file or an invalid line
    (reported with file and line) makes the allow-list invalid: `obied`
    does not start, `--check-config` fails, a reload is rejected.
  - An indicator is allow-listed if its range **overlaps** an entry: an
    address inside it, a CIDR inside it, or a CIDR containing it — blocking
    a range that contains an allow-listed address would block that address.
- **Overrides** are the store's `force_allow` / `force_block` records
  (ADR 0008), set with an optional TTL and note. A `force_allow` covers
  everything its range overlaps, like an allow-list entry; a `force_block`
  applies to exactly its indicator and needs no verdicts.
- **Precedence** (first match wins):
  1. `force_allow` override → `allowed`;
  2. *protected* allow-list (built-in, own addresses, bootstrap peers) →
     `allowed` — a `force_block` can never block the node's own
     infrastructure;
  3. `force_block` override → `block`;
  4. *operator* allow-list (`allowlist.cidrs`, `allowlist.files`) →
     `allowed` — a targeted `force_block` beats the operator's own broad
     allow-list entry;
  5. the trust-weighted decision of ADR 0011.
- **Decision state `allowed`** joins `block` and `none`: the verdicts are
  still shown with their score, but the indicator is never blocked. A
  `force_block` lasts until the override expires, capped at
  `decision.max_ttl` and refreshed like any capped block. The explanation's
  `sovereignty` section names the rule, its source (`builtin`, `self`,
  `bootstrap`, `config`, `file`, `override`), the matching range and the
  override's note and expiry.
- **Engine:** the engine keeps a decision for every indicator with active
  verdicts or a `force_block`. It reads the overrides from the store at
  start and after every override change or expiry; when a `force_allow`
  appears or disappears, every kept indicator overlapping it is
  re-evaluated.
- **Mode gate:** `internal/enforce.Gate` subscribes to the block change
  stream and is the only path to the enforcer. In `observe` (default) it
  logs every change ("not enforced") and forwards nothing; in `enforce` it
  forwards. Switching `observe → enforce` replays the current blocks as
  `added`, `enforce → observe` withdraws them as `removed` (cause `mode`).
  The mode cannot be changed through the admin API.
- **Admin API:** `GET /v1/overrides`, `POST /v1/overrides`
  (`{indicator, action, ttl, note}`), `DELETE /v1/overrides/{indicator}`;
  `obiectl allow|block <ip|cidr> [--ttl] [--note]`, `obiectl overrides`,
  `obiectl unoverride <ip|cidr>`. A `force_block` that the protected
  allow-list overrules is stored, but the response carries a warning.
  `GET /v1/status` reports the current mode; `obiectl status` prints it
  first and in capitals.
- **Reload on SIGHUP:** `obied` re-reads its configuration file and builds
  a new allow-list. If either is invalid, the error is logged and the
  running configuration is kept unchanged. Otherwise the trust weights,
  the decision settings, the allow-list and the mode take effect at once
  (every kept indicator is re-evaluated with cause `reload`); changes to
  any other key are logged as needing a restart.

## Consequences

- A fresh install never enforces, and no mesh consensus or operator
  `force_block` can block loopback, private, link-local, the node's own or
  its bootstrap peers' addresses.
- The operator's `allowlist.cidrs` / files are deliberately weaker than a
  `force_block`: the override is the more specific statement.
- Interface addresses and DNS names are sampled at start and on SIGHUP;
  after an address change the operator sends SIGHUP (or lists the address
  in `allowlist.cidrs`).
- ADR 0011's "changing trust or decision settings needs a restart" is
  superseded: SIGHUP applies them.
