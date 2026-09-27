# ADR 0002: Configuration loading and structured logging

- **Status:** Accepted
- **Date:** 2026-09-27
- **Work package:** [#1648](https://openproject.niew.dev/work_packages/1648)

## Context

ADR 0001 fixes that a node is configured by one strictly validated YAML file
and logs with `log/slog` as JSON. Operators must learn about every mistake at
startup with a message that names the offending key, and later work packages
(mesh, decision, enforcement, metrics, audit) need one typed, already
validated configuration to consume.

Two pieces need third-party code: parsing YAML, and validating libp2p
multiaddrs including the `/p2p/<peer-id>` component.

## Decision

- **YAML parser:** `go.yaml.in/yaml/v3`, the maintained continuation of
  `gopkg.in/yaml.v3` by the YAML organisation. The file is parsed into a node
  tree and decoded by our own walker in `internal/config`, which tracks the key
  path (e.g. `trust.publishers[1].weight`). Unknown keys, duplicate keys and
  type mismatches (strings are only accepted from YAML strings, integers only
  from YAML integers) are errors naming that path.
- **Multiaddr validation:** `github.com/multiformats/go-multiaddr`, the
  library go-libp2p itself uses, so the mesh accepts exactly what
  configuration validation accepted. Peer IDs are validated through its `p2p`
  component codec; we do not pull in go-libp2p for configuration alone.
- **Error reporting:** decoding and validation collect *all* problems and
  report them together, one line per problem in the form `<key path>: <message>`,
  so an operator fixes a file in one round trip.
- **Durations** accept Go duration syntax (`10s`, `1h30m`) plus a whole-day
  suffix (`7d`), because TTLs are naturally expressed in days.
- **`obied --check-config`** loads and validates the file and exits 0 (valid)
  or 1 (invalid or unreadable) without starting anything.
- **Logging:** `internal/logging` builds one `slog` JSON handler per process
  with the configured level (`debug`, `info`, `warn`, `error`). Loggers are
  only handed out per component (`Factory.Logger("mesh")`), so every log line
  carries a `component` attribute.

## Consequences

- The configuration schema lives in Go struct tags; the example file
  `documentation/examples/obie.yaml` is tested against it and cannot drift.
- Adding a configuration key requires a struct field, a default, a validation
  rule where applicable and an entry in the example file.
- Error messages refer to YAML key paths, never to Go type names.
- Code must not log through `slog.Default()` directly; it receives a
  component logger from the factory.
