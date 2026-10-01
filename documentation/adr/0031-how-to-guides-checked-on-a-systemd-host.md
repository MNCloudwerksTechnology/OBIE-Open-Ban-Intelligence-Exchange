# ADR 0031: How-to guides for everyday tasks, checked on a systemd host

- **Status:** Accepted
- **Date:** 2026-09-30
- **Work package:** [#1697](https://openproject.niew.dev/work_packages/1697)

## Context

Once a node runs, its operator has routine jobs: connect a friend's node,
review what would be blocked, find out why an address is blocked, lift a
false positive, block or report an address, withdraw a verdict, switch
between observe and enforce mode, stop trusting a peer, recover after a
lockout, back up the node's identity, upgrade and uninstall. The
reference pages (#1666) describe every command and setting, but a reader
has to put a task together from several of them. The epic (#1681)
requires that every command shown to users is checked automatically
against the current release, like the tutorial (ADR 0030). Tasks that
change the firewall need the way back before the change.

## Decision

- **One page per task, titled as the task.** Each guide is a page of its
  own in `documentation/guides/`, titled "How do I …?". It opens with
  one paragraph on what it achieves and when you need it, then has
  exactly these sections: *Before you start* (the prerequisites), *Steps*,
  *Check that it worked* and *Undo*. The index,
  `documentation/guides/README.md`, groups the guides by goal. The
  introduction, the tutorial and the README link it.
- **The firewall first.** A guide whose commands add or lift blocks, or
  remove OBIE's table, opens with a warning that says what changes in the
  firewall, and shows *Undo* before *Steps*. `make ci` finds these guides
  by their commands, so a new command of that kind cannot slip into a
  guide without the warning.
- **The console where it is easier.** Where the web console makes a task
  easier, the guide shows both ways. The command-line way is the one the
  check runs. The console way is a paragraph that starts with
  `**In the console:**`, names one console page, and sets in bold what
  that page shows, as in the sandbox walkthrough (ADR 0029).
- **The tutorial's rules for blocks.** The guides use the block rules of
  ADR 0030: only `sh` and `text` code blocks, a `text` block is the
  expected output of the one command before it, and `…` stands for any
  text. Settings change through commands (`sed`, `tee`, `obied setup`,
  `obiectl`), so that the check runs every change.
- **Prerequisites the check can build.** *Before you start* links the
  step of the tutorial that the node must have reached. The check starts
  a fresh server for each guide, runs the tutorial's commands up to and
  including that step, and then what the reader brings along. It runs
  the guide's *Steps*, *Check that it worked* and then *Undo*, whatever
  order the page shows them in, and compares every output. What the
  reader brings along is simulated as in ADR 0030, with the same
  addresses: the reader's SSH session, an attacker that Fail2Ban bans,
  and the friend's node at 198.51.100.20. A guide can add more: a customer
  or the reader's own second address that Fail2Ban bans by mistake, a
  verdict the friend publishes, a report by mistake. The index page says
  what is simulated.
- **`make guides-check` runs every guide.** It lives in the tutorial's
  test package (build tag `tutorial`), since it reuses the tutorial's
  server image, release build, simulated peer and attacker, terminal and
  output comparison. The check builds the release under the version the
  tutorial installs, and under the newer version the upgrade guide
  installs, from the same tree. `packaging/release.sh` refuses a final
  version that the capability overview does not describe; the check
  names its versions in `UNRELEASED_VERSION`, which lets the script build
  them all the same. Each guide runs on its own server, one
  after the other. CI runs the check in the packaging job, after the
  tutorial's. The console pages are read on the server with `curl`, as
  root, after signing in with the token of `obiectl console`.
- **`make ci` checks the form without Docker:** the title, the sections
  in order, a link to a step of the tutorial, at least one command with
  output in *Steps* or *Check that it worked*, the firewall warning and
  the early *Undo*, the console paragraphs, and that the index lists
  every guide once, grouped by goal, and every task of the work package.

## Consequences

- A change to `obiectl`'s or `obied`'s output, the console's pages, the
  installer or the unit that a guide shows fails CI until the guide shows
  the new behaviour.
- CI's packaging job takes several minutes longer: each guide starts a
  server and runs part of the tutorial first. The guides run one after the
  other, which keeps the load of a CI runner low.
- The guides fit a node installed and set up as in the tutorial. Commands
  that edit `/etc/obie/obie.yaml` with `sed` expect the layout that
  `obied setup` writes; each guide says what the command changes, so that
  a reader with another layout can make the change in an editor.
- As for the tutorial, only Ubuntu 24.04 on amd64 is checked, and the
  upgrade guide is checked between two versions built from the same tree,
  not between two real releases.
- The console way is checked by what its page shows, not by carrying the
  action out in a browser; the console's own tests cover the actions.
