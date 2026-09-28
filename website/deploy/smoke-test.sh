#!/bin/sh
# Smoke test of the production compose stack: starts compose.yaml with the
# `test` profile (Mailpit catches the mails) under its own project name and a
# throwaway environment file, checks the page, the health endpoint, the
# security headers, the container hardening and the forwarded headers,
# submits one inquiry and waits for its mail in Mailpit, then removes the
# stack with its volumes. Needs docker (with compose) and curl.
#
# Usage: IMAGE=obie-website:dev smoke-test.sh   (make -C website smoke)
set -eu

cd "$(dirname "$0")"
image=${IMAGE:-obie-website:dev}
project=${PROJECT:-obie-website-smoke}
timeout=${TIMEOUT:-90}
envfile=$(mktemp -t obie-website-smoke.XXXXXX)
compose() { docker compose -p "$project" --env-file "$envfile" --profile test "$@"; }

cleanup() {
	status=$?
	trap - EXIT INT TERM
	if [ "$status" -ne 0 ]; then
		compose logs --no-color --tail 80 >&2 || true
	fi
	compose down -v --remove-orphans >/dev/null 2>&1 || true
	rm -f "$envfile"
	exit "$status"
}
trap cleanup EXIT INT TERM

fail() {
	echo "smoke-test: FAILED: $*" >&2
	exit 1
}

ok() { echo "smoke-test: ok: $*"; }

# until_ok <description> <command...> retries the command every second until
# it succeeds or $timeout seconds have passed.
until_ok() {
	what=$1
	shift
	i=0
	until "$@" >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt "$timeout" ] || fail "$what within ${timeout}s"
		sleep 1
	done
	ok "$what"
}

# Random host ports on 127.0.0.1, Mailpit as the SMTP server, GitHub pointed
# at a closed local port (nothing leaves the machine), a quick minimum fill
# time and a rate limit of 2 so the forwarded-header check needs few requests.
cat >"$envfile" <<EOF
OBIE_ENV_FILE=$envfile
OBIE_IMAGE=$image
OBIE_HTTP_BIND=127.0.0.1:
MAILPIT_BIND=127.0.0.1:
OBIE_SITE_ORIGIN=https://obie.example
OBIE_DB_PASSWORD=smoke-$(date +%s)
OBIE_SMTP_HOST=mailpit
OBIE_SMTP_PORT=1025
OBIE_SMTP_STARTTLS=false
OBIE_MAIL_FROM=website@obie.example
OBIE_INQUIRY_RECIPIENT=operator@obie.example
OBIE_INQUIRY_SECRET=smoke-$(date +%s)-0123456789abcdefghijklmnopqrstuvwxyz
OBIE_INQUIRY_MIN_FILL_TIME=PT1S
OBIE_INQUIRY_RATE_LIMIT=2
OBIE_GITHUB_API_URL=http://127.0.0.1:9
EOF

compose up -d --wait --wait-timeout "$timeout"
ok "stack is up and healthy"
site="http://$(compose port website 8080)"
mailpit="http://$(compose port mailpit 8025)"

# Health, page and headers.
curl -fsS "$site/api/health" | grep -q '"status":"UP"' || fail "GET /api/health is not UP"
ok "GET /api/health is UP"
page=$(curl -fsS "$site/")
echo "$page" | grep -q '<title>[^<]*OBIE' || fail "GET / does not return the landing page"
echo "$page" | grep -q 'href="https://obie.example/"' || fail "GET / has no canonical URL on OBIE_SITE_ORIGIN"
ok "GET / returns the landing page"
headers=$(curl -fsS -o /dev/null -D - "$site/")
for header in 'content-security-policy: default-src' 'strict-transport-security: max-age=' \
	'x-content-type-options: nosniff' 'x-frame-options: deny' 'referrer-policy: no-referrer'; do
	echo "$headers" | tr '[:upper:]' '[:lower:]' | grep -q "^$header" || fail "GET / lacks the header $header"
done
ok "GET / sends the security headers"

# The container runs as described in the Dockerfile and compose.yaml.
container=$(compose ps -q website)
[ "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$container")" = true ] || fail "root filesystem is writable"
[ "$(docker inspect -f '{{.Config.User}}' "$container")" = 10001:10001 ] || fail "website does not run as 10001"
[ "$(docker inspect -f '{{.State.Health.Status}}' "$container")" = healthy ] || fail "health check is not healthy"
ok "website runs as 10001 on a read-only root filesystem, healthy"

# Forwarded headers: the rate limit (2) counts per X-Forwarded-For address,
# because the requests arrive from the Docker gateway, a private address.
post_status() {
	curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
		-H "X-Forwarded-For: $1" -H 'X-Forwarded-Proto: https' --data "$2" "$site/api/inquiries"
}
for expected in 400 400 429; do
	[ "$(post_status 203.0.113.10 '{}')" = "$expected" ] || fail "rate limit per forwarded address: expected $expected"
done
ok "X-Forwarded-For is honoured (203.0.113.10 is rate-limited)"

# One inquiry from another visitor, sent after the minimum fill time.
token=$(curl -fsS "$site/api/inquiries/form-token" | sed -n 's/.*"token" *: *"\([^"]*\)".*/\1/p')
[ -n "$token" ] || fail "GET /api/inquiries/form-token returned no token"
sleep 2
name="Smoke Test $$"
inquiry=$(printf '{"type":"talk","name":"%s","email":"visitor@example.org","message":"%s","consent":true,"website":"","formToken":"%s"}' \
	"$name" "Would you give a talk about OBIE at our meetup next spring?" "$token")
[ "$(post_status 198.51.100.20 "$inquiry")" = 202 ] || fail "POST /api/inquiries was not accepted"
ok "POST /api/inquiries accepted (202)"

mail_arrived() {
	curl -fsS "$mailpit/api/v1/messages" | grep -q "\[OBIE inquiry\] talk from $name"
}
until_ok "the inquiry mail reaches Mailpit" mail_arrived
curl -fsS "$mailpit/api/v1/messages" | grep -q '"Address":"operator@obie.example"' ||
	fail "the inquiry mail is not addressed to OBIE_INQUIRY_RECIPIENT"
stored=$(compose exec -T db psql -U obie -d obie -tAc "select count(*) from inquiry where name = '$name'")
[ "$stored" = 1 ] || fail "the inquiry is not stored in PostgreSQL"
ok "the inquiry is stored and mailed to operator@obie.example"

echo "smoke-test: website memory: $(docker stats --no-stream --format '{{.MemUsage}}' "$container")"
echo "smoke-test: PASSED"
