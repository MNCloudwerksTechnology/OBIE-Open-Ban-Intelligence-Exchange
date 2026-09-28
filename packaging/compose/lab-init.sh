#!/bin/sh
# Prepares the compose lab (packaging/compose): for each node directory
# given, creates the node key once and (re)writes the node's configuration
# so that every node bootstraps to, and fully trusts, all the others.
# The directories are the nodes' volumes; they are handed to the image user
# nonroot (65532) that runs obied.
#
# Usage: lab-init <dir>...   (each <dir> is /lab/<node name>)
set -eu

[ $# -ge 2 ] || {
	echo "lab-init: need at least two node directories" >&2
	exit 1
}

# The peer ID is recorded next to the key when it is created: obied refuses
# to read a key file owned by another user, and the key belongs to nonroot.
peer_id() {
	cat "$1/peer-id"
}

for dir in "$@"; do
	if [ ! -f "$dir/state/node.key" ]; then
		mkdir -p "$dir/state"
		chmod 0700 "$dir/state"
		obied keygen --state-dir "$dir/state" >"$dir/identity.txt"
		sed -n 's/^Peer ID: *//p' "$dir/identity.txt" >"$dir/peer-id"
		rm "$dir/identity.txt"
	fi
done

for dir in "$@"; do
	self=$(basename "$dir")
	{
		cat <<EOF
# Written by lab-init on every start of the lab; edits are overwritten.
node:
  state_dir: /var/lib/obie/state
  mode: enforce
admin:
  socket: /run/obie/obie.sock
  socket_group: nonroot
mesh:
  listen: [/ip4/0.0.0.0/tcp/4001, /ip4/0.0.0.0/udp/4001/quic-v1]
  bootstrap:
EOF
		for peer in "$@"; do
			[ "$peer" = "$dir" ] && continue
			echo "    - /dns4/$(basename "$peer")/tcp/4001/p2p/$(peer_id "$peer")"
		done
		echo "trust:"
		echo "  publishers:"
		for peer in "$@"; do
			[ "$peer" = "$dir" ] && continue
			echo "    - {peer_id: $(peer_id "$peer"), name: $(basename "$peer"), weight: 1.0}"
		done
		cat <<EOF
# Two reports at obiectl's default confidence (0.8) from two trusted
# nodes reach this threshold.
decision:
  threshold: 1.5
  quorum: 2
enforce:
  backend: dryrun
metrics:
  listen: 0.0.0.0:9464
EOF
	} >"$dir/obie.yaml"
	obied --config "$dir/obie.yaml" --check-config >/dev/null
	echo "lab-init: $self is $(peer_id "$dir")"
done

chown -R 65532:65532 "$@"
