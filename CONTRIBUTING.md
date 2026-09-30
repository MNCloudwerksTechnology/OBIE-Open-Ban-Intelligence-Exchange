# Contributing to OBIE

Thank you for helping build OBIE. This guide covers how to build and test the
code and what every change must satisfy. Read [ARCHITECTURE.md](ARCHITECTURE.md)
first — it is the binding technical baseline.

## Prerequisites

- Go 1.26 or newer
- GNU Make, Git
- Node.js with npm (for `make lint-md` only)
- Network access on the first run: `make lint`, `make lint-workflows`,
  `make lint-md` and `make vuln` install the pinned golangci-lint,
  actionlint, markdownlint-cli2 and govulncheck versions into
  `./bin/tools/`. The Go tools need a newer Go than `go.mod`
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
| `make lint-md`         | markdownlint-cli2 on every Markdown file (`.markdownlint-cli2.yaml`) |
| `make test-privileged` | Tests including the `privileged` build tag (needs root)          |
| `make fuzz`            | Every fuzz target for `FUZZTIME` each (default `30s`)            |
| `make soak`            | The soak test: 3 nodes, 50 events/s for 30 min (not in CI)       |
| `make sim-trust`       | The trust simulation's `SCENARIO` (default `reduced`, as in CI)  |
| `make ci`              | fmt-check + vet + lint + lint-workflows + lint-md + test + vuln  |
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
- **fuzz** — `make fuzz FUZZTIME=30s`: every fuzz target mutates inputs
  for 30 s (see [Fuzz testing](#fuzz-testing)).
- **release build, image and lab** — `make release` twice (the
  `SHA256SUMS` must match: the build is reproducible), `make check-unit`
  (`systemd-analyze verify` and an exposure of at most 3.0 for the systemd
  unit), `make image`, `make lab-smoke` (the three-node compose lab comes
  up, the nodes see each other and block by consensus) and
  `make sandbox-check` (every step of the
  [sandbox walkthrough](documentation/sandbox.md) runs and prints what the
  page shows; a change to `obiectl`'s output or the console that the page
  shows needs the page updated) and `make tutorial-check` (every command of
  the [getting-started tutorial](documentation/getting-started.md) runs on
  a systemd host in a privileged container, against the release built from
  the change, and prints what the page shows). Nothing is pushed.
- **trust simulation (reduced)** — `make sim-trust SCENARIO=reduced`: the
  reduced scenario of the [trust simulation](#trust-simulation), 20 seeds,
  checked against what v0.1's static weights guarantee; the report is
  uploaded as an artifact.
- **website** — `make -C website ci` for the website in `website/` (see
  [`website/README.md`](website/README.md)) and `make -C website smoke`
  (builds the website image and checks it in its production compose stack;
  nothing is pushed); its steps are skipped when nothing under `website/`
  changed.
- **privileged tests** — never part of the default run. Start it manually
  ("Run workflow" with the `privileged` input checked) to run the tests behind
  the `privileged` build tag as root.

`.gitea/workflows/release.yml` (again with a byte-identical GitHub copy)
runs only when a `v*` tag is pushed: it runs `make ci`, builds the release,
pushes the multi-arch image and then attaches the tarballs, SBOMs and
`SHA256SUMS` to the forge's release. How to release is described under
[Releasing](#releasing).

`.gitea/workflows/website-release.yml` (byte-identical GitHub copy) runs
only when a `website-v*` tag is pushed: it runs `make -C website ci` and the
smoke test, then pushes the website's multi-arch image
(`ghcr.io/<owner>/obie-website:<version>` on GitHub). The two release
workflows never trigger on each other's tags; see
[ADR 0018](documentation/adr/0018-website-container-and-deployment.md) and
[`website/deploy/README.md`](website/deploy/README.md).

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
- A new command of `obied` or `obiectl` is added to its tool's command
  registry (`internal/cli/ctl_commands.go`, `internal/cli/daemon_commands.go`)
  with its task group, a one-line summary, a description and at least one
  realistic example; every flag's usage text says what it does, and every
  listing command has `--json`. `TestEveryCommandIsDocumented` fails
  otherwise. Regenerate the [command-line reference](documentation/operations/cli.md)
  with `go run ./packaging/gendocs -reference documentation/operations/cli.md`
  (`TestReferenceIsCurrent`); the manual pages and shell completions are
  generated from the same registry at release time.
- A message for people follows the rules in
  [Messages of obied and obiectl](documentation/operations/messages.md):
  an error of a command is a `problem` with an ID that says what went wrong,
  why when known, and what to do next; a warning or error in the node's log
  names the next step in a `next` attribute when the operator must act.
  Each gets a row in that inventory (`TestMessageInventory`). Output uses
  the glossary's words, RFC 3339 times in UTC and no colour.

## Privileged tests

Tests that need the kernel — the nftables backend in
`internal/enforce/nft` and the nftables variant of the end-to-end test in
`test/e2e` — carry the build tag `privileged`
(`//go:build privileged`), so `make test` and `make ci` never build them.
They cover setup, applying and removing IPv4/IPv6 addresses and CIDRs,
reading them back over netlink, kernel expiry, dropped traffic, leaving an
unrelated table untouched, and applying 100k entries in under 5 seconds.
The end-to-end variant runs report → block under quorum → revoke with every
node programming the firewall of its own network namespace, and checks
that connections from the blocked address are dropped and pass again.

The tests never touch your host's firewall: their `TestMain` re-executes
the test binary in a fresh network namespace with `unshare -rn` (an
unprivileged user namespace), or `unshare -n` when that is not allowed and
you are root. If neither works, they are skipped.

```sh
make test-privileged                                   # every package, with -race
go test -tags privileged -count=1 -v ./internal/enforce/nft
go test -tags privileged -count=1 -v -run NFTables ./test/e2e
go test -tags privileged -run='^$' -bench=Apply100k ./internal/enforce/nft
```

`unshare -rn` needs unprivileged user namespaces
(`sysctl kernel.unprivileged_userns_clone=1` on Debian/Ubuntu kernels;
Ubuntu 24.04+ also restricts them through AppArmor —
`sysctl kernel.apparmor_restrict_unprivileged_userns=0` or run with
`sudo`). The CI job "privileged tests" runs `make test-privileged` as root
on a manual run.

## End-to-end test

`test/e2e` proves the whole path from a report to a block: four complete
`obied` nodes in one process (A, B and C trust each other, D is unknown),
each with its own configuration file, store, admin socket and libp2p host
on `127.0.0.1`. It is an ordinary test, part of `make test` and `make ci`,
needs no root and takes about 11 seconds:

```sh
go test -race -count=1 -v ./test/e2e
```

The scenarios build on each other and assert 5 s for propagation and 10 s
for enforcement. When one fails, the test prints the end of every node's
log and keeps the nodes' directories (`$TMPDIR/obie-e2e-*`) for
inspection. Keep it free of sleeps: wait with the polling helpers
`within` (until a condition holds, with a deadline) and `holds` (a
condition keeps holding for a while). Test-only switches go into
`daemon.Options.Testing`, never into the configuration (ADR 0016).

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
committed crash file into a permanent check. Runs that mutate inputs are a
separate target:

```sh
make fuzz                 # every target for 30 s, like the CI job "fuzz"
make fuzz FUZZTIME=10m    # before a release, or after changing a parser
```

`make fuzz` finds every `func Fuzz…` in a `_test.go` file, so a new target
is picked up without further wiring. The targets cover all input a node
takes from outside: event decoding and validation (`FuzzDecode`),
canonicalization (`FuzzTransform`), signature verification (`FuzzVerify`),
the configuration file (`FuzzParse`), admin API requests (`FuzzRequests`)
and the allow-list (`FuzzParseEntry`, `FuzzParseFile`). Before a release
each runs for at least 10 minutes; see
[`documentation/operations/performance.md`](documentation/operations/performance.md).

## Soak test

`make soak` runs `TestSoak` in `test/e2e` (build tag `soak`, so never part
of `make test`): three nodes that trust each other receive 50 unique
reports per second, spread evenly, for 30 minutes. The verdicts live 2
minutes, so each node settles at a steady working set, and
`mesh.rate_limit` is raised above its defaults (10 events/s per
publisher), which a node publishing 50/3 events per second would exceed.
It fails if an event does not reach every other node, if the 99th
percentile of the propagation time is 2 s or more, if the live heap —
without Badger's block and index caches, which fill up to their configured
size — grows by more than 20 % from the window after warm-up (the first
sixth of the run) to the last window, if the goroutines grow, or if a
goroutine outlives the nodes (goleak in `TestMain`). Shorter or heavier
runs (below about 15 minutes the working set is still filling, and the
memory check fails):

```sh
make soak SOAKTIME=15m SOAKRATE=100
```

Record the results of a full run in
[`documentation/operations/performance.md`](documentation/operations/performance.md).

## Trust simulation

`make sim-trust SCENARIO=…` runs `TestSimTrust` in `test/simtrust` (build
tag `simtrust`, so never part of `make test`): it replays a trace of
Fail2Ban bans and the verdicts of honest and adversarial publishers through
the real store, allow-list and decision engine of one node in virtual time,
and writes a report of every metric with its 95 % confidence interval over
`SEEDS` seeds (default 20) to `OUT` (default `bin/sim-trust/<scenario>`).
[ADR 0034](documentation/adr/0034-trust-simulation-by-trace-replay.md)
defines the world, the behavior models and the metrics. The scenarios:

- `reduced` (the default): a small world in about 20 seconds on four cores.
  It also checks what v0.1's static weights guarantee, so CI runs it.
- `baseline`: every model at 10, 20, 30 and 40 % in the default, lab and
  allow-list settings, the scenario of the v0.1 trust baseline.
- `honest`, `naive`, `careful`, `onoff`, `whitewash`, `sybil-1asn`,
  `sybil-masn`, `spies`, `suppressor`: one model of the baseline.

A work package that changes how the node weighs its publishers runs the
scenarios after its change and compares its report with the
[v0.1 trust baseline](documentation/validation/trust/README.md) (the
baseline scenario takes about 40 minutes on 32 cores):

```sh
make sim-trust SCENARIO=baseline OUT=/tmp/after
```

The baseline itself was written with `OUT=documentation/validation/trust`
at the commit its header names; write it there again only to replace it.

`TRACE=<file>` replays a recorded trace instead of the synthetic world.
`go run ./test/simtrust/cmd/trace-import` turns operators' Fail2Ban logs
into one, with every address pseudonymized; see ADR 0034 before you use
real logs.

## Releasing

Every release also updates what evaluators read before they install OBIE.
Before you tag:

1. Bring [What OBIE can and cannot do yet](documentation/capabilities.md)
   up to date: the version at the top, every status and plan, the
   requirements and the remaining risks. `make release` refuses to build a
   final version the page does not name
   (`TestReleaseRefusesStaleCapabilities`), and
   `TestCapabilitiesDescribeCurrentRelease` keeps the page on the release
   the README installs.
2. Measure again what the page's requirements rest on, and record the
   results in
   [performance.md](documentation/operations/performance.md): `make
   resources RESOURCESVERDICTS=10000,100000,1000000` (about 90 minutes)
   for a node's memory, processor and disk up to the verdicts it keeps by
   default, and `make fail2ban-versions` (needs Docker) for the Fail2Ban
   versions the action works with, besides the fuzzing and the soak test
   above.
3. Set the new version in the install commands of the README and the
   [getting-started tutorial](documentation/getting-started.md), and turn
   `[Unreleased]` in the [changelog](CHANGELOG.md) into the release.

Then tag the merged commit on `main`
(`git tag -a v0.1.0 -m "OBIE 0.1.0" && git push origin v0.1.0`); the
release workflow builds and publishes it (see
[ADR 0017](documentation/adr/0017-packaging-and-state-format.md)).

## Documentation

- Newcomers start at [What is OBIE?](documentation/introduction.md). Keep
  it in plain language: no commands, no configuration keys, no protocol
  details, short sentences and about five minutes of reading.
  `TestIntroductionIsPlainLanguage` in `test/docs` checks it. When the
  introduction changes substantially, repeat the
  [reader check](documentation/validation/introduction-reader-check.md)
  with a real reader without a networking background and record it there.
- Every guide links a glossary term to the
  [glossary](documentation/glossary.md) the first time it uses it. A new
  term gets an entry of one or two sentences, in alphabetical order; a new
  guide is added to `glossaryGuides` in `test/docs/glossary_test.go`.
  `TestGlossaryDefinesEveryTerm` checks the glossary, and
  `TestGuidesLinkGlossaryOnFirstUse` the first use of every term in
  `requiredTerms`.
- Diagrams have a text alternative and do not rely on colour alone.
- [What OBIE can and cannot do yet](documentation/capabilities.md) is the
  evaluator's overview. Update it with every change that adds, removes or
  switches on or off something it lists. `test/docs/capabilities_test.go`
  checks it: a status follows `config.Default()`, a plan links where it is
  planned, every threat of the threat model is summarised, and every
  sentence stays short.
- Relative links and their anchors must resolve
  (`TestRelativeLinksResolve`).

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
