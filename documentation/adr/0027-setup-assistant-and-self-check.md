# ADR 0027: First-run setup assistant and node self-check

- **Status:** Accepted
- **Date:** 2026-09-29
- **Work package:** [#1693](https://openproject.niew.dev/work_packages/1693)

## Context

A first-time installer gets the annotated example configuration, 250
lines that explain every key, and has to work out which few of them
matter before the first start. When something is wrong afterwards, the
hints are spread over `obied --check-config`, `obiectl status`, the log,
`nft`, Fail2Ban's log and the troubleshooting page. The epic #1681 wants a
newcomer to reach a running, observe-only node that reports Fail2Ban bans
without help, and nobody to lock themselves out on the way.

## Decision

- **Two offline-style commands of `obied`:** `obied setup` writes the
  configuration, `obied self-check` checks the node and the host. They sit
  next to `keygen` and `identity` because they work on what `obied` owns —
  its configuration, state directory and identity — and must work before
  the node ever ran. `self-check` also asks a running node over the admin
  socket, with the client `obiectl` uses. Both stay on the standard
  library: plain line prompts, no terminal UI library.
- **One rendering path.** The assistant collects `Answers` (state
  directory, audit log, peers with name and trust weight, mode, allow-list
  entries). Interactively each question is explained and offers a safe
  default; `--non-interactive` takes the same answers from flags. Both
  render the file through the same function, so the same answers give a
  byte-identical file. The file sets only the answered keys, with comments,
  and points to `obie.yaml.example` and the reference for every other key.
  It is parsed with `config.Parse` before it is written, so the assistant
  cannot write a configuration `obied` rejects.
- **Never replace without consent.** An existing file is replaced only
  after the operator agreed (interactively; `--force` up front). The old
  file is kept as `<file>.bak` (`.bak.1`, … if taken) through a hard link,
  and the new one is renamed into place, so the path never disappears. The
  file gets mode 0640 and the group `obie`, as `install.sh` installs it.
  The assistant checks that it may write the directory before it asks
  anything.
- **The operator's session address.** `internal/session` reads
  `SSH_CONNECTION` (or `SSH_CLIENT`) from the process, and else from its
  ancestors' `/proc/<pid>/environ`, because `sudo` and `su` drop the
  variable from the environment but not from their own. The assistant
  offers the address as the default allow-list entry; the self-check
  reports whether it is protected. A running node answers that through
  `explain`, which includes overrides; otherwise the allow-list is built
  from the configuration as the node would build it.
- **Self-check report.** Nine checks in a fixed order, each with a stable
  ID (`config`, `identity`, `admin`, `node`, `peers`, `clock`,
  `fail2ban`, `firewall`, `session`) and a status `ok`, `warning` or
  `problem`; every warning and problem carries the next step. A check that
  cannot see something (not root, node not running) says so as a warning
  instead of guessing. `--json` prints the same report. The exit status is
  0 without problems, 1 with at least one, 2 on a usage error and 3 when
  the report cannot be written. An unprotected session address is printed
  as a banner, a problem in enforce mode and a warning in observe mode.
- **Before the first start.** A node that is not running is a warning
  while its state directory has no `FORMAT` file — `obied` stamps it at
  its first start — and a problem afterwards. A missing identity is a
  warning before the first start and a problem after it, because the next
  start would create a new peer ID.
- **Read-only host probes, no new dependencies.** The clock's sync state
  comes from `adjtimex(2)` (what `timedatectl` shows as "NTP
  synchronized"); nftables is probed by listing the tables over netlink;
  Fail2Ban by `fail2ban-client -d` (which jails use the `obie` action),
  the node's own active verdicts and the last `obie-fail2ban` journal
  message; peers by a TCP connection to their address and the running
  node's peer list. The self-check changes nothing and never prints the
  node key.

## Consequences

- The configuration written by the assistant is short and readable, but
  a key it does not set is only described in `obie.yaml.example` and the
  configuration reference.
- The self-check needs root for a complete result: the configuration,
  the state directory and the admin socket are closed to other users by
  design.
- Adding a check means a new ID, a row in the setup and self-check guide
  and a test; the IDs and statuses are part of the JSON output that
  scripts use.
- Probes of Fail2Ban and the journal depend on those tools' output; when
  they are missing or fail, the check says so and asks for the manual
  step instead of failing.
