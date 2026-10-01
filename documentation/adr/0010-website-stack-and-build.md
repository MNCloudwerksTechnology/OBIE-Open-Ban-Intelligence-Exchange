# ADR 0010: Website stack and single-artefact build

- **Status:** Accepted (font clause superseded by [ADR 0012](0012-landing-page-content-and-design-system.md))
- **Date:** 2026-09-28
- **Work package:** [#1672](https://openproject.niew.dev/work_packages/1672)

## Context

OBIE needs a public landing page (epic #1671): plain-language explanation,
calls to action to the repository, an inquiry form with a back end, legal
pages, Lighthouse ≥ 95 and no third-party requests from the visitor's
browser. The page must be fully rendered HTML for search engines and visitors
without JavaScript. The website is a separate product from the Go node and
must not change how the node is built or checked.

## Decision

- **Location:** everything lives in `website/` (`frontend/`, `backend/`,
  `Makefile`, `README.md`). Nothing in the node's Go module, root `Makefile`
  or root `make ci` depends on it; `go.mod` ignores `./website` so npm
  packages that ship Go files never reach `./...` (lint, vet, tests).
- **Front end:** Angular 22 (standalone components, strict TypeScript, SCSS),
  built with `@angular/build:application` in **`outputMode: "static"`**:
  every public route is prerendered at build time (SSG) and hydrated in the
  browser. There is no Node server at runtime. Linting with angular-eslint
  and Prettier, unit tests with the Angular CLI's Vitest runner. No UI
  component library: a small hand-built design system (CSS custom properties
  in `src/styles.scss`, system fonts only) is enough for a landing page and
  keeps the page free of third-party requests.
- **Back end:** Spring Boot 3.5 on Java 21, Maven with the committed wrapper
  (`./mvnw`, script-only, checksum-pinned distribution), package
  `org.obie.website`. Starters: `web` and `actuator` only.
- **One artefact:** Maven packages the prerendered front end
  (`frontend/dist/frontend/browser`) as `classpath:/static/` into
  `obie-website.jar`; the build fails early if it is missing. The jar serves
  the site and the API under `/api/**`.
- **Routing:** a prerendered route is served from `<route>/index.html`; there
  is **no fallback to the index page**. Unknown URLs get HTTP 404 with the
  prerendered not-found page (`/404`) for HTML clients and Spring Boot's JSON
  error for API clients.
- **Health:** Actuator's base path is `/api`; only `health` is exposed
  (`GET /api/health`, no details, no discovery page).
- **Checks:** `make -C website ci` = front-end lint, unit tests, production
  build; `./mvnw verify` (tests incl. a full-application smoke test,
  Spotless with google-java-format, SpotBugs at max effort / low threshold);
  dependency scan: `npm audit --omit=dev --audit-level=high` for the front
  end and **osv-scanner** (pinned, installed via `go install`) over the
  runtime SBOM that Spring Boot's CycloneDX setup writes during the build. The
  back-end scan fails on any known vulnerability; versions managed by Spring
  Boot are overridden in `pom.xml` when a fix is only available upstream.
- **CI:** a `website` job in the existing workflow (`.gitea/workflows/ci.yml`,
  mirrored to `.github/workflows/ci.yml`) runs `make -C website ci` when
  something under `website/` changed and skips its steps otherwise, so it can
  be a required check.

## Alternatives considered

- **Angular SSR server (`outputMode: "server"`)** — needs a Node runtime in
  production next to the JVM; rejected, all public content is static.
- **Serving `index.html` for every unknown path (SPA fallback)** — soft 404s
  hurt search engines and hide broken links; rejected.
- **OWASP dependency-check** — needs a local copy of the NVD, which takes a
  long time to download without an API key and makes every CI run slow and
  flaky. osv-scanner queries OSV (which includes the GitHub advisories for
  Maven) and is already used on the build host. Rejected in favour of it.
- **Spring Boot 4** — the work package asks for the current 3.x line.
- **frontend-maven-plugin (Maven downloads Node and builds the front end)** —
  duplicates the host's Node 24 and slows every Maven run; the Makefile
  orders the two builds instead.

## Consequences

- `./mvnw verify` needs the front end built first (`make -C website frontend`
  or any target that depends on it).
- The security overrides in `pom.xml` must be revisited, and removed, when
  Spring Boot is upgraded.
- Later website work packages add routes to `app.routes.ts` and
  `app.routes.server.ts` (prerendered) and API controllers under
  `org.obie.website`; the back end serves new prerendered routes without
  changes.
