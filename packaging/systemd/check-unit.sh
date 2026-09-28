#!/bin/sh
# Checks packaging/systemd/obied.service with systemd-analyze: `verify` must
# pass without a single warning, and the offline `security` exposure score
# must not exceed MAX_EXPOSURE (default 3.0).
#
# Usage: check-unit.sh <bin-dir>
#
# <bin-dir> holds a built obied: verify requires the unit's executables to
# exist, so the check runs on a copy of the unit whose /usr/local/bin paths
# point there.
set -eu

bin_dir=${1:?usage: check-unit.sh <bin-dir>}
max_exposure=${MAX_EXPOSURE:-3.0}
unit="$(dirname "$0")/obied.service"

if ! command -v systemd-analyze >/dev/null 2>&1; then
	echo "check-unit: systemd-analyze not found; it ships with systemd" >&2
	exit 1
fi
if [ ! -x "$bin_dir/obied" ]; then
	echo "check-unit: $bin_dir/obied not found; run make build first" >&2
	exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
bin_abs=$(cd "$bin_dir" && pwd)
sed "s#/usr/local/bin/#$bin_abs/#g" "$unit" >"$tmp/obied.service"

# verify reports unknown or invalid settings as warnings with exit code 0,
# so any output is a failure.
out=$(systemd-analyze verify "$tmp/obied.service" 2>&1) || {
	echo "$out" >&2
	echo "check-unit: systemd-analyze verify failed" >&2
	exit 1
}
if [ -n "$out" ]; then
	echo "$out" >&2
	echo "check-unit: systemd-analyze verify reported problems" >&2
	exit 1
fi
echo "check-unit: systemd-analyze verify passed"

# --threshold is the exposure times 10.
threshold=$(echo "$max_exposure" | awk '{ printf "%d", $1 * 10 }')
out=$(systemd-analyze security --offline=yes --threshold="$threshold" "$tmp/obied.service" 2>&1) || {
	echo "$out" >&2
	echo "check-unit: exposure above $max_exposure" >&2
	exit 1
}
echo "$out" | grep 'Overall exposure level'
