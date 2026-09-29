#!/bin/sh
# Installs OBIE from an extracted release tarball:
#
#   tar -xzf obie-<version>-linux-<arch>.tar.gz
#   cd obie-<version>-linux-<arch>
#   sudo ./install.sh
#
# It creates the system user and group obie, installs obied and obiectl
# into $PREFIX/bin, the example configuration as /etc/obie/obie.yaml (only
# if there is none; the current example always goes to
# /etc/obie/obie.yaml.example), the systemd unit obied.service and, if
# Fail2Ban is installed, its action obie.conf. Running it again upgrades
# the binaries, the unit and the action and changes nothing else, so it is
# also the upgrade path. It never starts or restarts obied.
#
# Environment:
#   PREFIX   installation prefix of the binaries (default /usr/local)
#   DESTDIR  stage everything below this directory instead of /, e.g. for
#            packaging; user and group, ownership and systemctl are skipped
set -eu

PREFIX=${PREFIX:-/usr/local}
DESTDIR=${DESTDIR:-}
src=$(cd "$(dirname "$0")" && pwd)

bindir="$DESTDIR$PREFIX/bin"
confdir="$DESTDIR/etc/obie"
unitdir="$DESTDIR/etc/systemd/system"
f2bdir="$DESTDIR/etc/fail2ban/action.d"

log() { echo "install.sh: $*"; }
die() {
	echo "install.sh: $*" >&2
	exit 1
}

case "${1:-}" in
"") ;;
-h | --help)
	sed -n '2,/^set -eu/p' "$0" | sed '$d; s/^# \{0,1\}//'
	exit 0
	;;
*) die "unexpected argument $1; see --help" ;;
esac

case "$PREFIX" in
/*) ;;
*) die "PREFIX must be an absolute path, got '$PREFIX'" ;;
esac
case "$PREFIX" in
*[\#\&\ ]* | /home/* | /root/*)
	die "PREFIX must not contain spaces, # or & nor lie below /home or /root (the unit sets ProtectHome), got '$PREFIX'"
	;;
esac

for f in bin/obied bin/obiectl etc/obie.yaml systemd/obied.service fail2ban/action.d/obie.conf; do
	[ -f "$src/$f" ] || die "$src/$f is missing; run install.sh from an extracted release tarball"
done
if [ -z "$DESTDIR" ] && [ "$(id -u)" -ne 0 ]; then
	die "must run as root (or with DESTDIR set)"
fi

# The system user and group that run obied; members of the group may use
# obiectl.
if [ -z "$DESTDIR" ]; then
	if ! getent group obie >/dev/null; then
		groupadd --system obie
		log "created group obie"
	fi
	if ! getent passwd obie >/dev/null; then
		nologin=$(command -v nologin || echo /usr/sbin/nologin)
		useradd --system --gid obie --home-dir /var/lib/obie --no-create-home \
			--shell "$nologin" --comment "OBIE node daemon" obie
		log "created user obie"
	fi
fi

install -d -m 0755 "$bindir"
for b in obied obiectl; do
	install -m 0755 "$src/bin/$b" "$bindir/$b"
done
log "installed obied and obiectl into $bindir"

# obie.yaml is readable by root and the obie group only: it may name
# internal networks. An existing obie.yaml is never touched.
install -d -m 0755 "$confdir"
install -m 0644 "$src/etc/obie.yaml" "$confdir/obie.yaml.example"
if [ -e "$confdir/obie.yaml" ]; then
	log "kept $confdir/obie.yaml; the current example is $confdir/obie.yaml.example"
else
	install -m 0640 "$src/etc/obie.yaml" "$confdir/obie.yaml"
	[ -n "$DESTDIR" ] || chown root:obie "$confdir/obie.yaml"
	log "installed $confdir/obie.yaml"
fi

install -d -m 0755 "$unitdir"
sed "s#/usr/local/bin/#$PREFIX/bin/#g" "$src/systemd/obied.service" >"$unitdir/obied.service.tmp"
chmod 0644 "$unitdir/obied.service.tmp"
mv -f "$unitdir/obied.service.tmp" "$unitdir/obied.service"
log "installed $unitdir/obied.service"

if [ -d "$f2bdir" ]; then
	install -m 0644 "$src/fail2ban/action.d/obie.conf" "$f2bdir/obie.conf"
	log "installed the Fail2Ban action $f2bdir/obie.conf (override it in obie.local)"
else
	log "Fail2Ban not found; its action is in $src/fail2ban/action.d/obie.conf"
fi

# Only where systemd runs: in an image build or a container without it,
# daemon-reload would fail after everything is installed.
if [ -z "$DESTDIR" ] && [ -d /run/systemd/system ]; then
	systemctl daemon-reload
	if systemctl is-active --quiet obied; then
		log "obied is running; restart it to use the new version: systemctl restart obied"
		exit 0
	fi
fi

cat <<EOF
install.sh: done. Next steps:
  1. Answer a few questions to write /etc/obie/obie.yaml:
       $PREFIX/bin/obied setup
     (or review the file yourself and check it:
       $PREFIX/bin/obied --config /etc/obie/obie.yaml --check-config)
  2. Start the node:  systemctl enable --now obied
  3. Let it check itself; every problem comes with the next step:
       $PREFIX/bin/obied self-check
  4. Use obiectl as root or as a member of the group obie:
     usermod -aG obie <user>; obiectl status
EOF
