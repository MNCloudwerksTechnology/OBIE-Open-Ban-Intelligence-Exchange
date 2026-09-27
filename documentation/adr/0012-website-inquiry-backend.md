# ADR 0012: Inquiry back end of the website

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1674](https://openproject.niew.dev/work_packages/1674)
- **Amends:** [ADR 0010](0010-website-stack-and-build.md) (back-end starters)

## Context

The website's inquiry form (epic #1671) must reach the operator reliably and
keep spam out, without third-party CAPTCHAs and without tracking visitors.
ADR 0010 limited the back end to the `web` and `actuator` starters; storing
and mailing inquiries needs more. An inquiry must never be lost because the
mail server is down.

## Decision

- **New dependencies:** `spring-boot-starter-validation` (Bean Validation),
  `spring-boot-starter-data-jpa` with the PostgreSQL driver, Flyway
  (`flyway-core`, `flyway-database-postgresql`) for the schema, and
  `spring-boot-starter-mail` for SMTP. Tests use Testcontainers (PostgreSQL)
  and GreenMail (an in-process SMTP server). Every Spring Boot test runs
  against a real PostgreSQL in a container; Docker is required to run
  `./mvnw verify`.
- **No Spring Security.** The API has no users, sessions or cookies, so
  there is nothing to authenticate and no CSRF surface. Security headers and
  the body size limit are plain servlet filters; CORS uses Spring MVC's CORS
  support, restricted to the site's own origin.
- **Store first, mail later (outbox).** `POST /api/inquiries` only validates
  and stores the inquiry (status `NEW`) and answers `202 {"id": …}`. The
  inquiry row carries its own mail state (notification sent, confirmation
  sent, attempts, next attempt). A scheduled dispatcher sends due mails and,
  on failure, logs the error and schedules the next attempt with exponential
  backoff up to a maximum number of attempts. The inquiry survives mail
  outages and restarts; no separate queue or retry library is needed.
- **Abuse protection without third parties:** a honeypot field and a minimum
  fill time (both answered with an indistinguishable fake `202`, nothing
  stored), a per-IP token bucket in memory (`429`), and a 16 KiB request body
  limit (`413`). The rate limit is per instance, which is enough for a single
  container.
- **Server-signed form-render timestamp.** The form-render timestamp is not
  taken from the visitor's clock: the front end fetches a token
  (`GET /api/inquiries/form-token`, `<epoch millis>.<HMAC>`) when it shows the
  form and sends it back as `formToken`. A visitor whose clock is ahead can
  therefore not be mistaken for a bot and silently lose an inquiry, and bots
  cannot forge an old timestamp. A missing token is a validation error (400),
  so a broken front end is noticed; a token with a wrong signature is treated
  as a bot.
- **Privacy:** the client IP is stored only as an HMAC-SHA-256 with a
  server-side salt (never in clear). Inquiries older than a configurable
  period (default 12 months) are deleted by a scheduled job.
- **CSP without `unsafe-inline` for scripts:** the prerendered pages contain
  inline scripts written by the Angular build (e.g. the critical-CSS loader).
  At startup the back end hashes every inline script of the packaged HTML and
  allows exactly those hashes in `script-src`, so front-end changes need no
  back-end change. Styles keep `'unsafe-inline'` because Angular inlines
  component and critical styles.
- **Configuration:** all deployment settings come from `OBIE_*` environment
  variables mapped in `application.properties`; the recipient address is
  never hard-coded. `website/README.md` lists them.

## Alternatives considered

- **Sending mail inside the request, with Spring Retry** — a slow or broken
  SMTP server would slow down or fail submissions, and retries in memory are
  lost on restart. Rejected in favour of the outbox.
- **Spring Security for headers and CORS** — brings authentication defaults
  (generated password, login page, CSRF) that must all be switched off for a
  cookie-less API. Rejected as unnecessary weight.
- **Bucket4j for rate limiting** — a small token bucket is a few lines;
  a dependency is not worth it.
- **H2 for tests** — differs from PostgreSQL in SQL and types; the work
  package asks for Testcontainers.

## Consequences

- Running the jar needs a PostgreSQL database and SMTP settings; `make run`
  needs the environment variables from `website/README.md`.
- `./mvnw verify` and CI need Docker for Testcontainers.
- Behind a reverse proxy, the client IP (rate limit, hash) comes from the
  forwarded headers only when `SERVER_FORWARD_HEADERS_STRATEGY` is set;
  otherwise all visitors share the proxy's IP and its rate limit.
- The mail dispatcher and the rate limiter assume a single instance; running
  several would need row locking (`FOR UPDATE SKIP LOCKED`) and a shared
  limiter.
