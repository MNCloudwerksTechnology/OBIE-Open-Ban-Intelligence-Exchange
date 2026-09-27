# ADR 0003: Daemon lifecycle, ops endpoints and admin API

- **Status:** Accepted
- **Date:** 2026-09-27
- **Work package:** [#1649](https://openproject.niew.dev/work_packages/1649)

## Context

ADR 0001 fixes that `obied` exposes Prometheus metrics and health endpoints on
a separate listen address and that `obiectl` talks to it over HTTP/JSON on a
Unix socket. Every later subsystem (store, mesh, decision, enforcer) must plug
into one process that starts, reports readiness and shuts down predictably, so
the start/stop contract has to exist before those subsystems do.

## Decision

- **Lifecycle:** `internal/lifecycle` defines a `Subsystem` (`Name`, `Start`,
  `Stop`, both taking a `context.Context`) and a `Manager`. Subsystems start in
  registration order, each under its own start timeout, and stop in reverse
  order under one shared shutdown deadline (`node.shutdown_timeout`, default
  10s). When a subsystem fails to start, the manager stops the already started
  ones and the daemon exits non-zero; a subsystem whose `Start` fails must
  release what it acquired itself. A subsystem may implement `Ready() error`
  to report readiness beyond "started". The manager's status snapshot feeds
  both `/readyz` and the admin API.
- **Signals:** SIGTERM and SIGINT trigger the graceful shutdown (exit 0 when
  every subsystem stopped in time). A second signal falls back to Go's default
  handling and terminates the process immediately.
- **Ops server** (`internal/ops`, on `metrics.listen`): `/healthz` answers 200
  while the process serves HTTP; `/readyz` answers 200 when all subsystems are
  ready and 503 with a JSON body listing the ones that are not; `/metrics`
  serves the Prometheus default registry.
- **Metrics library:** `github.com/prometheus/client_golang` — the reference
  Prometheus client, whose default registry later work packages register
  their collectors with. Pinned to the newest release that still supports the
  module's Go 1.23 baseline.
- **Admin API** (`internal/admin`): HTTP/JSON on the Unix socket
  `admin.socket`, versioned under `/v1/`. At start a stale socket file is
  removed — but only if nothing answers on it, so a second `obied` refuses to
  start instead of hijacking a running node's socket, and a non-socket file is
  never deleted. The socket is chmod'ed to 0660 and chgrp'ed to
  `admin.socket_group` if that group exists (a warning is logged otherwise;
  failing to chgrp an existing group is a start failure, because the socket
  would otherwise be accessible to the wrong group). The same package holds
  the client used by `obiectl`, so the wire types exist exactly once.
- **CLI:** `obiectl` stays on the standard library `flag` package with a small
  hand-written subcommand dispatcher (`obiectl [--socket path] status
  [--json]`) — no CLI framework dependency.

## Consequences

- New subsystems only implement `Subsystem` and are registered in
  `internal/daemon`; readiness and status reporting come for free.
- Admin API types are internal; they may change together with `obiectl`
  until a later ADR declares them stable.
- `client_golang` pulls in `prometheus/common`, `procfs` and protobuf;
  govulncheck in `make ci` covers them.
