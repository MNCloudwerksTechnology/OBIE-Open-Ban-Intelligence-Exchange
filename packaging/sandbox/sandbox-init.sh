#!/bin/sh
# Prepares the sandbox (packaging/sandbox): creates each node's key once
# and (re)writes each node's configuration. The trusted nodes bootstrap to
# and fully trust each other; the stranger connects to all of them, but
# nobody lists it as a trusted publisher, so its verdicts count for
# nothing. The directories are the nodes' volumes; run as root (the init
# container), the script hands them to the image user nonroot (65532)
# that runs obied.
#
# Usage: sandbox-init --stranger <dir> <dir>...   (each <dir> is /sandbox/<node name>)
set -eu

usage() {
	echo "usage: sandbox-init --stranger <dir> <trusted dir> <trusted dir>..." >&2
	exit 2
}

[ $# -ge 4 ] && [ "$1" = --stranger ] || usage
stranger=$2
shift 2

# The peer ID is recorded next to the key when it is created: obied refuses
# to read a key file owned by another user, and the key belongs to nonroot.
peer_id() {
	cat "$1/peer-id"
}

for dir in "$stranger" "$@"; do
	if [ ! -f "$dir/state/node.key" ]; then
		mkdir -p "$dir/state"
		chmod 0700 "$dir/state"
		obied keygen --state-dir "$dir/state" >"$dir/identity.txt"
		sed -n 's/^Peer ID: *//p' "$dir/identity.txt" >"$dir/peer-id"
		rm "$dir/identity.txt"
	fi
done

# bootstrap <dir>... lists the mesh addresses of the nodes in the dirs.
bootstrap() {
	echo "  bootstrap:"
	for peer in "$@"; do
		echo "    - /dns4/$(basename "$peer")/tcp/4001/p2p/$(peer_id "$peer")"
	done
}

# common writes the keys every node shares.
common() {
	cat <<EOF
node:
  state_dir: /var/lib/obie/state
  # enforce with the dryrun backend: the node decides and lists what it
  # blocks, and blocks nothing.
  mode: enforce
admin:
  socket: /run/obie/obie.sock
  socket_group: nonroot
enforce:
  backend: dryrun
EOF
}

for dir in "$@"; do
	self=$(basename "$dir")
	{
		echo "# Written by sandbox-init on every start of the sandbox; edits are overwritten."
		common
		echo "mesh:"
		echo "  listen: [/ip4/0.0.0.0/tcp/4001]"
		others=""
		for peer in "$@"; do
			[ "$peer" = "$dir" ] || others="$others $peer"
		done
		# Unquoted: the directory names have no blanks.
		bootstrap $others
		echo "trust:"
		echo "  publishers:"
		for peer in $others; do
			echo "    - {peer_id: $(peer_id "$peer"), name: $(basename "$peer"), weight: 1.0}"
		done
		cat <<EOF
  # Every other publisher, the stranger among them, counts nothing.
  default_weight: 0
decision:
  # Two reports at obiectl's default confidence (0.8) from two trusted
  # nodes reach this threshold; one never does.
  threshold: 1.5
  quorum: 2
  # Off, so that a node's own report counts like any other and every node
  # waits for a second one: on a real server it blocks its own detections
  # at once.
  local_autoblock: false
console:
  enabled: true
  listen: 127.0.0.1:9465
audit:
  path: /var/lib/obie/audit.jsonl
EOF
	} >"$dir/obie.yaml"
	obied --config "$dir/obie.yaml" --check-config >/dev/null
	echo "sandbox-init: $self is $(peer_id "$dir")"
done

{
	echo "# Written by sandbox-init on every start of the sandbox; edits are overwritten."
	echo "# The stranger connects to the trusted nodes, none of which trusts it."
	common
	echo "mesh:"
	echo "  listen: [/ip4/0.0.0.0/tcp/4001]"
	bootstrap "$@"
} >"$stranger/obie.yaml"
obied --config "$stranger/obie.yaml" --check-config >/dev/null
echo "sandbox-init: $(basename "$stranger") is $(peer_id "$stranger") (trusted by nobody)"

if [ "$(id -u)" -eq 0 ]; then
	chown -R 65532:65532 "$stranger" "$@"
fi
