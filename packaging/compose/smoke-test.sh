#!/bin/sh
# Smoke test of the compose lab: builds and starts it under its own project
# name, waits until every node is connected to both others, reports one
# address from node1 and node2 and waits until node3 blocks it (dryrun),
# then removes the lab with its volumes.
#
# Usage: smoke-test.sh   (make lab-smoke)
set -eu

cd "$(dirname "$0")"
project=${PROJECT:-obie-lab-smoke}
timeout=${TIMEOUT:-60}
addr=1.2.3.4
compose() { docker compose -p "$project" "$@"; }

cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		compose logs --no-color --tail 50 >&2 || true
	fi
	compose down -v --remove-orphans >/dev/null 2>&1 || true
	exit "$status"
}
trap cleanup EXIT INT TERM

# until <description> <command...> retries the command every second until
# it succeeds or $timeout seconds have passed.
until_ok() {
	what=$1
	shift
	i=0
	until "$@" >/dev/null 2>&1; do
		i=$((i + 1))
		if [ "$i" -ge "$timeout" ]; then
			echo "smoke-test: FAILED: $what within ${timeout}s" >&2
			"$@" >&2 || true
			return 1
		fi
		sleep 1
	done
	echo "smoke-test: ok: $what"
}

peers_of() {
	[ "$(compose exec -T "$1" obiectl peers --json | grep -c '"peer_id"')" -eq 2 ]
}

blocks() {
	compose exec -T "$1" obiectl explain --json "$addr" | grep -q '"state": "block"' &&
		compose exec -T "$1" obiectl enforced | grep -q "$addr"
}

compose up -d --build --wait --wait-timeout "$timeout"
for node in node1 node2 node3; do
	until_ok "$node sees both other nodes" peers_of "$node"
done
for node in node1 node2; do
	compose exec -T "$node" obiectl report --protocol ssh --reason bruteforce --events 5 "$addr" >/dev/null
done
until_ok "node3 blocks $addr on the reports of node1 and node2" blocks node3
echo "smoke-test: PASSED"
