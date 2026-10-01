# Deploying the OBIE website

The website runs as two containers on one server: the website itself (the
image from [`../Dockerfile`](../Dockerfile)) and PostgreSQL. A reverse proxy
you already run, or add, terminates TLS in front of it. Everything is in this
directory:

| File | What it is |
|------|------------|
| `compose.yaml` | The website and PostgreSQL (named volume `db-data`, health check, no published port), plus Mailpit in the `test` profile. |
| `.env.example` | Every setting, with examples and defaults. Copy it to `.env` and fill it in. |
| `smoke-test.sh` | Starts the stack locally, sends one inquiry, checks it, removes the stack (`make -C website smoke`). |

Design decisions: [ADR 0018](../../documentation/adr/0018-website-container-and-deployment.md).
Each variable is explained in `.env.example` and in the
[configuration table](../README.md#configuration) of the website README.

## What the container does and needs

- Runs as the unprivileged user `obie` (UID 10001) with a read-only root
  filesystem, no capabilities and a 64 MB tmpfs on `/tmp` (Tomcat's work
  files), limited to 512 MB of memory. The JVM flags in the image
  (`JDK_JAVA_OPTIONS`) are sized for that limit; the site uses about 270 MB.
- Listens on port 8080; `OBIE_HTTP_BIND` publishes it, by default on
  `127.0.0.1:8080` only.
- Reports its health at `GET /api/health` (`{"status":"UP"}`); Docker's
  health check and `docker compose up --wait` use it.
- Writes no access log. It logs to stdout (`docker compose logs website`).
- Creates and migrates its database schema itself (Flyway) at startup.

## Before going live: checklist

Founder content and legal texts are part of the image, not of `.env`: they
live in the front end's content files and are compiled into the page. Change
them in the repository, then publish a new image (a `website-v*` tag, see
[Releases](#releases)) or build one yourself (`make -C website image
IMAGE=registry.example/obie-website:1.0.0`) and set `OBIE_IMAGE` to it.
Search the repository for `TODO(operator)` to find every open placeholder;
the [website README](../README.md) describes each field.

Every text exists in English and German (`*.content.ts` and
`*.content.de.ts`); fill in each placeholder in both.

- [ ] **Founder photo**: `founder.photo` in
      `website/frontend/src/app/content/landing.content.ts` and
      `landing.content.de.ts` (photo into `website/frontend/public/founder/`,
      a non-empty `alt` in each language).
- [ ] **Talk topics** confirmed or replaced: `founder.topics` in the same files.
- [ ] **Bio, name, role, links** checked: `founder.*` in the same files.
- [ ] **Privacy policy placeholder** in
      `website/frontend/src/app/content/legal.content.ts` and
      `legal.content.de.ts`: the e-mail (SMTP) provider (`privacy.inquiries`),
      which must process the e-mails within the EU/EEA. The other facts come
      from [cloudwerks.de/datenschutz](https://cloudwerks.de/datenschutz).
- [ ] **Matomo** (site 5 on `metrics.cloudwerks.de`): old raw data deleted
      after 14 months, as the policy states; tracking accepted only for the
      site's own URL (see the
      [website README](../README.md#visitor-statistics-matomo)).
- [ ] **Impressum** checked against the company data in the same files.
- [ ] **Legal review done**, including the German texts: `reviewPending:
      false` in `legal.content.ts` and `legal.content.de.ts` removes the
      review notice from `/impressum`, `/privacy`, `/de/impressum` and
      `/de/datenschutz`.
- [ ] **Inquiry retention** in the policy matches `OBIE_INQUIRY_RETENTION`
      (default 12 months).
- [ ] **Log retention**: the reverse proxy's access log and the container
      logs are deleted after 7 days, as the privacy policy says (see
      [Logs](#logs)).
- [ ] **`.env` filled in**: every `REQUIRED` value, real SMTP credentials,
      fresh random secrets, `OBIE_IMAGE` pinned to a version.
- [ ] **A test inquiry** from the live site reaches `OBIE_INQUIRY_RECIPIENT`.
- [ ] **Backups** scheduled and one restore tried (see [Backups](#backups)).

## First deploy

Requirements: a Linux server with Docker Engine and the Compose plugin
(`docker compose`), a DNS name pointing at it, a reverse proxy that
terminates TLS (examples below), and an SMTP account that may send as
`OBIE_MAIL_FROM`.

1. Copy `compose.yaml` and `.env.example` to a directory on the server, e.g.
   `/opt/obie-website/`.
2. Create the environment file and fill it in:

   ```sh
   cd /opt/obie-website
   cp .env.example .env
   chmod 600 .env
   openssl rand -base64 32   # once for OBIE_DB_PASSWORD, once for OBIE_INQUIRY_SECRET
   $EDITOR .env
   ```

   Required: `OBIE_SITE_ORIGIN`, `OBIE_DB_PASSWORD`, `OBIE_SMTP_HOST`,
   `OBIE_MAIL_FROM`, `OBIE_INQUIRY_RECIPIENT`, `OBIE_INQUIRY_SECRET`;
   `docker compose` refuses to start while one of them is missing. Leave
   optional settings commented out rather than empty.
3. Start it and wait until both containers are healthy:

   ```sh
   docker compose pull
   docker compose up -d --wait
   curl -fsS http://127.0.0.1:8080/api/health   # {"status":"UP"}
   ```

   The GitHub Container Registry package must be public, or log in first
   with `docker login ghcr.io`.
4. Point the reverse proxy at `127.0.0.1:8080` (next section) and open
   `OBIE_SITE_ORIGIN` in a browser.
5. Send a test inquiry through the form and check that it arrives. Mail
   problems show up in `docker compose logs website`; failed deliveries are
   retried with backoff.

## Reverse proxy

The website speaks plain HTTP and expects a proxy in front that terminates
TLS and sets `X-Forwarded-For` and `X-Forwarded-Proto` (Caddy and Traefik
do both by default). `compose.yaml` sets
`SERVER_FORWARD_HEADERS_STRATEGY=native`, so the rate limit and the stored IP
hash see the visitor's address instead of the proxy's. Tomcat accepts these
headers only from private and loopback addresses (10/8, 172.16/12,
192.168/16, 127/8, …), which is where the proxy is.

Do not publish port 8080 to the internet (`OBIE_HTTP_BIND=0.0.0.0:8080`):
visitors would bypass TLS, and connections Docker relays through its
gateway (e.g. over IPv6) could forge their address. The site sends HSTS
(`Strict-Transport-Security`), so serve it over HTTPS only.

### Caddy on the host

`/etc/caddy/Caddyfile`; Caddy obtains the certificate itself:

```caddyfile
obie.example {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8080

	# Access log, kept for 7 days as the privacy policy states.
	log {
		output file /var/log/caddy/obie-website.log {
			roll_keep_for 168h
		}
	}
}
```

`encode` is optional: the website compresses its responses itself.

### Traefik (Docker provider)

Traefik runs in its own compose project with a `websecure` entry point on
port 443 and a certificate resolver (`letsencrypt` below) and watches the
Docker socket. Put the website on Traefik's network with a
`compose.override.yaml` next to `compose.yaml`; `docker compose` reads it
automatically (`!reset` needs Compose 2.24 or newer):

```yaml
services:
  website:
    # Reached through Traefik only; nothing is published on the host.
    ports: !reset []
    networks: [default, traefik]
    labels:
      traefik.enable: "true"
      traefik.docker.network: traefik
      traefik.http.routers.obie-website.rule: Host(`obie.example`)
      traefik.http.routers.obie-website.entrypoints: websecure
      traefik.http.routers.obie-website.tls.certresolver: letsencrypt
      traefik.http.services.obie-website.loadbalancer.server.port: "8080"

networks:
  traefik:
    external: true
```

Traefik's container address on the `traefik` network is private, so the
forwarded headers are honoured. Keep Traefik's access log (if enabled) for
at most 7 days.

## Updating

1. Take a backup (next section).
2. Set `OBIE_IMAGE` in `.env` to the new version, then:

   ```sh
   docker compose pull
   docker compose up -d --wait
   docker compose ps          # both containers "healthy"
   ```

   Flyway migrates the database at startup. Migrations only go forward: to
   return to an older version after a migration, restore the backup taken
   before the update.
3. `docker image prune` removes the old image once the new one runs.

PostgreSQL stays on major version 16 (`postgres:16-alpine`); minor updates
arrive with `docker compose pull`. A new major version needs a dump and
restore (below) into a fresh volume.

## Backups

The database holds the inquiries. Back up the database and `.env` (without
`OBIE_INQUIRY_SECRET`, stored IP hashes can no longer be compared):

```sh
cd /opt/obie-website
mkdir -p backup
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > "backup/obie-website-$(date +%F).dump"
```

A daily cron job with the same command (plus deleting dumps older than your
backup period, so deleted inquiries do not live on in backups for longer
than the privacy policy allows) is enough for a site of this size. Copy the
dumps off the server.

Restore into the running stack (replaces the current data):

```sh
docker compose stop website
docker compose exec -T db sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' \
  < backup/obie-website-2026-01-31.dump
docker compose start website
```

## Logs

`docker compose logs website` shows the application's log. Docker's default
`json-file` driver keeps logs until the container is removed; to delete them
after 7 days as the privacy policy states, use the `journald` driver with a
7-day retention (`MaxRetentionSec=7day` in `journald.conf`) or rotate the
`json-file` logs by size (`max-size`, `max-file`) small enough for 7 days,
in `/etc/docker/daemon.json` or per service under `logging:`.

## Smoke test

`make -C website smoke` builds the image (`obie-website:dev`), starts this
stack with the `test` profile under the project name `obie-website-smoke`,
with a throwaway environment file and random local ports, and checks:
the landing page and its canonical origin, `/api/health`, the security
headers, the container's user, read-only root filesystem and health, that
`X-Forwarded-For` is honoured, and that one inquiry is stored and its mail
reaches Mailpit. It removes the stack with its volumes afterwards, also on
failure. `IMAGE=<image> website/deploy/smoke-test.sh` checks an existing
image without building. The CI `website` job runs it on every change under
`website/`.

## Releases

Pushing a tag `website-v<version>` (e.g. `website-v1.0.0`) runs
`.github/workflows/website-release.yml`: `make -C website ci`, the smoke
test, then the multi-arch image (linux/amd64, linux/arm64) is pushed to
`ghcr.io/<owner>/obie-website:<version>`, and to `:latest` unless the
version has a pre-release suffix (`1.0.0-rc.1`). Normal pushes and pull
requests never publish an image; the node's `v*` tags do not either.
