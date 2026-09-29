#!/bin/sh
# Checks the Fail2Ban action action.d/obie.conf against the Fail2Ban of
# real distributions (make fail2ban-versions). For each image, it starts a
# container, installs the distribution's Fail2Ban, and runs a real
# fail2ban-server with one jail that uses the action with a fake obiectl:
# two failed logins in the jail's log must make Fail2Ban call
# `obiectl report` with the address, the failure count, the ban time as TTL
# and the matched lines on stdin, and an unban must call `obiectl revoke`.
# It prints one line per image and fails if any image fails.
#
# Usage: check-versions.sh [image...]   (default: $IMAGES below)
# Needs docker and network access to the distributions' package mirrors.
set -eu

IMAGES=${IMAGES:-"debian:12 debian:13 ubuntu:22.04 ubuntu:24.04 ubuntu:26.04 alpine:3.22 rockylinux/rockylinux:9"}

# inside runs in the container: installs Fail2Ban, bans and unbans, checks
# what the action handed to obiectl.
inside() {
	if command -v apt-get >/dev/null; then
		apt-get update -qq >/dev/null
		DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends fail2ban bsdutils >/dev/null
	elif command -v apk >/dev/null; then
		apk add -q fail2ban >/dev/null
	elif command -v dnf >/dev/null; then
		dnf install -y -q epel-release >/dev/null
		dnf install -y -q fail2ban-server util-linux >/dev/null
	else
		echo "no known package manager" >&2
		return 1
	fi
	version=$(fail2ban-client --version 2>&1 | sed -n 's/^Fail2Ban v\{0,1\}//p' | head -n 1)

	work=/tmp/obie-check
	mkdir -p "$work" /var/run/fail2ban
	cat >"$work/obiectl" <<'EOF'
#!/bin/sh
{ echo --call--; for a in "$@"; do printf '%s\n' "$a"; done; } >>/tmp/obie-check/args
case " $* " in *" --evidence-from-stdin "*) cat >>/tmp/obie-check/stdin;; esac
EOF
	chmod 0755 "$work/obiectl"
	: >"$work/auth.log"

	conf=/etc/fail2ban
	install -m 0644 /obie/action.d/obie.conf "$conf/action.d/obie.conf"
	cat >"$conf/filter.d/obie-check.conf" <<'EOF'
[Definition]
failregex = obie-check: failed login from <HOST>$
EOF
	# Only this jail: drop the distribution's own jails.
	rm -rf "$conf/jail.d" "$conf"/*.local
	cat >"$conf/fail2ban.local" <<EOF
[Definition]
logtarget = $work/fail2ban.log
EOF
	cat >"$conf/jail.local" <<EOF
[DEFAULT]
backend = polling

[obie-check]
enabled  = true
filter   = obie-check
logpath  = $work/auth.log
maxretry = 2
findtime = 600
bantime  = 900
action   = obie[obiectl=$work/obiectl, revoke_on_unban=true]
EOF
	fail2ban-client -t >/dev/null
	fail2ban-client -x start >/dev/null

	ip=203.0.113.7
	for _ in 1 2; do
		echo "$(date '+%Y-%m-%d %H:%M:%S') obie-check: failed login from $ip" >>"$work/auth.log"
	done
	wait_for "a report" grep -qsx report "$work/args"
	fail2ban-client set obie-check unbanip "$ip" >/dev/null
	wait_for "a revocation" grep -qsx revoke "$work/args"
	fail2ban-client stop >/dev/null

	expect_args report --ip "$ip" --protocol obie-check --reason bruteforce --events 2 \
		--mitre T1110 --ttl 900s --evidence-from-stdin
	expect_args revoke --reason unbanned "$ip"
	if [ "$(grep -c "obie-check: failed login from $ip" "$work/stdin")" -ne 2 ]; then
		fail "stdin of obiectl report holds $(wc -l <"$work/stdin") lines, want the 2 matched lines"
	fi
	echo "Fail2Ban $version: ok"
}

# wait_for <what> <command...> waits up to 30 seconds for the command to
# succeed.
wait_for() {
	what=$1
	shift
	i=0
	until "$@"; do
		i=$((i + 1))
		[ "$i" -lt 60 ] || fail "no $what within 30s"
		sleep 0.5
	done
}

# expect_args <arg...> checks that one call of obiectl had exactly these
# arguments after the four of --socket and --timeout.
expect_args() {
	want=$(printf '%s|' "$@")
	calls=$(awk '/^--call--$/ { if (c != "") print c; c = ""; skip = 4; next }
		skip > 0 { skip--; next }
		{ c = c $0 "|" }
		END { if (c != "") print c }' /tmp/obie-check/args)
	printf '%s\n' "$calls" | grep -qxF "$want" ||
		fail "obiectl was not called with: $* (calls: $calls)"
}

fail() {
	echo "FAILED: $*" >&2
	if [ -f /tmp/obie-check/fail2ban.log ]; then tail -n 20 /tmp/obie-check/fail2ban.log >&2; fi
	exit 1
}

if [ "${1:-}" = --inside ]; then
	inside
	exit
fi

dir=$(cd "$(dirname "$0")" && pwd)
[ $# -gt 0 ] || set -- $IMAGES
failed=0
for image in "$@"; do
	if out=$(docker run --rm -v "$dir:/obie:ro" "$image" sh /obie/check-versions.sh --inside 2>&1); then
		echo "$image: $(printf '%s\n' "$out" | tail -n 1)"
	else
		echo "$image: FAILED" >&2
		printf '%s\n' "$out" | sed 's/^/  /' >&2
		failed=1
	fi
done
exit "$failed"
