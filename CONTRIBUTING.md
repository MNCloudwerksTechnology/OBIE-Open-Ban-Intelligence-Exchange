# Contributing to OBIE

Thank you for helping build OBIE. This guide covers how to build and test the
code and what every change must satisfy. Read [ARCHITECTURE.md](ARCHITECTURE.md)
first — it is the binding technical baseline.

## Prerequisites

- Go 1.23 or newer
- GNU Make, Git
- Network access on the first run: `make lint`, `make lint-workflows` and
  `make vuln` install the pinned golangci-lint, actionlint and govulncheck
  versions into `./bin/tools/`. These tools need a newer Go than `go.mod`
  requires; with the default `GOTOOLCHAIN=auto` the `go` command downloads it
  automatically (do not set `GOTOOLCHAIN=local` on an older Go).

## Build and test

| Command                | What it does                                                     |
|------------------------|------------------------------------------------------------------|
| `make build`           | Static (`CGO_ENABLED=0`) `obied` and `obiectl` into `./bin/`     |
| `make test`            | `go test -race -count=1 ./...`                                   |
| `make vet`             | `go vet ./...`                                                   |
| `make fmt-check`       | Fails if any Go file is not `gofmt`-formatted                    |
| `make lint`            | golangci-lint with the committed `.golangci.yml`                 |
| `make vuln`            | govulncheck against the Go vulnerability database                |
| `make lint-workflows`  | actionlint on the CI workflows; checks both copies are identical |
| `make test-privileged` | Tests including the `privileged` build tag (needs root)          |
| `make ci`              | fmt-check + vet + lint + lint-workflows + test + vuln            |
| `make clean`           | Removes `./bin/` including installed tools                       |

The version embedded in the binaries comes from `git describe`; override it
with `make build VERSION=v0.1.0`. A plain `go build` reports `dev`.

**`make ci` must pass before you open a pull request.** The CI pipeline runs
the same checks.

## Continuous integration

Every pull request and every push to `develop` or `main` runs the CI pipeline
defined in `.gitea/workflows/ci.yml` (Gitea Actions). `.github/workflows/ci.yml`
is a byte-identical copy for the public GitHub mirror — change both together;
`make lint-workflows` fails if they differ. The pipeline has these jobs:

- **make ci** — the exact local gate, on `ubuntu-latest` with the Go version
  from `go.mod`.
- **build linux/amd64, linux/arm64** — static `obied` and `obiectl` binaries,
  uploaded as build artifacts.
- **privileged tests** — never part of the default run. Start it manually
  ("Run workflow" with the `privileged` input checked) to run the tests behind
  the `privileged` build tag as root.

**A red pipeline blocks merge.** A pull request is only merged when every job
of its latest pipeline run is green; fix the failure (or the check) rather
than bypassing it. The pipeline only runs `make` targets, so every red check
can be reproduced locally with the same command — the local `make ci` is the
source of truth.

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
