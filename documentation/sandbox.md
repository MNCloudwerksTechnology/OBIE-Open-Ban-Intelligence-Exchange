# Try OBIE in a sandbox

Watch OBIE work on your own computer before you let it near a real
firewall. The sandbox runs four OBIE [nodes](glossary.md#node) in
containers: three that trust each other and a stranger that nobody trusts.
In about fifteen minutes you report an attack on one node, watch another
node block the attacker once a second node agrees, ask why, overrule the
decision, take the report back, and see the stranger fail to get anything
blocked.

Nothing on your computer is blocked, at any step. Every node runs in
[enforce mode](glossary.md#enforce-mode) with a pretend firewall (the
`dryrun` backend): it decides and lists what it would block, and blocks
nothing. The containers have no permission to change any firewall, and
the sandbox never asks for root.

An automated test runs every `./sandbox` command this page shows with its
output, on every change to OBIE, and compares the output with the page. Only
what changes from run to run differs in yours:
[peer IDs](glossary.md#peer-id), event IDs, times, latencies, container
addresses and tokens.

## What you need

- A computer with [Docker](https://docs.docker.com/get-started/get-docker/)
  and its Compose plugin. The sandbox is tested on Linux with Docker
  Engine. Docker Desktop on macOS or Windows and rootless Docker should
  work too, but nobody has tested them yet. On Windows, run the commands
  below in WSL 2: `./sandbox` is a shell script.
- A user who may use Docker: `docker info` must work without `sudo`.
- `git`, to get OBIE's source code. The sandbox builds OBIE from it.
- The ports 9401, 9402 and 9403 on `127.0.0.1` free, for the web consoles.
  [Other ports](#a-console-port-is-taken) work too.

The first start downloads Docker's Go build image and builds OBIE. That
takes a few minutes and a couple of gigabytes of disk; later starts take
seconds.

## What is in the sandbox

| Node | Trusts | Web console |
|------|--------|-------------|
| node1 | node2 and node3 | <http://127.0.0.1:9401/> |
| node2 | node1 and node3 | <http://127.0.0.1:9402/> |
| node3 | node1 and node2 | <http://127.0.0.1:9403/> |
| stranger | nobody; nobody trusts it | none |

Each node is connected to the three others. A report is a
[verdict](glossary.md#verdict): a statement, signed by the node that makes
it, that an address attacked it. Every node decides for itself whether to
block an address. It adds up the verdicts on the address: each counts its
publisher's [trust weight](glossary.md#trust-weight) (1 for node1, node2
and node3, 0 for everyone else) times how sure the publisher is (0.8
here). A node blocks the address once this score reaches the
[threshold](glossary.md#threshold) of 1.5 and at least two nodes reported
it (the [quorum](glossary.md#quorum)). One report scores 0.8, so it is
never enough; two score 1.6.

The sandbox differs from a real server in one more way. There, a node
blocks what it detects itself at once, without waiting for the others
(local autoblock). In the sandbox this is off, so that a node's own
report counts like any other and you can watch the nodes agree.

## Start the sandbox

Get OBIE's source code and go to the sandbox:

```sh
git clone https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange.git
cd OBIE-Open-Ban-Intelligence-Exchange/packaging/sandbox
```

Start it. Docker shows its progress first; then the sandbox waits until
every node is connected to the three others:

```sh
./sandbox up
```

```text
The sandbox runs: node1, node2 and node3 trust each other; nobody trusts the stranger.
Every node decides and lists what it blocks, and blocks nothing (enforce mode, dryrun backend).

Web consoles, one per trusted node; sign in with the token:
node1  http://127.0.0.1:9401/  token K_JtXjeMXfOG1B9EHceiJT0rRHSUouCZexuXTYfBF6o
node2  http://127.0.0.1:9402/  token QRJT9YNbhudCWGldk7tkr9ho29DnxqBGhvIvrw4F_5Q
node3  http://127.0.0.1:9403/  token F2Qw4odM8CrkBvx9ZDQKZtFQPTkUp9E8EV5yEqHFykg

Next: follow the walkthrough in documentation/sandbox.md. Remove the sandbox with ./sandbox down
```

If it stops with a message instead, see
[When the sandbox does not start](#when-the-sandbox-does-not-start).

Every step below runs `obiectl`, the command you use on a real server, in
one of the nodes: `./sandbox exec <node> obiectl <command>` is what
`sudo obiectl <command>` is on a server. Ask node3 which
[peers](glossary.md#peer) it is connected to:

```sh
./sandbox exec node3 obiectl peers
```

```text
PEER ID                                               NAME   TRUST  BOOTSTRAP  CONNECTED SINCE       LATENCY  ADDRESSES
12D3KooWKBqD57onvndX5F4g8dKq51PCPWo4E8wskuwqRfSgbVk9  node1  1      yes        2026-09-29T18:26:20Z  200µs    /ip4/172.23.0.3/tcp/4001
12D3KooWNUDSzZ3pbYX5bwa7M4KRUeDeNBFJ8bWTADErgS7kafZ3  -      0      no         2026-09-29T18:26:21Z  200µs    /ip4/172.23.0.4/tcp/4001
12D3KooWPF4rx1f4xYknji6ZCm1fizNSGyVtxdb7FXmXbRP42KYz  node2  1      yes        2026-09-29T18:26:20Z  100µs    /ip4/172.23.0.2/tcp/4001
```

node1 and node2 have trust 1. The peer without a name and with trust 0 is
the stranger: node3 does not know it, but lets it connect.

The same story is visible in the web consoles, one per trusted node. Open
<http://127.0.0.1:9403/> in a browser on this computer and sign in with
node3's token from the output of `./sandbox up`. This shows it again:

```sh
./sandbox console node3
```

```text
node3  http://127.0.0.1:9403/  token F2Qw4odM8CrkBvx9ZDQKZtFQPTkUp9E8EV5yEqHFykg
```

Keep the console open next to your terminal: each step below says where
to look.

**In the console:** node3's **Peers** page, <http://127.0.0.1:9403/peers>,
lists node1 and node2 as **Trusted publisher** and the stranger with
**Trust weight 0**: **No influence on decisions**.

## 1. node1 detects an attack and publishes a verdict

On a real server, Fail2Ban notices an attack and reports it through
`obiectl report`. Here you play Fail2Ban: tell node1 that the address
`1.2.3.4` tried 12 SSH passwords. node1 signs a verdict and sends it to
its peers:

```sh
./sandbox exec node1 obiectl report --protocol ssh --reason password_bruteforce --events 12 1.2.3.4
```

```text
Reported ipv4:1.2.3.4: verdict 01a0ee6b-08e1-731d-a8b7-66c3c3a411b8 issued and published.

Indicator:   ipv4:1.2.3.4
Action:      ban
Confidence:  0.8
Protocol:    ssh
Reason:      password_bruteforce
Events:      12
Log hash:    -
Issued:      2026-09-29T18:26:27Z
Expires:     2026-10-06T18:26:27Z (7d)
Publisher:   12D3KooWKBqD57onvndX5F4g8dKq51PCPWo4E8wskuwqRfSgbVk9
```

The verdict asks the peers to block the address (`ban`), with node1's
confidence of 0.8, for seven days. No log line leaves node1: had you given
the log lines as evidence, only their hash would travel.

**In the console:** open node1's console, <http://127.0.0.1:9401/verdicts>,
and sign in with node1's token. Its **Verdicts** page lists the verdict
on 1.2.3.4 as published by **This node**, with **12 events**.

## 2. node3 receives it, but does not block yet

Ask node3 about the address:

```sh
./sandbox exec node3 obiectl explain 1.2.3.4
```

```text
Indicator:             ipv4:1.2.3.4
Decision:              none
Reason:                below consensus: score 0.8 < threshold 1.5, 1 < quorum 2
Score:                 0.8 (threshold 1.5)
Publishers:            1 (quorum 2)
Local autoblock:       no
Allow-list/overrides:  none apply
Evaluated:             2026-09-29T18:26:29Z

PUBLISHER  PEER ID                                               ACTION  WEIGHT  CONFIDENCE  SCORE  COUNTS  PROTOCOL  REASON               ISSUED                EXPIRES
node1      12D3KooWKBqD57onvndX5F4g8dKq51PCPWo4E8wskuwqRfSgbVk9  ban     1       0.8         0.8    yes     ssh       password_bruteforce  2026-09-29T18:26:27Z  2026-10-06T18:26:27Z
```

node3 has node1's verdict, and it counts: weight 1 × confidence 0.8 =
0.8. But one publisher is not enough. The score is below the threshold
of 1.5 and one publisher is below the quorum of 2, so the decision is
`none`: node3 does not block. One node, however trusted, cannot make the
others block on its word alone.

**In the console:** node3's decision on the address,
<http://127.0.0.1:9403/decisions/ipv4:1.2.3.4>, says **Not blocked: below
consensus** and **Score 0.8 of threshold 1.5, 1 publisher of quorum 2.**
It checks the address again every five seconds, so you can leave it open
for the next step.

## 3. node2 agrees, and node3 blocks

The same attacker now tries node2, which reports it too:

```sh
./sandbox exec node2 obiectl report --protocol ssh --reason password_bruteforce --events 7 1.2.3.4
```

```text
Reported ipv4:1.2.3.4: verdict 01a0ee6b-133c-7b25-b0f1-56f2965a7f9e issued and published.

Indicator:   ipv4:1.2.3.4
Action:      ban
Confidence:  0.8
Protocol:    ssh
Reason:      password_bruteforce
Events:      7
Log hash:    -
Issued:      2026-09-29T18:26:29Z
Expires:     2026-10-06T18:26:29Z (7d)
Publisher:   12D3KooWPF4rx1f4xYknji6ZCm1fizNSGyVtxdb7FXmXbRP42KYz
```

node3 never saw the attacker itself, yet it now blocks it:

```sh
./sandbox exec node3 obiectl decisions
```

```text
Decisions: 1 (1 block, 0 allowed, 0 none)

INDICATOR     STATE  SCORE  PUBLISHERS  EXPIRES               REASON
ipv4:1.2.3.4  block  1.6    2           2026-10-06T18:26:29Z  consensus: score 1.6 >= threshold 1.5, 2 >= quorum 2
```

Its pretend firewall applies the block, for as long as the verdicts last:

```sh
./sandbox exec node3 obiectl enforced
```

```text
Entries applied: 1

PREFIX      EXPIRES               REMAINING
1.2.3.4/32  2026-10-06T18:26:29Z  6d23h59m56s
```

On a real server in enforce mode, this entry would drop every packet from
1.2.3.4 in OBIE's own nftables table.

node1 and node2 have the same two verdicts, so they block the address too.
node1, for example:

```sh
./sandbox exec node1 obiectl enforced
```

```text
Entries applied: 1

PREFIX      EXPIRES               REMAINING
1.2.3.4/32  2026-10-06T18:26:29Z  6d23h59m55s
```

**In the console:** node3's **Firewall** page,
<http://127.0.0.1:9403/enforcement>, says **1 entry applied for 1
decided block.** and lists 1.2.3.4.

## 4. Look up why node3 blocks the address

When a user asks why they cannot reach your server, `obiectl explain` has
the answer:

```sh
./sandbox exec node3 obiectl explain 1.2.3.4
```

```text
Indicator:             ipv4:1.2.3.4
Decision:              block until 2026-10-06T18:26:29Z
Reason:                consensus: score 1.6 >= threshold 1.5, 2 >= quorum 2
Score:                 1.6 (threshold 1.5)
Publishers:            2 (quorum 2)
Local autoblock:       no
Allow-list/overrides:  none apply
Evaluated:             2026-09-29T18:26:32Z

PUBLISHER  PEER ID                                               ACTION  WEIGHT  CONFIDENCE  SCORE  COUNTS  PROTOCOL  REASON               ISSUED                EXPIRES
node1      12D3KooWKBqD57onvndX5F4g8dKq51PCPWo4E8wskuwqRfSgbVk9  ban     1       0.8         0.8    yes     ssh       password_bruteforce  2026-09-29T18:26:27Z  2026-10-06T18:26:27Z
node2      12D3KooWPF4rx1f4xYknji6ZCm1fizNSGyVtxdb7FXmXbRP42KYz  ban     1       0.8         0.8    yes     ssh       password_bruteforce  2026-09-29T18:26:29Z  2026-10-06T18:26:29Z
```

Two trusted nodes reported the address, each adding 0.8: the score of
1.6 reaches the threshold, and two publishers meet the quorum. The block
ends when the newest verdict expires, unless the publishers renew it.

**In the console:** the decision page,
<http://127.0.0.1:9403/decisions/ipv4:1.2.3.4>, now says **Blocked by
consensus** and **Score 1.6 of threshold 1.5, 2 publishers of quorum 2.**,
with one row per publisher.

## 5. Overrule the decision on node3

Your server has the last word. Say 1.2.3.4 is a partner's gateway that
must never be blocked, whatever the others report. An
[override](glossary.md#override) says so:

```sh
./sandbox exec node3 obiectl allow 1.2.3.4 --note "a partner's gateway"
```

```text
Override set: force_allow on ipv4:1.2.3.4, until removed (note: "a partner's gateway").
Decision now: allowed — operator force-allow override on ipv4:1.2.3.4 (note: "a partner's gateway"); verdicts: consensus: score 1.6 >= threshold 1.5, 2 >= quorum 2
```

The verdicts still say block, but node3 no longer does, and its firewall
lets the address in again:

```sh
./sandbox exec node3 obiectl enforced
```

```text
No entries applied.
```

**In the console:** the decision page,
<http://127.0.0.1:9403/decisions/ipv4:1.2.3.4>, says **Allowed: never
blocked, whatever the verdicts** and shows your note, **a partner's
gateway**. Its **Always allow…** and **Remove the override…** buttons do
the same as `obiectl allow` and `obiectl unoverride`, after a
confirmation.

`obiectl block` does the opposite: it blocks an address whatever the
verdicts say. Remove the override, so that the verdicts decide again:

```sh
./sandbox exec node3 obiectl unoverride 1.2.3.4
```

```text
Override on 1.2.3.4 removed.
Decision now: block — consensus: score 1.6 >= threshold 1.5, 2 >= quorum 2
```

## 6. Revoke a verdict, and the block disappears everywhere

node1's report was a mistake, say a colleague who mistyped a password a
dozen times. node1 takes it back with a
[revocation](glossary.md#revocation), which it signs and sends to its
peers like the verdict:

```sh
./sandbox exec node1 obiectl revoke 1.2.3.4
```

```text
Revoked verdict 01a0ee6b-08e1-731d-a8b7-66c3c3a411b8 on ipv4:1.2.3.4 (revocation 01a0ee6b-3052-74f5-aca8-961afe705948, reason false_positive).
```

Only node2's verdict is left, and one publisher is not enough:

```sh
./sandbox exec node3 obiectl decisions
```

```text
Decisions: 1 (0 block, 0 allowed, 1 none)

INDICATOR     STATE  SCORE  PUBLISHERS  EXPIRES  REASON
ipv4:1.2.3.4  none   0.8    1           -        below consensus: score 0.8 < threshold 1.5, 1 < quorum 2
```

The block is gone from every node's firewall:

```sh
./sandbox exec node1 obiectl enforced
```

```text
No entries applied.
```

```sh
./sandbox exec node2 obiectl enforced
```

```text
No entries applied.
```

```sh
./sandbox exec node3 obiectl enforced
```

```text
No entries applied.
```

**In the console:** node3's **Activity** page,
<http://127.0.0.1:9403/activity>, tells the whole story, newest first. It
shows **Block removed** for 1.2.3.4, with **Cause: revoke**.

## 7. The stranger tries to get an address blocked, and fails

The stranger is connected to every node, but none of them trusts it. It
tries to get 1.2.3.4 blocked again, as sure as can be (confidence 1) and
with 500 attacks as evidence:

```sh
./sandbox exec stranger obiectl report --protocol ssh --reason password_bruteforce --confidence 1 --events 500 1.2.3.4
```

```text
Reported ipv4:1.2.3.4: verdict 01a0ee6b-3b83-7b5f-aea1-e540a93e8c3e issued and published.

Indicator:   ipv4:1.2.3.4
Action:      ban
Confidence:  1
Protocol:    ssh
Reason:      password_bruteforce
Events:      500
Log hash:    -
Issued:      2026-09-29T18:26:40Z
Expires:     2026-10-06T18:26:40Z (7d)
Publisher:   12D3KooWNUDSzZ3pbYX5bwa7M4KRUeDeNBFJ8bWTADErgS7kafZ3
```

node3 receives the verdict and shows it, but it counts for nothing:

```sh
./sandbox exec node3 obiectl explain 1.2.3.4
```

```text
Indicator:             ipv4:1.2.3.4
Decision:              none
Reason:                below consensus: score 0.8 < threshold 1.5, 1 < quorum 2
Score:                 0.8 (threshold 1.5)
Publishers:            1 (quorum 2)
Local autoblock:       no
Allow-list/overrides:  none apply
Evaluated:             2026-09-29T18:26:42Z

PUBLISHER  PEER ID                                               ACTION  WEIGHT  CONFIDENCE  SCORE  COUNTS  PROTOCOL  REASON               ISSUED                EXPIRES
-          12D3KooWNUDSzZ3pbYX5bwa7M4KRUeDeNBFJ8bWTADErgS7kafZ3  ban     0       1           0      no      ssh       password_bruteforce  2026-09-29T18:26:40Z  2026-10-06T18:26:40Z
node2      12D3KooWPF4rx1f4xYknji6ZCm1fizNSGyVtxdb7FXmXbRP42KYz  ban     1       0.8         0.8    yes     ssh       password_bruteforce  2026-09-29T18:26:29Z  2026-10-06T18:26:29Z
```

The stranger's weight is 0, so its verdict adds 0 to the score and does
not count towards the quorum, however sure it claims to be and however
often it reports. Only the nodes you list, with the weight you give
them, can influence your decisions. A stranger could also invent reports
to get an innocent address blocked; it fails the same way.

Each node decides for itself, the stranger too. On its own server, its
own report blocks the address at once (local autoblock is on there, as
on a real server):

```sh
./sandbox exec stranger obiectl decisions
```

```text
Decisions: 1 (1 block, 0 allowed, 0 none)

INDICATOR     STATE  SCORE  PUBLISHERS  EXPIRES               REASON
ipv4:1.2.3.4  block  1      1           2026-10-06T18:26:40Z  local autoblock: this node's own ban verdict (score 1 < threshold 1.8, 1 < quorum 2)
```

That is its own business: it cannot make anyone else block.

**In the console:** node3's decision page,
<http://127.0.0.1:9403/decisions/ipv4:1.2.3.4>, still says **Not blocked:
below consensus**. The stranger's row shows **Trust weight 0** and, under
Counts, **No: weight 0**.

## Remove the sandbox

One command removes everything the sandbox created: its containers, their
network, the nodes' keys and data, and the images it built:

```sh
./sandbox down
```

```text
The sandbox is removed: its containers, network, volumes and images.
```

Docker keeps its build cache and the base images it downloaded to build
OBIE (`golang`, `busybox` and `gcr.io/distroless/static-debian12`), which
make the next start fast. `docker builder prune` removes the build cache;
`docker image ls` lists the base images, and `docker image rm` removes
those that nothing else of yours needs.

## When the sandbox does not start

`./sandbox up` checks Docker and the ports before it starts anything. When
something is missing, it says what and how to fix it, and exits with
status 1.

### Docker is not installed

```text
sandbox: Docker is not installed: there is no docker command.
  Why: the sandbox runs its nodes in containers, and Docker runs them.
  Next: install Docker Engine (https://docs.docker.com/engine/install/) or Docker Desktop, then run ./sandbox up again.
        Rootless Docker needs no root at all: https://docs.docker.com/engine/security/rootless/
```

### Docker refuses your user

```text
sandbox: your user may not use Docker.
  Why: permission denied while trying to connect to the docker API at unix:///var/run/docker.sock
  Next: add yourself to the group docker (sudo usermod -aG docker "$USER", then log in again),
        or use rootless Docker (https://docs.docker.com/engine/security/rootless/). The sandbox itself needs no sudo.
```

Know that members of the group `docker` control Docker, and through it the
whole computer. Rootless Docker runs as your user and avoids that.

### Docker does not answer

```text
sandbox: Docker is installed but does not answer.
  Why: failed to connect to the docker API at unix:///var/run/docker.sock; check if the path is correct and if the daemon is running: dial unix /var/run/docker.sock: connect: no such file or directory
  Next: start Docker (sudo systemctl start docker, or open Docker Desktop), then run ./sandbox up again.
```

The `Why` line is Docker's own message, so yours may read differently.

### The Compose plugin is missing

```text
sandbox: the Docker Compose plugin is missing: docker compose does not work.
  Why: the sandbox is a Compose project (packaging/sandbox/compose.yaml).
  Next: install the plugin (package docker-compose-plugin, https://docs.docker.com/compose/install/linux/), then run ./sandbox up again.
```

### A console port is taken

```text
sandbox: port 9403 on 127.0.0.1 is taken by another program, so the sandbox cannot publish a web console on it.
  Why: the consoles of node1, node2 and node3 use the ports 9401, 9402 and 9403.
  Next: stop the program that uses port 9403, or start the sandbox on three other ports, e.g. OBIE_SANDBOX_PORT=9501 ./sandbox up
```

`OBIE_SANDBOX_PORT` is the port of node1's console; node2 and node3 use the
next two. The console addresses on this page change to match. If only
Docker notices the taken port, `./sandbox up` says the same after
Docker's own message, and removes what it had started.

### Anything else

If a node does not start or connect, `./sandbox up` says which;
`./sandbox logs <node>` shows what the node logged. `./sandbox down` and
`./sandbox up` start from scratch.

## What the sandbox does not show

- **A real firewall.** The nodes only list what they would block. On a
  server, enforce mode drops the traffic in OBIE's own nftables table; the
  [nftables guide](guides/nftables.md) shows how.
- **[Observe mode](glossary.md#observe-mode).** A real node starts in
  observe mode: it decides and lists like the sandbox's nodes, but
  applies nothing, until you switch it to enforce mode.
- **Blocking its own detections at once.** On a real server local
  autoblock is on: the node that detects an attack blocks the address at
  once, without waiting for a second report, and keeps blocking it as
  long as its own verdict lasts. Had node2 run like that, it would still
  block 1.2.3.4 after node1 took its report back in step 6.
- **Who may sign in to a console.** On a server, only the node's
  operators reach its web console. The sandbox publishes each console on
  a port of `127.0.0.1`, so anyone logged in to your computer reaches its
  sign-in page; only the token lets them in.
- **Docker's own rules.** The sandbox's nodes never touch a firewall, but
  Docker itself adds the forwarding rules its container network and the
  published console ports need, as for any container, and removes them
  with the sandbox. Rootless Docker keeps even those inside its own
  namespace.

## How this page is tested

`make sandbox-check` runs every `./sandbox` command this page shows with
its output, in order, against a sandbox of its own, and compares each
output with the output shown here. It opens every console page this page
links and checks that it shows what is set in bold, and after
`./sandbox down` it checks that nothing of the sandbox is left. The
messages under
[When the sandbox does not start](#when-the-sandbox-does-not-start) are
compared with what `./sandbox up` says when a stand-in for Docker reports
each problem, or when a program holds a console port. Continuous
integration runs both on every change, so this page cannot go stale
unnoticed ([ADR 0029](adr/0029-sandbox-and-checked-walkthrough.md)).

## Where to go next

- [What is OBIE?](introduction.md) explains the ideas behind what you
  just saw.
- [What OBIE can and cannot do yet](capabilities.md) says whether it fits
  your servers.
- [Quick start](operations/quickstart.md): your first real node, safely in
  observe mode.
- [Web console](operations/console.md): every page of the console.
- [Federation](operations/federation.md): connecting your node with a
  friend's.
