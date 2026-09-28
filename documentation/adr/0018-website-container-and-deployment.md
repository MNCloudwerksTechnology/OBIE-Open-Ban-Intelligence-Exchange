# ADR 0018: Website container image and deployment

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1679](https://openproject.niew.dev/work_packages/1679)
- **Builds on:** [ADR 0010](0010-website-stack-and-build.md) (one jar),
  [ADR 0017](0017-packaging-and-state-format.md) (release workflow pattern)

## Context

The website is one Spring Boot jar that needs PostgreSQL and an SMTP server
(ADR 0010, ADR 0012). The operator runs it on his own server behind a
TLS-terminating reverse proxy and wants to put it live with one compose file
and one environment file. The container must be small in privilege (non-root,
read-only root filesystem) and in memory (512 MB), and publishing an image
must be a deliberate act, never a side effect of a push.

## Decision

- **Multi-stage `website/Dockerfile`, context `website/`.** Stage 1 builds
  the front end (`node:24-alpine`, `npm ci`, `npm run build`), stage 2 the
  jar (`eclipse-temurin:21-jdk-alpine`, `./mvnw package` without tests:
  `make -C website ci` runs them) and extracts it into Spring Boot's layers
  (`-Djarmode=tools extract --layers --launcher`). Both run on the build
  platform, because their output is platform-independent; only the runtime
  stage is per target platform, so multi-arch builds do not emulate the
  build.
- **Runtime `eclipse-temurin:21-jre-alpine`.** A JRE instead of a jlinked
  runtime: smaller maintenance, security updates by rebuilding. Alpine keeps
  busybox `wget` for the `HEALTHCHECK` on `/api/health`; distroless would
  need a JVM-based health probe. The application runs as `obie` (UID
  10001), its files belong to root, and it writes only Tomcat's work files
  to `/tmp`, so it runs with a read-only root filesystem and a tmpfs on
  `/tmp`.
- **JVM flags in `JDK_JAVA_OPTIONS`** (replaceable without a new image),
  sized for a 512 MB limit: `MaxRAMPercentage=50`, capped metaspace (160 MB),
  code cache (64 MB) and direct memory (32 MB), 512 KB thread stacks, the
  serial collector, `ExitOnOutOfMemoryError` (Docker restarts it) and no
  perf data file. The smoke test measures about 270 MB in use.
- **`website/deploy/compose.yaml`**: the website and `postgres:16-alpine`
  (named volume, `pg_isready` health check, no published port). The
  website gets the operator's `.env` as `env_file`, so an unset optional
  variable keeps the application's default instead of being duplicated in
  the compose file; required variables are also interpolated with `:?` so
  `docker compose` refuses to start without them. It publishes on
  `127.0.0.1:8080` by default and runs read-only, without capabilities,
  with `no-new-privileges` and `mem_limit: 512m`. Mailpit joins only in the
  `test` profile.
- **Forwarded headers.** Compose sets `SERVER_FORWARD_HEADERS_STRATEGY=native`:
  Tomcat's `RemoteIpValve` takes `X-Forwarded-For`/`-Proto` only from
  internal-proxy (private and loopback) addresses, which is where any proxy
  in front of the container is, and never from a visitor connecting
  directly over a public address. Any proxy that sets those headers works;
  the deployment README shows Caddy and Traefik.
- **Release on `website-v*` tags only.** `website-release.yml`,
  byte-identical in `.gitea/workflows/` and `.github/workflows/` like the
  other workflows, runs `make -C website ci` and the smoke test, then pushes
  the multi-arch image (linux/amd64, linux/arm64) to
  `<registry>/<owner>/obie-website:<version>` (`ghcr.io` on GitHub) and
  `:latest` for versions without a pre-release suffix. The node's `v*`
  release does not match these tags, and this workflow does not match the
  node's. The CI `website` job builds the image and runs the smoke test
  (`make -C website smoke`) but never pushes.
- **Smoke test** (`website/deploy/smoke-test.sh`) runs the real compose file
  with the `test` profile and a throwaway environment file on random local
  ports, and checks page, health, headers, container hardening, forwarded
  headers and one inquiry end to end (PostgreSQL row, mail in Mailpit).

## Consequences

- Founder content and legal texts are compiled into the image; changing
  them means a new image (a tag, or a local `make -C website image`).
- The CI `website` job needs Docker with Compose and builds the site twice
  (once for the tests, once in the image).
- Publishing the website is independent of releasing the node; each has its
  own tag prefix.
- PostgreSQL major upgrades are a manual dump and restore (documented in
  `website/deploy/README.md`).
