# ADR 0015: nftables enforcement backend in its own table

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1662](https://openproject.niew.dev/work_packages/1662)

## Context

ADR 0014 reconciles the decided blocks into a pluggable enforcer; so far
only `dryrun` exists. Real enforcement on Linux must coexist with any
firewall the operator already runs (nftables, iptables-nft, firewalld,
Docker), must survive a crashed or restarted `obied` without leftovers,
must apply 100k entries in under 5 s and must need nothing beyond
CAP_NET_ADMIN.

## Decision

- **Netlink, no `nft` binary.** Package `internal/enforce/nft` talks to
  the kernel through `github.com/google/nftables` (with
  `github.com/mdlayher/netlink`). Linux only; other platforms build a stub
  whose every call fails.
- **One table, nothing else touched.** Table `inet obie` with sets
  `obie_v4` (`ipv4_addr`) and `obie_v6` (`ipv6_addr`), both
  `flags interval,timeout`, and a chain `input` (type filter, hook input,
  priority -10, policy accept) with `ip saddr @obie_v4 counter drop` and
  `ip6 saddr @obie_v6 counter drop`. `enforce.nftables.forward: true` adds
  the same rules in a chain `forward`. Priority -10 runs before the
  usual filter chains at 0; an accept there cannot let a blocked source
  through, since a drop in any base chain is final.
- **Setup** reuses the table if its sets, chains and rules are exactly as
  above, and otherwise deletes and recreates it in one transaction (its
  entries return with the same reconciliation pass). `List` checks the
  structure too and fails with `ErrDrift` if the table is missing or was
  changed; the reconciler now sets the backend up again after any failed
  pass, so a deleted or flushed table heals with the next retry (≈1 s).
- **Elements.** A prefix is the interval `[first, last+1)`: a start
  element with the remaining timeout (rounded up to milliseconds, never 0,
  which would mean "no timeout") and the prefix as its comment, and an
  interval-end element without a timeout (the kernel refuses one and
  removes the end with its expired start). A range reaching the top of the
  address space has no end element. `List` reads the prefix from the
  comment, because the kernel keeps an expired range's end element until
  the next change of the set, which makes pairing starts and ends
  ambiguous; entries with less than `enforce.MinTimeout` left are not
  listed, so they are never deleted just as they expire.
- **No overlaps.** Interval sets reject overlapping ranges, so the
  reconciler leaves out a prefix inside a wider desired one (for every
  backend). If the narrower block outlives the wider one, it is applied by
  the first pass after the wider one ended.
- **Apply** deletes, then adds, in one netlink transaction made of
  messages of at most 1000 elements. The socket send buffer is raised to
  hold the transaction (`SO_SNDBUFFORCE`, else `SO_SNDBUF` up to
  `net.core.wmem_max`); only if it still cannot, the change is split into
  several transactions, logged as a warning, and the next pass corrects
  any partial result.
- **Permissions.** Only CAP_NET_ADMIN is needed. A refusal by the kernel
  is reported as `ErrPermission`, naming the capability.
- **Teardown.** `obied teardown-firewall` deletes the table (a missing one
  is fine). With `--on-stop`, for the systemd unit's `ExecStopPost`, it
  only does so when `enforce.backend` is `nftables` and
  `enforce.nftables.teardown_on_stop` is true; the default `false` keeps
  the blocks across restarts until their timeout. Switching to `observe`
  still tears the table down (ADR 0014).
- **Tests.** Mapping and error paths are unit tests; kernel behaviour is
  covered by tests behind the `privileged` build tag, which re-execute
  themselves in a new network namespace (`unshare -rn`) and never touch
  the host's firewall.

## Consequences

- The operator's own ruleset is never modified; removing OBIE is
  `obied teardown-firewall`.
- Entries outlive `obied` until their timeout; a restart reconciles them.
- A block covered by a wider one is not listed by `obiectl enforced` while
  the wider one is applied.
- ADR 0014's "`nftables` follows in its own work package; until then
  `obied` refuses to start with it" is superseded.
