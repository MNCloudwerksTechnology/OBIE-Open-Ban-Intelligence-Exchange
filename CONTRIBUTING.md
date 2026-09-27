# Contributing to OBIE

Thank you for helping build OBIE. This guide covers how to build and test the
code and what every change must satisfy. Read [ARCHITECTURE.md](ARCHITECTURE.md)
first — it is the binding technical baseline.

## Prerequisites

- Go 1.23 or newer
- GNU Make, Git
- Network access on the first run: `make lint` and `make vuln` install the
  pinned golangci-lint and govulncheck versions into `./bin/tools/`.

## Build and test

| Command          | What it does                                                   |
|------------------|----------------------------------------------------------------|
| `make build`     | Static (`CGO_ENABLED=0`) `obied` and `obiectl` into `./bin/`   |
| `make test`      | `go test -race -count=1 ./...`                                 |
| `make vet`       | `go vet ./...`                                                 |
| `make fmt-check` | Fails if any Go file is not `gofmt`-formatted                  |
| `make lint`      | golangci-lint with the committed `.golangci.yml`              |
| `make vuln`      | govulncheck against the Go vulnerability database              |
| `make ci`        | fmt-check + vet + lint + test + vuln                          |
| `make clean`     | Removes `./bin/` including installed tools                     |

The version embedded in the binaries comes from `git describe`; override it
with `make build VERSION=v0.1.0`. A plain `go build` reports `dev`.

**`make ci` must pass before you open a pull request.** The CI pipeline runs
the same checks.

## Code guidelines

- Follow the layout and conventions in [ARCHITECTURE.md](ARCHITECTURE.md);
  code lives under `internal/` unless it is part of the public protocol
  package `pkg/obieproto`.
- Write table-driven unit tests; add fuzz tests for every decoder. Tests that
  need root (e.g. nftables) go behind the `privileged` build tag.
- Handle every error explicitly; do not silence linters without a comment
  explaining why.
- Architecture changes need a new ADR in `documentation/adr/` and an update to
  `ARCHITECTURE.md`.
- A new configuration key needs a struct field and default in
  `internal/config`, a validation rule with tests where applicable, and a
  commented entry in `documentation/examples/obie.yaml` (a test enforces it).
- Log only through component loggers from `internal/logging`, never through
  `slog.Default()`.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```text
<type>(<scope>): <summary>

<optional body>
```

Common types: `feat`, `fix`, `docs`, `test`, `refactor`, `build`, `ci`,
`chore`. Reference the OpenProject work package where one exists, e.g.
`feat(config): load and validate YAML config (WP-1648)`.

## License

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE.md).
