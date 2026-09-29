# ADR 0030: A getting-started tutorial checked on a systemd host in a container

- **Status:** Accepted
- **Date:** 2026-09-30
- **Work package:** [#1696](https://openproject.niew.dev/work_packages/1696)

## Context

A first-time installer needs one tutorial that goes from nothing to a
working node on a real server. It covers the requirements, installation,
the setup assistant (ADR 0027), observe mode, the self-check, Fail2Ban,
the first verdict, a peer, what would be blocked, and optionally
enforcement. At every step they must know whether it worked. The quick
start (`documentation/operations/quickstart.md`) covered most of this.
Its commands were only mapped to the tests that exercise them, one by
one. The epic (#1681) requires that every command and output shown to
users is checked automatically against the current release. The sandbox
walkthrough does that for containers (ADR 0029). A real server adds
systemd, Fail2Ban, sshd and nftables. A newcomer also cannot make an
attacker or a peer appear on demand.

Trying the quick start's steps in a container with systemd showed why the
check matters. `fail2ban-client reload` does not add a new action to a
running jail (Fail2Ban 1.0.2 of Ubuntu 24.04). The quick start's way of
connecting Fail2Ban reported nothing, and no test noticed.

## Decision

- **One tutorial replaces the quick start.**
  `documentation/getting-started.md` is the linear path on a server with
  systemd, installed from the release archive. The container image has a
  section of its own, and so do a server without Fail2Ban and a node
  without a peer. `quickstart.md` becomes a short page that sends readers
  to the tutorial, because `obied setup` of earlier builds prints its
  address. The setup assistant, the self-check and the documentation link
  the tutorial.
- **File edits are commands.** The tutorial changes files with `tee` and
  `sed`, and changes the node's settings with `obied setup` and
  `obiectl`, so that every change it asks for is a command the check runs.
  A reader who prefers an editor learns what the command changes.
- **The page follows fixed rules, as in ADR 0029.** It has only `sh` and
  `text` code blocks. An `sh` block followed by a `text` block holds one
  command, and the text is its expected output. The commands of an `sh`
  block without output must succeed. In expected output, `…` stands for
  any text within a line, and a line of only `…` for any number of lines.
  Each numbered step starts with one sentence that says what the step is
  for. It shows at least one expected output and links to
  troubleshooting for when the result differs. Step 10 checks that the
  reader's own address is protected, and practises the way back, before
  the command that switches enforcement on.
- **`make tutorial-check` runs every command.** The test has the build
  tag `tutorial`, and CI runs it in the packaging job. It builds the
  release archive for linux/amd64 with `packaging/release.sh`, under the
  version the page installs. It builds a host image, Ubuntu 24.04 with
  systemd, Fail2Ban, OpenSSH, nftables, sudo and curl, and starts it with
  systemd as process 1. It then runs the page from top to bottom as the
  user `alice`, who may use `sudo`, like an administrator in an SSH
  session. What a reader brings along is simulated:
  - the SSH session: `SSH_CONNECTION` names 85.10.3.20, which the setup
    assistant and the self-check read;
  - the download: the release's address is replaced by a local copy
    served inside the container;
  - the attack: an address in a network namespace inside the container,
    85.10.0.7, fails five SSH logins, and the sshd jail bans it;
  - the peer: `obied` from the same release runs in a second network
    namespace, at 198.51.100.20;
  - the answers to `obied setup`: the test gives each prompt the answer
    the page shows, through a pseudo-terminal.

  The test compares every output with the page. First it replaces what
  differs from run to run: peer IDs, event IDs, fingerprints, times and
  durations. Commands that only read are repeated for up to 30 seconds,
  until they show what the page shows, since bans and peers take a
  moment. Commands that change something run once. The container
  section's `docker` commands run on the Docker host, against an image
  built from the same source. Their container, volume and port are
  replaced by the test's own. The test removes its containers, volumes
  and images at the end.
- **The host container is privileged.** systemd, the service's sandbox,
  nftables and network namespaces need it. The container has its own
  network namespace, so its Fail2Ban and nftables rules never reach the
  host's firewall. The check is for CI and developers; readers never run
  it.
- **`make ci` checks the form without Docker:** the steps in order, each
  with its purpose, expected output and troubleshooting link. It also
  checks the block kinds, that access is protected and the way back
  practised before the switch, the separate container section, the edge
  cases and the "What next" links.

## Consequences

- A change to the installer, the setup assistant, the self-check,
  `obiectl`'s output, the Fail2Ban action or the unit that the tutorial
  shows fails CI until the page shows the new behaviour.
- CI's packaging job takes about three minutes longer. The check needs
  Docker with privileged containers, and network access to build the host
  image.
- Only Ubuntu 24.04 on amd64 is checked. Other distributions, arm64 and
  a real internet connection are not. The page says so, and it says
  where a reader's output may differ.
- The first real verdict depends on a real attacker. The tutorial tells
  the reader how long that usually takes and what to do meanwhile. The
  check simulates the attack.
- The time a newcomer needs (at most 30 minutes) is measured in the
  usability validation (#1698), not by the check.
