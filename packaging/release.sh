#!/bin/sh
# Builds the OBIE release artefacts into <out-dir> (ADR 0017); run it
# through `make release VERSION=x.y.z`.
#
# For each platform in PLATFORMS:
#   obie-<version>-linux-<arch>.tar.gz          obied, obiectl, install.sh, the
#                                              example configuration, the systemd
#                                              unit, the Fail2Ban action, the
#                                              manual pages and shell completions
#                                              (share/), LICENSE and README
#   obie-<version>-linux-<arch>.<bin>.cdx.json  CycloneDX SBOM of each binary
# and SHA256SUMS over all of them.
#
# The artefacts are reproducible: the same commit, Go toolchain and
# SOURCE_DATE_EPOCH (default: the commit time) give byte-identical files.
# Binaries are static (CGO_ENABLED=0) and built with -trimpath and an empty
# build ID; tar entries are sorted, owned by 0:0 and dated SOURCE_DATE_EPOCH,
# and gzip stores no name or time. Needs GNU tar.
#
# A final VERSION (x.y.z) is refused unless documentation/capabilities.md,
# the capability overview, describes it: every release updates it.
#
# Environment: VERSION (required, x.y.z[-pre]), GO, CYCLONEDX_GOMOD,
# PLATFORMS (default "linux/amd64 linux/arm64"), SOURCE_DATE_EPOCH.
set -eu
# The staged files' modes must not depend on the caller's umask.
umask 022

out=${1:?usage: release.sh <out-dir>}
version=${VERSION:-}
go=${GO:-go}
cyclonedx=${CYCLONEDX_GOMOD:?CYCLONEDX_GOMOD must name the cyclonedx-gomod binary}
platforms=${PLATFORMS:-linux/amd64 linux/arm64}
module=github.com/MNCloudwerksTechnology/obie
root=$(cd "$(dirname "$0")/.." && pwd)

die() {
	echo "release.sh: $*" >&2
	exit 1
}

if ! echo "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then
	die "VERSION must look like 1.2.3 or 1.2.3-rc.1 (without a leading v), got '$version'"
fi
# Every release updates the capability overview for evaluators; a final
# release is only built once the overview names it. Pre-releases (and CI's
# 0.0.0-ci) are not checked.
case "$version" in
*-*) ;;
*)
	grep -qF "**OBIE $version**" "$root/documentation/capabilities.md" ||
		die "documentation/capabilities.md does not describe OBIE $version; update it for the release (CONTRIBUTING.md, Releasing)"
	;;
esac
tar --version 2>/dev/null | grep -q 'GNU tar' || die "GNU tar is required"
if [ -z "${SOURCE_DATE_EPOCH:-}" ]; then
	SOURCE_DATE_EPOCH=$(git -C "$root" log -1 --format=%ct)
fi
export SOURCE_DATE_EPOCH

rm -rf "$out"
mkdir -p "$out"
# The manual pages and shell completions (packaging/gendocs) are the same
# for every platform; they are generated once, for the build host.
share="$out/share"
(cd "$root" && "$go" run ./packaging/gendocs -out "$share" -version "$version")
for platform in $platforms; do
	goos=${platform%/*}
	goarch=${platform#*/}
	name="obie-$version-$goos-$goarch"
	stage="$out/$name"
	echo "release.sh: building $name"
	mkdir -p "$stage/bin" "$stage/etc" "$stage/systemd" "$stage/fail2ban/action.d"
	for cmd in obied obiectl; do
		(cd "$root" && CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch "$go" build -trimpath \
			-ldflags "-s -w -buildid= -X $module/internal/version.Version=$version" \
			-o "$stage/bin/$cmd" "./cmd/$cmd")
		# The SBOM is read from the binary's embedded build information, so
		# it lists exactly the modules linked into it.
		GOOS=$goos GOARCH=$goarch "$cyclonedx" bin -json -std -noserial -notimestamp \
			-version "$version" -output "$out/$name.$cmd.cdx.json" "$stage/bin/$cmd"
	done
	install -m 0755 "$root/packaging/install.sh" "$stage/install.sh"
	install -m 0644 "$root/documentation/examples/obie.yaml" "$stage/etc/obie.yaml"
	install -m 0644 "$root/packaging/systemd/obied.service" "$stage/systemd/obied.service"
	install -m 0644 "$root/contrib/fail2ban/action.d/obie.conf" "$stage/fail2ban/action.d/obie.conf"
	install -m 0644 "$root/LICENSE.md" "$stage/LICENSE.md"
	install -m 0644 "$root/README.md" "$stage/README.md"
	cp -R "$share" "$stage/share"
	# No pipe: sh has no pipefail, and a failing tar must fail the release.
	tar --sort=name --format=gnu --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
		--mode='go-w' -C "$out" -cf "$out/$name.tar" "$name"
	gzip -9 -n "$out/$name.tar"
	rm -rf "$stage"
done
rm -rf "$share"

(cd "$out" && sha256sum -- *.tar.gz *.cdx.json >SHA256SUMS)
echo "release.sh: artefacts in $out:"
cat "$out/SHA256SUMS"
