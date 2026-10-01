#!/bin/sh
# Publishes the artefacts of `make release` as the release of a tag on the
# forge the release workflow runs on: Gitea (GITEA_ACTIONS=true) or GitHub.
# Re-running it for the same tag reuses the release and uploads only the
# files it does not have yet.
#
# Usage: publish-release.sh <dir>
# Environment: TOKEN, SERVER_URL, REPO (owner/name), TAG (vX.Y.Z[-pre]);
# on GitHub also GITHUB_API_URL. Needs curl and jq.
set -eu

dir=${1:?usage: publish-release.sh <dir>}
: "${TOKEN:?}" "${SERVER_URL:?}" "${REPO:?}" "${TAG:?}"

if [ "${GITEA_ACTIONS:-}" = true ]; then
	api="$SERVER_URL/api/v1"
else
	api=${GITHUB_API_URL:-https://api.github.com}
fi

# request <method> <url> [curl args...] prints the response body and fails
# on an HTTP error.
request() {
	method=$1
	url=$2
	shift 2
	curl --fail-with-body --silent --show-error --location -X "$method" \
		-H "Authorization: token $TOKEN" -H "Accept: application/json" "$@" "$url"
}

prerelease=false
case "$TAG" in *-*) prerelease=true ;; esac

# Only a 404 means there is no release yet; any other failure stops here.
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
status=$(curl --silent --show-error --location -o "$tmp" -w '%{http_code}' \
	-H "Authorization: token $TOKEN" -H "Accept: application/json" "$api/repos/$REPO/releases/tags/$TAG")
release=$(cat "$tmp")
case "$status" in
200) ;;
404)
	body=$(jq -n --arg tag "$TAG" --argjson pre "$prerelease" '{
		tag_name: $tag, name: ("OBIE " + $tag), prerelease: $pre,
		body: "Static linux/amd64 and linux/arm64 builds of obied and obiectl with install.sh, the systemd unit, the example configuration and the Fail2Ban action; a CycloneDX SBOM per binary. Verify the downloads with `sha256sum -c SHA256SUMS`. See documentation/operations/install.md."
	}')
	release=$(request POST "$api/repos/$REPO/releases" -H "Content-Type: application/json" -d "$body")
	echo "publish-release: created release $TAG"
	;;
*)
	echo "publish-release: looking up the release of $TAG failed with HTTP $status: $release" >&2
	exit 1
	;;
esac
id=$(echo "$release" | jq -r .id)
upload_url=$(echo "$release" | jq -r '.upload_url // empty' | sed 's/{.*}//')

for file in "$dir"/*.tar.gz "$dir"/*.cdx.json "$dir"/SHA256SUMS; do
	name=$(basename "$file")
	if echo "$release" | jq -e --arg n "$name" '.assets[]? | select(.name == $n)' >/dev/null; then
		echo "publish-release: $name already attached"
		continue
	fi
	if [ "${GITEA_ACTIONS:-}" = true ]; then
		request POST "$api/repos/$REPO/releases/$id/assets?name=$name" -F "attachment=@$file" >/dev/null
	else
		request POST "$upload_url?name=$name" -H "Content-Type: application/octet-stream" \
			--data-binary "@$file" >/dev/null
	fi
	echo "publish-release: attached $name"
done
