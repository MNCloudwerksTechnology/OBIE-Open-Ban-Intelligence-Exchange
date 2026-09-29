# ADR 0029: A sandbox for newcomers and a walkthrough that runs as a test

- **Status:** Accepted
- **Date:** 2026-09-29
- **Work package:** [#1695](https://openproject.niew.dev/work_packages/1695)

## Context

A newcomer should watch OBIE work end to end on their own computer before
they let it near a firewall: an attack reported on one node, a block on
another once enough trusted nodes agree, an explanation, an override, a
revocation, and a node nobody trusts that cannot change anything. The
compose lab of ADR 0017 (`packaging/compose`) shows three nodes that
trust each other, but it has no untrusted node, no web console and no
guided story, and a node blocks its own reports at once (local
autoblock), so one revocation cannot lift a block everywhere. The epic
#1681 also requires that every command and output shown to users is
checked automatically, and that a missing container runtime or a taken
port ends with a message that says what to do.

## Decision

- **A sandbox next to the lab.** `packaging/sandbox` is its own Compose
  project: `node1`, `node2` and `node3` bootstrap to and trust each other
  with weight 1; the `stranger` connects to all three, and nobody lists it
  as a publisher (`trust.default_weight: 0`). The lab stays what ADR 0017
  made it, the smoke test of the image in a mesh.
- **A configuration that makes the story visible.** Every node runs in
  enforce mode with the `dryrun` backend. The trusted nodes block at a
  threshold of 1.5 with a quorum of 2, so one report at obiectl's default
  confidence (0.8) never blocks and two do, and they run with
  `decision.local_autoblock: false`, so a node's own report counts like any
  other publisher's and one revocation lifts the block on every node. The
  walkthrough says that a real node blocks its own detections at once.
  The web console and the audit log (for the activity timeline) are on.
- **Nothing on the host changes.** No host networking, no privileged
  container: every container drops every capability and runs with
  `no-new-privileges`, so no node could program a firewall even if it
  tried, and the `dryrun` backend never tries. The nodes share one bridge
  network; the consoles are published on `127.0.0.1` only. Docker itself
  adds and removes the forwarding rules of its networks and published
  ports, as for any container; the walkthrough says so. Only a user who may
  use Docker is needed, never `sudo`.
- **The console stays loopback-only.** ADR 0019 does not change: the
  console listens on the node's own loopback interface. A forwarder
  container per trusted node joins the node's network namespace and passes
  the published port on to the console with busybox `nc`, running as
  `nonroot` (65532), the user the console admits like obiectl. Browsers
  send `Host: 127.0.0.1:<port>`, which the host check accepts, and the
  session cookie carries the port, so the three consoles do not sign each
  other out. The forwarder uses the `sandbox-init` image (busybox plus
  obied), so the sandbox pulls no image of its own.
- **One script.** `packaging/sandbox/sandbox` has `up`, `exec`, `console`,
  `logs` and `down`. `up` first checks that Docker is installed, answers
  and has the Compose plugin, and that the console ports are free (with
  `ss` or `lsof`, and Docker's own error as the fallback), and prints each
  problem the way obied and obiectl do (ADR 0028): what, why, next. It
  waits until every node is connected to the three others and prints the
  console addresses and tokens. `down` removes the containers, the
  network, the volumes and the images the sandbox built; Docker keeps its
  build cache and the base images, as it does for every build.
  `OBIE_SANDBOX_PORT` moves the consoles and `OBIE_SANDBOX_PROJECT` names
  the project and its images, so a test can run next to a user's sandbox.
- **The walkthrough is the test.** `documentation/sandbox.md` follows fixed
  rules: a shell block with one `./sandbox` command, followed by a text
  block, is a step and its expected output; a shell block of `git clone`
  and `cd` alone prepares; a paragraph that starts with
  `**In the console:**` names one console page, and every phrase it sets
  in bold must be on that page. `make sandbox-check` (build tag `sandbox`,
  run in CI with the lab's smoke test) starts a sandbox under its own
  project name and ports, runs every step in order, compares the output
  with the page after replacing what differs from run to run (peer IDs,
  event IDs, times, durations, container addresses, ports, tokens) and
  sorting the rows of tables, signs in to the consoles and checks the
  pages, and after `./sandbox down` checks that nothing of the project is
  left. Reading commands are repeated for up to 30 seconds, since
  verdicts take a moment to travel; commands that change something run
  once. `make ci` checks without Docker what it can: the story's sections
  and their order, that every step has its output and every story step
  its console page, the script's messages for a missing or unreachable
  Docker and a taken port against the page's troubleshooting section
  (with a stand-in `docker`), the Compose file's hardening, and the
  configurations `sandbox-init` writes.

## Consequences

- A change to obiectl's output, the console's pages or the decision rules
  that the walkthrough shows fails CI until the page shows the new output.
- CI's packaging job takes about two minutes longer.
- The sandbox needs Docker with the Compose plugin, and network access for
  the first build. Podman and other runtimes are not tested; the script
  names Docker's installation and rootless mode as the fix.
- The consoles are reachable from the sandbox's own network, the stranger
  included, through the forwarders; the token still protects them. That is
  acceptable for a sandbox and does not apply to real nodes.
