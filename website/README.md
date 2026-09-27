# OBIE website

The public website of OBIE: an Angular front end, prerendered to static HTML
at build time, served together with a small API by a Spring Boot back end. The
production build is a single jar. The website is independent of the OBIE node;
design decisions are recorded in
[ADR 0010](../documentation/adr/0010-website-stack-and-build.md).

```text
website/
  Makefile    build and check commands (this file documents them)
  frontend/   Angular 22, standalone components, strict TypeScript, SCSS
  backend/    Spring Boot 3.5, Java 21, Maven wrapper, package org.obie.website
```

## Prerequisites

- Node.js 24 with npm
- Java 21 (no Maven needed: the back end ships the Maven wrapper `./mvnw`)
- Go (only to install the pinned `osv-scanner` for `make vuln`)

## Build and run

```sh
make -C website            # build website/backend/target/obie-website.jar
make -C website run        # java -jar …/obie-website.jar → http://localhost:8080
```

`GET /` returns the prerendered home page, `GET /api/health` returns
`{"status":"UP"}`. Unknown URLs return HTTP 404 with the prerendered
not-found page; there is no fallback to the index page.

For front-end work, `npm start` in `frontend/` runs the Angular dev server on
http://localhost:4200.

## Commands

| Command                        | What it does                                                        |
|--------------------------------|---------------------------------------------------------------------|
| `make -C website ci`           | Every check the CI `website` job runs; must pass before committing |
| `make -C website frontend-lint`| ESLint (angular-eslint) and Prettier check                          |
| `make -C website frontend-test`| Front-end unit tests (Vitest)                                       |
| `make -C website frontend`     | Production build, all public routes prerendered (`frontend/dist/`) |
| `make -C website backend`      | `./mvnw verify`: tests incl. smoke test, Spotless, SpotBugs, jar    |
| `make -C website vuln`         | `npm audit --omit=dev --audit-level=high` and osv-scanner on the back end's runtime SBOM |
| `make -C website run`          | Run the built jar                                                   |
| `make -C website clean`        | Remove build output and installed tools                             |

`make -C website backend` builds the front end first, because the jar
contains it; running `./mvnw verify` directly fails with a hint if
`frontend/dist/` is missing. Fix formatting with `npm run format` (front end)
and `./mvnw spotless:apply` (back end).

## How it fits together

- **Front end.** Angular builds in `outputMode: "static"`: every route listed
  in `src/app/app.routes.server.ts` with `RenderMode.Prerender` is rendered to
  `<route>/index.html` at build time and hydrated in the browser. There is no
  Node server in production. Styling uses a small design system of CSS custom
  properties in `src/styles.scss` (colour tokens for a light and a dark theme,
  a 4-pt spacing scale) and no component library. Inter and Source Code Pro
  are self-hosted from `@fontsource` packages, so the browser makes no
  third-party requests ([ADR 0012](../documentation/adr/0012-landing-page-content-and-design-system.md)).
- **Landing page copy.** Every user-visible string of the landing page lives
  in `src/app/content/landing.content.ts`; templates only bind to it. Edit
  copy there. A German version is a second `LandingContent` object provided
  through the `LANDING_CONTENT` token. Describe only what the code does;
  label everything else "in progress" or "planned".
- **Back end.** Maven copies `frontend/dist/frontend/browser` into the jar as
  `classpath:/static/`. `StaticSiteConfig` serves files and prerendered
  routes; `NotFoundPageResolver` renders the 404 page. API endpoints live
  under `/api/**`; Actuator's health endpoint is the only one exposed
  (`/api/health`).
- **Adding a page.** Add the route to `src/app/app.routes.ts` and a
  `RenderMode.Prerender` entry to `src/app/app.routes.server.ts`. The back
  end serves it without changes.
- **Dependency overrides.** `backend/pom.xml` overrides a few versions
  managed by Spring Boot to pick up security fixes; remove them when Spring
  Boot catches up.
