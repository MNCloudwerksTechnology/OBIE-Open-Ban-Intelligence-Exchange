#!/usr/bin/env bash
# Runs Lighthouse CI (mobile) against the production jar, started locally with
# a throwaway PostgreSQL container, and fails when a category scores below the
# thresholds in lighthouserc.json. Called by `make -C website lighthouse`.
#
# Environment: JAR (the built jar), LHCI_VERSION (pinned @lhci/cli version),
# LIGHTHOUSE_PORT (default 8089), CHROME_PATH (optional, the Chrome to use).
set -euo pipefail

cd "$(dirname "$0")/.."
: "${JAR:?JAR must point at the built jar}"
: "${LHCI_VERSION:?LHCI_VERSION must be set}"
PORT="${LIGHTHOUSE_PORT:-8089}"
ORIGIN="http://localhost:${PORT}"
DB_CONTAINER="obie-lighthouse-db-$$"
LOG="$(mktemp -t obie-lighthouse-jar.XXXXXX.log)"
JAR_PID=""

cleanup() {
  if [ -n "$JAR_PID" ]; then kill "$JAR_PID" 2>/dev/null || true; wait "$JAR_PID" 2>/dev/null || true; fi
  docker rm -f "$DB_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "lighthouse: starting PostgreSQL ($DB_CONTAINER)"
docker run -d --rm --name "$DB_CONTAINER" -e POSTGRES_PASSWORD=lighthouse \
  -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
for _ in $(seq 60); do
  docker exec "$DB_CONTAINER" pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1 && break
  sleep 1
done
DB_PORT="$(docker port "$DB_CONTAINER" 5432/tcp | head -n1 | sed 's/.*://')"

# Nothing listens on port 9 (discard): no mail is sent and GitHub is not
# asked, so the measurement depends on nothing outside this machine.
echo "lighthouse: starting $JAR on $ORIGIN (log: $LOG)"
OBIE_DB_URL="jdbc:postgresql://127.0.0.1:${DB_PORT}/postgres" \
OBIE_DB_USERNAME=postgres OBIE_DB_PASSWORD=lighthouse \
OBIE_SMTP_HOST=127.0.0.1 OBIE_SMTP_PORT=9 OBIE_SMTP_STARTTLS=false \
OBIE_MAIL_FROM=website@example.org OBIE_INQUIRY_RECIPIENT=operator@example.org \
OBIE_INQUIRY_SECRET="lighthouse-$(date +%s)-0123456789abcdefghijklmnopqrstuvwxyz" \
OBIE_SITE_ORIGIN="$ORIGIN" OBIE_GITHUB_API_URL=http://127.0.0.1:9 SERVER_PORT="$PORT" \
  java -jar "$JAR" >"$LOG" 2>&1 &
JAR_PID=$!
for _ in $(seq 90); do
  if curl -fsS "$ORIGIN/api/health" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$JAR_PID" 2>/dev/null; then
    echo "lighthouse: the jar exited during startup:" >&2; tail -n 50 "$LOG" >&2; exit 1
  fi
  sleep 1
done
curl -fsS "$ORIGIN/api/health" >/dev/null

npx --yes "@lhci/cli@${LHCI_VERSION}" autorun --config=lighthouserc.json \
  --collect.url="$ORIGIN/" --collect.url="$ORIGIN/impressum" --collect.url="$ORIGIN/privacy"
