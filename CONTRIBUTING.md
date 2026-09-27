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
- Write table-driven unit tests; add [fuzz tests](#fuzz-testing) for every
  decoder. Tests that need root (e.g. nftables) go behind the `privileged`
  build tag.
- Handle every error explicitly; do not silence linters without a comment
  explaining why.
- Architecture changes need a new ADR in `documentation/adr/` and an update to
  `ARCHITECTURE.md`.
- A new configuration key needs a struct field and default in
  `internal/config`, a validation rule with tests where applicable, and a
  commented entry in `documentation/examples/obie.yaml` (a test enforces it).
- Log only through component loggers from `internal/logging`, never through
  `slog.Default()`.

## Fuzz testing

### What it is

Fuzz testing ("fuzzing") means feeding a function huge numbers of random and
malformed inputs and checking that it never crashes and never breaks a
guarantee it promises. The fuzzer starts from a few example inputs, mutates
them (flips bits, cuts, splices, inserts odd bytes) and keeps every mutation
that reaches new code, so it explores inputs nobody would think to write by
hand. The name goes back to a 1988 University of Wisconsin experiment that
fed random "fuzz" — noise from a dial-up line — to Unix utilities and watched
many of them crash.

### Why OBIE needs it

An OBIE node reads messages from peers it does not control, and any of them
may be buggy or hostile. Every piece of code that parses outside input —
event decoders, signature verification, canonicalization, configuration and
admin-API parsing — must therefore hold up against arbitrary bytes: it must
never panic (a panic takes the node down) and never accept data that is
inconsistent, such as an event that re-encodes to something different from
what was signed. Hand-written tests only cover the cases we thought of;
fuzzing looks for the ones we did not.

### Writing a fuzz test

Go's built-in fuzzing (`go test -fuzz`) dictates the shape: a fuzz function
lives in a `_test.go` file, is named `FuzzXxx` and takes `f *testing.F`. It
adds seed inputs with `f.Add` and passes the property check to `f.Fuzz`:

```go
// FuzzDecode checks that decoding arbitrary input never panics, and that
// every accepted event re-encodes to input that decodes to the same event.
func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{}`)) // seed inputs: valid and edge-case examples
	f.Fuzz(func(t *testing.T, data []byte) {
		// call the code under test with data; t.Fatal if a property breaks
	})
}
```

Every fuzz function needs a doc comment stating the property it checks
("never panics", "round-trips", "is idempotent", …) — without it a reviewer
cannot tell what a failure means. Seed with realistic valid inputs plus the
edge cases you already know about; the fuzzer mutates from there.

### Running a fuzz target

`-fuzz` runs one target in one package at a time; `-run='^$'` skips the
normal tests:

```sh
go test -run='^$' -fuzz='^FuzzVerify$' -fuzztime=30s ./pkg/obieproto
```

Without `-fuzztime` the run continues until you press Ctrl+C or it finds a
failure. List the targets with `grep -rn '^func Fuzz' --include='*_test.go' .`.

Inputs come from three places:

- **Seed corpus** — the `f.Add` calls plus any files committed under
  `testdata/fuzz/FuzzXxx/` next to the test.
- **Generated corpus** — interesting inputs the fuzzer found, kept in the Go
  build cache (`$(go env GOCACHE)/fuzz`) so the next run continues from them.
  It is never committed.
- **Crash files** — when an input breaks the property, Go writes it to
  `testdata/fuzz/FuzzXxx/<hash>` and prints the command to replay it, e.g.
  `go test -run=FuzzVerify/<hash> ./pkg/obieproto`.

A crash file is a ready-made regression test: because files in
`testdata/fuzz/` belong to the seed corpus, every later `go test` runs it.
Fix the bug, check that the replay command passes, and **commit the crash
file together with the fix** so the bug can never return unnoticed.

### Resource use

While fuzzing, Go starts one worker process per CPU core (they show up in
`ps` as `-test.fuzzworker`) and keeps every core busy until the run ends. On
shared machines limit the workers with `-parallel=N` or `GOMAXPROCS=N`:

```sh
go test -run='^$' -fuzz='^FuzzVerify$' -fuzztime=30s -parallel=2 ./pkg/obieproto
```

### Fuzzing and `make ci`

Every plain `go test` — and therefore `make test` and `make ci` — runs each
fuzz function once per seed-corpus input, like an ordinary table-driven test,
without generating new inputs. That keeps the gate fast and turns every
committed crash file into a permanent check. Long fuzz runs that mutate
inputs are opt-in: run them manually as shown above when you change a
decoder or parser. A `make fuzz` target for them follows with the hardening
work package (WP-1667).

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
