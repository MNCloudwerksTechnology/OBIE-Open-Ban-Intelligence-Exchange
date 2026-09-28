# OBIE three-node lab

Three OBIE nodes on one Docker bridge network, trying the whole mesh on a
laptop in a few minutes: each node bootstraps to the other two and trusts
them fully, and blocks an address once two of them report it. The nodes
run in `enforce` mode with the `dryrun` backend, so a "block" is only
logged and listed — nothing on your machine is ever blocked.

You need Docker with the Compose plugin (`docker compose version`). No
host networking, no root, no Go toolchain: the image is built from the
repository's `Dockerfile`.

## Start

```sh
cd packaging/compose
docker compose up -d --build --wait
```

The first build takes a minute or two. The one-shot `init` service
creates a node key per node (kept in the volumes `node1`..`node3`) and
writes each node's configuration (`/var/lib/obie/obie.yaml` in the node);
then `node1`, `node2` and `node3` start. `--wait` returns once all three
report healthy (`obiectl status` answers).

## Look around

```sh
docker compose exec node1 obiectl status
docker compose exec node1 obiectl peers            # node2 and node3, trust 1
docker compose logs -f node3                       # JSON logs
```

## Block an address by consensus

Report a (made-up) SSH brute-force from `1.2.3.4` on two nodes; the
third decides on their signed verdicts:

```sh
docker compose exec node1 obiectl report --protocol ssh --reason bruteforce --events 5 1.2.3.4
docker compose exec node2 obiectl report --protocol ssh --reason bruteforce --events 5 1.2.3.4
docker compose exec node3 obiectl explain 1.2.3.4   # Decision: block, score 1.6 >= 1.5, 2 publishers
docker compose exec node3 obiectl enforced          # what the dryrun backend "applies"
```

With only one report, `node3` shows `below consensus` (quorum 2); the
reporting node itself blocks at once (`decision.local_autoblock`).
Withdraw one report and `node3` lifts the block, since consensus is gone
(`node2` keeps its own local block until it revokes too):

```sh
docker compose exec node1 obiectl revoke 1.2.3.4
docker compose exec node3 obiectl explain 1.2.3.4   # Decision: none
```

Private addresses such as the lab's own `172.x` network are on the
built-in allow-list and can never be reported or blocked.

## Stop

```sh
docker compose down        # keeps the node keys and event stores
docker compose down -v     # removes them too
```

## Smoke test

`make lab-smoke` (or `packaging/compose/smoke-test.sh`) runs the steps
above unattended under a separate project name: it starts the lab, waits
until each node is connected to both others and until `node3` blocks the
address reported by `node1` and `node2`, and removes the lab again. CI
runs it on every pull request.

## Files

- `compose.yaml` — the lab: `init` plus three nodes on the network `lab`.
- `lab-init.sh` — the `init` service (Dockerfile stage `lab-init`):
  keys, peer IDs, configurations.
- `smoke-test.sh` — the smoke test.

The image itself (`docker build -t obie .` in the repository root) runs a
single node with `packaging/docker/obie.yaml`; see
[documentation/operations/install.md](../../documentation/operations/install.md).
