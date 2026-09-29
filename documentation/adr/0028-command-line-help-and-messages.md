# ADR 0028: Command-line help, error messages, completion and manual pages

- **Status:** Accepted
- **Date:** 2026-09-29
- **Work package:** [#1694](https://openproject.niew.dev/work_packages/1694)

## Context

`obied` and `obiectl` explain themselves unevenly. Most commands have a
summary and Go's flag list, but no purpose statement, no examples, and
defaults only where a flag has a non-zero one. `obiectl` without a command
prints an alphabetical list; `obied` without arguments starts a node with
the default configuration. Errors range from complete advice to a bare
`open …: permission denied`. There is no shell completion and no manual
page. The epic #1681 wants a newcomer to get unstuck without the
documentation or the source code, and nobody to hit an error that does
not say what to do next.

## Decision

- **One registry per tool, standard library only.** Each command is
  described once: name, task group (*look*, *decide*, *report*,
  *manage*), a one-line summary, its synopsis, a description and
  realistic examples. The flags stay where the command defines them with
  the standard `flag` package; the renderers read them from the command's
  own flag set by running it with `--help` against a recorder. Help,
  the overview, the manual pages, the shell completion and the generated
  CLI reference all come from this registry, so they cannot disagree. No
  CLI framework is added.
- **Help is output, a mistake is an error.** `--help`, `-h` and
  `help <command>` print the full help to standard output and exit 0.
  Every flag is listed with its default. A usage error prints one line
  saying what is wrong and where the help is, to standard error, exit 2.
  An unknown command suggests the closest known one. Without a command,
  both tools print their commands grouped by task and where to start.
- **Bare `obied` shows the overview.** It no longer starts a node with
  the default configuration. `obied run` is the explicit command, and
  `obied --config <file>` keeps running the node, as the systemd unit and
  the container image call it; both are unchanged.
- **What, why, next.** An error a user can realistically hit is a
  `problem`: what went wrong (first line, prefixed with the program), why
  when it is known, and one or more next steps, each on its own `Why:` or
  `Next:` line, like the self-check's findings. Every problem has a
  stable ID, and `documentation/operations/messages.md` lists every ID
  with its message and next steps. A test compares the IDs in the code
  with the inventory in both directions, so a new message cannot ship
  without an entry and a stale entry cannot stay.
- **Checks before requests.** `obiectl` checks addresses, networks and the
  required report fields before it asks the node, so these mistakes are
  usage errors that name the flag, not refusals of the admin API.
- **Output for people and for programs.** Dates are RFC 3339 in UTC,
  durations use days where they are long, and there is no colour: labels
  are words. Every listing command has `--json`. Long listings print a
  summary first and at most `--limit` rows (100 by default), blocks
  first; `--limit 0` and `--json` give everything. `obied setup` asks its
  questions only on a terminal; otherwise it points to
  `--non-interactive`, so piped output never contains a prompt.
- **Completion and manual pages ship with the release.** Both tools
  print a completion script for bash, zsh and fish (`completion <shell>`).
  `packaging/gendocs` writes the scripts and the manual pages `obied(1)`
  and `obiectl(1)` from the registry; `release.sh` puts them under
  `share/` in every tarball and `install.sh` installs them below
  `$PREFIX/share`. The Markdown CLI reference is generated the same way
  and a test keeps it current.

## Consequences

- A new command cannot ship without a summary, a group, at least one
  example that parses, and a documented default for every flag:
  `TestEveryCommandIsDocumented` fails otherwise. A new error message
  needs an ID and a row in the message inventory.
- Scripts that relied on `obied` without arguments starting a node must
  use `obied run` or pass `--config`. Help text moved from standard error
  to standard output.
- The listing tables of `decisions` and `enforced` are no longer complete
  by default; scripts use `--json`, which is.
- Completion is static: it completes commands, flags and fixed flag
  values, not addresses or peer IDs from the running node.
