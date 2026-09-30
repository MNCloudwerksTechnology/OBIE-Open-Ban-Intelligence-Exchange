# OBIE website

The public website of OBIE: an Angular front end, prerendered to static HTML
at build time, served together with a small API by a Spring Boot back end. The
production build is a single jar. The website is independent of the OBIE node;
design decisions are recorded in
[ADR 0010](../documentation/adr/0010-website-stack-and-build.md).

```text
website/
  Makefile    build and check commands (this file documents them)
  Dockerfile  production container image (ADR 0018)
  frontend/   Angular 22, standalone components, strict TypeScript, SCSS
  backend/    Spring Boot 3.5, Java 21, Maven wrapper, package org.obie.website
  deploy/     compose file, .env.example and smoke test for the server
```

**Deploying:** [`deploy/README.md`](deploy/README.md) covers the first
deploy with Docker Compose, reverse proxies (Caddy, Traefik), updates,
backups and the checklist before going live.

## Prerequisites

- Node.js 24 with npm
- Java 21 (no Maven needed: the back end ships the Maven wrapper `./mvnw`)
- Go (only to install the pinned `osv-scanner` for `make vuln`)
- Docker (the back-end tests start PostgreSQL and, for the browser tests,
  Chromium with Testcontainers; `make image` and `make smoke` need Docker
  with the Compose plugin)
- Google Chrome, only for `make lighthouse` and `npm run share-image`
- To run the jar: a PostgreSQL database and an SMTP server (see
  [Configuration](#configuration))

## Build and run

```sh
make -C website            # build website/backend/target/obie-website.jar
make -C website run        # java -jar …/obie-website.jar → http://localhost:8080
```

`make run` needs the required environment variables from
[Configuration](#configuration); the application refuses to start without
them. For a local run, a throwaway database and mail catcher are enough:

```sh
docker run -d --name obie-db -p 5432:5432 -e POSTGRES_PASSWORD=obie postgres:16-alpine
docker run -d --name obie-mail -p 1025:1025 -p 8025:8025 mailhog/mailhog
export OBIE_DB_URL=jdbc:postgresql://localhost:5432/postgres OBIE_DB_USERNAME=postgres \
  OBIE_DB_PASSWORD=obie OBIE_SMTP_HOST=localhost OBIE_SMTP_PORT=1025 \
  OBIE_SMTP_STARTTLS=false OBIE_SITE_ORIGIN=http://localhost:8080 \
  OBIE_INQUIRY_RECIPIENT=me@example.org OBIE_MAIL_FROM=website@example.org \
  OBIE_INQUIRY_SECRET=$(openssl rand -base64 32)
make -C website run        # mails show up at http://localhost:8025
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
| `make -C website backend`      | `./mvnw verify`: tests (PostgreSQL and a Chromium browser via Testcontainers, GreenMail), Spotless, SpotBugs, jar |
| `make -C website vuln`         | `npm audit --omit=dev --audit-level=high` and osv-scanner on the back end's runtime SBOM |
| `make -C website lighthouse`   | Build the jar, start it with a throwaway PostgreSQL (Docker) and run Lighthouse CI (mobile) on every page; fails below 95 in any category |
| `make -C website image`        | Build the container image `obie-website:dev` (`VERSION=`, `IMAGE=` override) |
| `make -C website smoke`        | Build the image, start `deploy/compose.yaml` with Mailpit, check page, health, headers and one inquiry, remove it ([`deploy/README.md`](deploy/README.md#smoke-test)) |
| `make -C website run`          | Run the built jar                                                   |
| `make -C website clean`        | Remove build output and installed tools                             |

`make -C website backend` builds the front end first, because the jar
contains it; running `./mvnw verify` directly fails with a hint if
`frontend/dist/` is missing. Fix formatting with `npm run format` (front end)
and `./mvnw spotless:apply` (back end).

## Configuration

All settings come from environment variables. Required ones have no default.
Durations use ISO-8601 (`PT3S` = 3 seconds, `PT1H` = 1 hour), periods too
(`P12M` = 12 months).

| Variable | Required | Default | Meaning |
|----------|----------|---------|---------|
| `OBIE_DB_URL` | yes | – | JDBC URL of the PostgreSQL database, e.g. `jdbc:postgresql://db:5432/obie`. Flyway creates and migrates the schema at startup. |
| `OBIE_DB_USERNAME` | yes | – | Database user. |
| `OBIE_DB_PASSWORD` | yes | – | Database password. |
| `OBIE_SMTP_HOST` | yes | – | SMTP server for the inquiry mails. |
| `OBIE_SMTP_PORT` | no | `587` | SMTP port (`587` for STARTTLS, `465` for TLS). |
| `OBIE_SMTP_USERNAME` | no | – | SMTP user, if the server needs authentication. |
| `OBIE_SMTP_PASSWORD` | no | – | SMTP password. |
| `OBIE_SMTP_STARTTLS` | no | `true` | Use STARTTLS and refuse to send without it. Set to `false` only for a local mail catcher or together with `OBIE_SMTP_SSL`. |
| `OBIE_SMTP_SSL` | no | `false` | Connect with TLS from the start (port 465). |
| `OBIE_MAIL_FROM` | yes | – | Sender address of all mails, e.g. `website@obie.example`. It must be allowed to send through the SMTP server. |
| `OBIE_INQUIRY_RECIPIENT` | yes | – | Address that receives every inquiry. |
| `OBIE_INQUIRY_SECRET` | yes | – | Server-side secret, at least 32 characters (e.g. `openssl rand -base64 32`). It salts the hashes of client IPs and signs form tokens. Keep it stable: after a change, forms that were already open are treated as bots (fake `202`, nothing stored), and old IP hashes no longer match new ones. |
| `OBIE_SITE_ORIGIN` | yes | – | The site's public origin, e.g. `https://obie.example` (scheme, host and optional port only): the only origin allowed to call the API from a browser (CORS), and the base of the canonical URLs, the share image URL, `robots.txt` and `sitemap.xml`. |
| `OBIE_INQUIRY_MIN_FILL_TIME` | no | `PT3S` | Submissions sent faster than this after the form was rendered count as bots. |
| `OBIE_INQUIRY_FORM_TOKEN_MAX_AGE` | no | `P1D` | Forms rendered longer ago than this are rejected with 400 ("please reload the page"), so one token cannot be reused forever. |
| `OBIE_INQUIRY_RATE_LIMIT` | no | `5` | Inquiries allowed per client IP within `OBIE_INQUIRY_RATE_LIMIT_PERIOD`. |
| `OBIE_INQUIRY_RATE_LIMIT_PERIOD` | no | `PT1H` | Time in which a client's allowance refills completely. |
| `OBIE_INQUIRY_RETENTION` | no | `P12M` | Inquiries older than this are deleted (daily at 03:30 UTC). |
| `OBIE_MAIL_MAX_ATTEMPTS` | no | `10` | Delivery attempts per inquiry before the mails are given up (logged as an error; the inquiry stays stored). |
| `OBIE_MAIL_RETRY_INITIAL_DELAY` | no | `PT1M` | Delay after the first failed delivery; doubled after each further failure. |
| `OBIE_MAIL_RETRY_MAX_DELAY` | no | `PT6H` | Upper bound of the retry delay. |
| `OBIE_GITHUB_REPOSITORY` | no | `MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange` | `owner/name` of the repository whose stats `GET /api/project` shows. |
| `OBIE_GITHUB_TOKEN` | no | – | GitHub token for the stats requests (a fine-grained token with read-only access to public repositories is enough). Without one GitHub allows 60 requests per hour and IP, which the cache stays well below. |
| `OBIE_GITHUB_CACHE_TTL` | no | `PT15M` | How long fetched stats are served before GitHub is asked again. |
| `OBIE_GITHUB_RETRY_DELAY` | no | `PT1M` | After a failed fetch (error, timeout, rate limit), GitHub is not asked again before this has passed; meanwhile the last good stats are served. |
| `OBIE_GITHUB_API_URL` | no | `https://api.github.com` | Base URL of the GitHub REST API. |
| `SERVER_PORT` | no | `8080` | HTTP port (Spring Boot). |
| `SERVER_FORWARD_HEADERS_STRATEGY` | no | – (`native` in `deploy/compose.yaml`) | Set to `native` behind a reverse proxy that sets `X-Forwarded-For`/`X-Forwarded-Proto`, so rate limit and IP hash see the visitor's address. Tomcat then accepts the headers only from private and loopback addresses. Leave unset when visitors can reach the application directly from such addresses: they could forge the headers. |

## Founder content: what the operator fills in

The founder section and the inquiry form (`/#contact`) take all their text
from `frontend/src/app/content/landing.content.ts` (`founder` and
`contact`). Nothing about the founder is invented: every gap is marked
`TODO(operator)` in that file, and the page looks finished while the
placeholders are in place. Search for `TODO(operator)` to find them.

| Field (`founder.…`) | State | What to do |
|---------------------|-------|------------|
| `photo.src`, `photo.alt` | **TODO(operator): headshot.** A neutral placeholder avatar (`/founder/avatar-placeholder.svg`) with an empty `alt`. | Put a square photo (at least 240 × 240 px, JPEG or WebP) into `frontend/public/founder/`, set `src` to its path (e.g. `/founder/markus-niewerth.jpg`) and describe it in `alt`. The content test requires a non-empty `alt` once `src` is not the placeholder. |
| `topics` | **TODO(operator): confirm talk topics.** Four proposals derived from OBIE's principles, shown under "Proposed talk topics". | Confirm, edit or replace them (3 to 5 topics). Change `topicsHeading`/`topicsNote` if they are no longer proposals. |
| `name`, `role` | Supplied by the operator. | Change only if they change. |
| `bio` | Supplied and approved by the operator (at most 80 words, checked by a test). | Change only with the operator's approval; do not add claims. |
| `links` | LinkedIn and GitHub, supplied by the operator. | Optional: an empty list hides the links. |

The inquiry recipient is not content: it is `OBIE_INQUIRY_RECIPIENT` (see
[Configuration](#configuration)).

## Legal pages: what the operator fills in and reviews

`/impressum` (§ 5 DDG, § 18 MStV) and `/privacy` (Art. 13 GDPR) take all
their text from `frontend/src/app/content/legal.content.ts`; the footer links
to both, and the inquiry form's consent checkbox links to `/privacy`. The
pages are in English and show the German legal terms ("Impressum",
"Datenschutzerklärung", and each section's German heading) alongside. A
German version is a second `LegalContent` object provided through the
`LEGAL_CONTENT` token.

**Review banner.** While `reviewPending` is `true`, both pages open with a
notice that they must be reviewed by the operator before the site goes live.
After the review, set `reviewPending: false` in `legal.content.ts`; that one
flag removes the notice from both pages.

The company data was supplied by the operator (WP #1677) and is in the file
already. Nothing is invented: every gap is marked `TODO(operator)` and shows
on the page as it is. Search for `TODO(operator)` to find them.

| Field | State | What to do |
|-------|-------|------------|
| Impressum: provider, address, representative, phone, e-mail | Supplied by the operator: Cloudwerks Technology GmbH, Pottenort 15, 45891 Gelsenkirchen; Markus Niewerth. | Change only if they change. |
| Impressum: register entry, VAT ID | Supplied: Amtsgericht Gelsenkirchen, HRB 17839; DE363640900. | Change only if they change. |
| Impressum: responsible under § 18 Abs. 2 MStV | Supplied: Markus Niewerth. | Change only if it changes. |
| `privacy.hosting` | **TODO(operator): hosting provider.** | Name and address of the provider whose servers run the site and its database. |
| `privacy.inquiries` | **TODO(operator): e-mail (SMTP) provider** (the server behind `OBIE_SMTP_HOST`) and **how long answered inquiries stay in the mailbox**. | Name and address of the provider; your mailbox retention. |
| `privacy.third-countries` | **TODO(operator): transfers outside the EU/EEA.** | Confirm that the hosting and the e-mail provider process data only within the EU/EEA, or name the transfer and its safeguard. |

Keep the privacy policy true to the deployment and the code:

- **Inquiry retention.** The policy states `INQUIRY_RETENTION` (12 months),
  the default of `OBIE_INQUIRY_RETENTION`. A test checks it against the
  default; if the deployment sets another value, change `INQUIRY_RETENTION`.
- **Server logs.** The policy promises that access logs are kept for at most
  `SERVER_LOG_RETENTION` (7 days, the operator's setting). The application
  writes no access log; set the reverse proxy or web server in front of it,
  and the collection of the application's own log (stdout), to delete logs
  after 7 days.
- **Client IP.** Rate limit and IP hash use the visitor's address only if
  `SERVER_FORWARD_HEADERS_STRATEGY=native` is set behind a reverse proxy (see
  [Configuration](#configuration)); otherwise they see the proxy's address.
- **No cookies, no tracking, no third-party requests.** That is why the site
  has no cookie banner. Adding any of them (analytics, embedded videos,
  externally hosted fonts or scripts, anything stored in the browser) needs
  a new privacy assessment, likely a consent banner, and an updated policy.
- **New features that process personal data** (for example a new form
  field) need a matching change in the policy.

## The three-node demo: captions and numbers

"How it works" ends with a demo the visitor steps through: three servers,
a password-guessing bot, a web scanner, a rogue participant and an innocent
office address, in ten steps
([ADR 0028](../documentation/adr/0028-website-mesh-demo.md)). Its code is in
`frontend/src/app/sections/mesh-demo/`.

- **Wording.** Every caption, label and the settings sentence are in
  `landing.content.ts` under `howItWorks.demo`. Change the wording there;
  the demo's behaviour does not change. Tests keep each caption to at most
  40 words, the steps in the scenario's order and the numbers of the
  settings sentence in line with the scenario. Mark anything the current
  release does not do as `planned`.
- **Story and numbers.** `mesh-demo/scenario.ts` holds the servers' trust
  weights and settings, the example addresses and each step's events
  (detections, reports, the rogue's flood, a revocation). What every server
  shows is computed by `decision-rule.ts`, a copy of the node's decision
  rule (`internal/decision`): score = Σ weight × confidence over distinct
  reporters, threshold and quorum, local autoblock, safety list, expiry.
  `scenario.spec.ts` fixes who blocks and who watches at every step and
  checks the settings against `documentation/examples/obie.yaml` and the
  federation guide; it fails when the node's defaults change, so the demo
  is updated with them. A change to the rule in `internal/decision` has to
  be carried over to `decision-rule.ts` by hand.
- **In a talk.** Previous and Next (also the arrow keys while a control has
  focus), a button per step, Restart, and Play, which shows each step long
  enough to read it (`autoplayDelay`: 0.4 s per word, at least 8 s).
  Autoplay starts only on request and pauses on any step the presenter
  picks, at the last step, when the demo scrolls out of view and when the
  tab is hidden.
- **Without JavaScript, and for search engines,** the prerendered page shows
  the ten steps as an ordered list; the interactive demo offers the same list
  under "All ten steps as text". With reduced motion, steps change without
  animation. The demo stores nothing and sends no requests.
- **Checks.** `mesh-demo/*.spec.ts` (rule, scenario, component: navigation,
  every step's state, autoplay, reduced motion, the prerendered list, axe at
  every step) and, in a real browser under the production Content Security
  Policy, `MeshDemoBrowserTest` (no JavaScript, hydration without console
  errors, reduced motion, no sideways scrolling at 360 px, only same-origin
  requests, and a link below the demo staying on its target).
  Screenshots at 360 and 1440 px: [`docs/screenshots/wp-1759/`](docs/screenshots/wp-1759/).

## Inquiry API

The inquiry form talks to two endpoints. Errors are
[RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details
(`application/problem+json`) and never contain internal details.

1. **`GET /api/inquiries/form-token`** when the form is shown →
   `200 {"token": "…"}`. The token is a signed form-render timestamp.
2. **`POST /api/inquiries`** with `Content-Type: application/json`:

   | Field | Required | Rules |
   |-------|----------|-------|
   | `type` | yes | `talk`, `workshop`, `interview`, `collaboration` or `other` |
   | `name` | yes | at most 200 characters, one line |
   | `email` | yes | valid address, at most 254 characters |
   | `organisation` | no | at most 200 characters, one line |
   | `eventDate` | no | `YYYY-MM-DD`, in the future |
   | `eventLocation` | no | at most 200 characters, one line (`online` is fine) |
   | `audienceSize` | no | whole number, 1 to 1,000,000 |
   | `message` | yes | 20 to 5000 characters |
   | `consent` | yes | `true` (privacy notice accepted) |
   | `website` | no | honeypot: hide the field from people and send it empty |
   | `formToken` | yes | the token from step 1 |

   Responses:
   - `202 {"id": "…"}`: accepted. Bots (honeypot filled, invalid token,
     sent less than 3 s after the form was rendered) get the same answer, but
     nothing is stored or sent.
   - `400` with `errors: [{"field": "email", "message": "…"}]`: show each
     message next to its field. Unknown fields are errors, too.
   - `413`: body larger than 16 KiB. `429` with `Retry-After` (seconds): too
     many inquiries from this IP (IPv6: from this /64 network). Every
     request counts, including rejected ones.

An accepted inquiry is stored first (PostgreSQL, table `inquiry`; the client
IP only as a salted hash) and mailed afterwards: the operator gets
`[OBIE inquiry] <type> from <name>` with Reply-To set to the visitor, the
visitor a short plain-text confirmation that repeats nothing they typed. When
the SMTP server fails, delivery is retried with exponential backoff and each
failure is logged.

## Project API

**`GET /api/project`** returns the repository's live stats, which the back
end fetches from the GitHub REST API and caches (see
[Configuration](#configuration)); the visitor's browser never contacts
GitHub ([ADR 0014](../documentation/adr/0014-website-github-project-stats.md)).
It always answers `200`:

```json
{"available": true,
 "repositoryUrl": "https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange",
 "stars": 42, "forks": 7, "openIssues": 3,
 "latestRelease": {"tag": "v0.1.0", "publishedAt": "2026-09-01T12:00:00Z", "url": "https://github.com/…/releases/tag/v0.1.0"},
 "lastCommitAt": "2026-09-20T10:30:00Z"}
```

`latestRelease` is `null` while there is no release; `lastCommitAt` is the
newest commit on the default branch; `openIssues` is GitHub's count, which
includes pull requests. When GitHub fails or rate-limits, the last good
stats are served; before the first success the answer is
`{"available": false}` with every other field `null`, and the page leaves
the stats strip out. Failures are logged as warnings.

## Search engines, performance and accessibility

Design decisions: [ADR 0015](../documentation/adr/0015-website-seo-and-delivery.md).

- **Head tags.** `core/seo.ts` sets title, description, canonical link,
  Open Graph and Twitter card tags per page; the not-found page is
  `noindex`. The home page carries JSON-LD (`SoftwareSourceCode`,
  `Organization`, `Person`) built from `content/seo.content.ts`; the founder's
  `image` appears only once `founder.photo` is a real photo.
- **Origin.** Pages are prerendered with the placeholder
  `https://site-origin.invalid`; the back end replaces it with
  `OBIE_SITE_ORIGIN` when serving. `GET /robots.txt` and `GET /sitemap.xml`
  are generated from the same origin and the prerendered pages.
- **Share image.** `frontend/public/social/obie-share.png`, 1200 × 630.
  After a design change, regenerate it with `npm run share-image` in
  `frontend/` (headless Chrome; `CHROME=/path/to/chrome` picks another one)
  and commit it.
- **Build checks and delivery.** `npm run build` ends with
  `scripts/postbuild.mjs`: it fails when the initial JavaScript exceeds
  150 KB gzip or an image lacks `width`/`height` or is not SVG, WebP or AVIF,
  preloads the Inter weights listed in `PRELOADED_FONTS`, and writes `.br`
  and `.gz` variants of the text assets. The back end serves those variants,
  gzips pages and API responses, sends hashed files (`*-<hash>.js|css`,
  `media/`) with a one-year `immutable` cache and everything else with
  `no-cache`. The legal pages are loaded on demand, the inquiry form's and
  the three-node demo's code right after the first paint, when the browser
  is idle.
- **Accessibility checks.** `src/app/a11y.spec.ts` runs axe-core on every
  route in both themes (WCAG 2.1 A/AA and best practices); a new route must
  be added to its `ROUTES`. Colour contrast is checked on the tokens
  (`styles.spec.ts`) and by Lighthouse, because jsdom has no layout.
- **Lighthouse.** `make -C website lighthouse` needs Docker and Google Chrome
  (or `CHROME_PATH`); `LIGHTHOUSE_PORT` (default `8089`) sets the jar's port.
  Reports land in `website/.lighthouseci/reports/`. Mail and GitHub are
  pointed at a closed local port, so the run depends on nothing outside the
  machine. Over `http://localhost` Chrome accepts gzip but not Brotli, so the
  local scores are slightly pessimistic. In CI, the `website lighthouse` job
  runs it after the `website` job and does not block a merge.

## How it fits together

- **Front end.** Angular builds in `outputMode: "static"`: every route listed
  in `src/app/app.routes.server.ts` with `RenderMode.Prerender` is rendered to
  `<route>/index.html` at build time and hydrated in the browser. There is no
  Node server in production. Styling uses a small design system of CSS custom
  properties in `src/styles.scss` (colour tokens for a light and a dark theme,
  a 4-pt spacing scale) and no component library. Inter and Source Code Pro
  are self-hosted from `@fontsource` packages, so the browser makes no
  third-party requests ([ADR 0012](../documentation/adr/0012-landing-page-content-and-design-system.md)).
- **Inquiry form.** `sections/contact.ts` is the form (reactive forms,
  in-page section `#contact`); `core/inquiry-api.ts` talks to the API. It
  fetches the form token only in the browser, never while prerendering, and
  waits until the token is older than the minimum fill time before it
  sends, so a quick retry is never discarded as a bot. The validators in
  `core/inquiry-validators.ts` and the messages in the content file mirror
  `InquiryRequest.java`; change both together
  ([ADR 0013](../documentation/adr/0013-website-inquiry-form.md)).
- **GitHub links and stats.** "View on GitHub" sits in the header, the hero
  and the footer. `sections/github-strip.ts` (in "Get started") shows the
  contributor links and, once `core/project-api.ts` has loaded them after
  the first render in the browser, stars, the latest release and the last
  commit. Every link that can leave the site has `rel="noopener"`; the
  front-end and smoke tests fail otherwise.
- **Landing page copy.** Every user-visible string of the landing page lives
  in `src/app/content/landing.content.ts`; templates only bind to it. Edit
  copy there. A German version is a second `LandingContent` object provided
  through the `LANDING_CONTENT` token. Describe only what the code does;
  label everything else "in progress" or "planned".
- **Legal pages.** `pages/legal/legal-page.ts` renders `/impressum` and
  `/privacy` from `src/app/content/legal.content.ts`; the route's
  `data.legalPage` picks the page (see
  [Legal pages](#legal-pages-what-the-operator-fills-in-and-reviews)).
- **Back end.** Maven copies `frontend/dist/frontend/browser` into the jar as
  `classpath:/static/`. `StaticSiteConfig` serves files and prerendered
  routes; `NotFoundPageResolver` renders the 404 page. API endpoints live
  under `/api/**`; Actuator's health endpoint is the only one exposed
  (`/api/health`). The inquiry feature (`org.obie.website.inquiry`) is
  described in [ADR 0012](../documentation/adr/0012-website-inquiry-backend.md),
  the GitHub stats (`org.obie.website.project`) in
  [ADR 0014](../documentation/adr/0014-website-github-project-stats.md).
- **Security headers.** `SecurityHeadersFilter` sets CSP, HSTS,
  `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy` and
  `frame-ancestors 'none'` on every response. Inline scripts are allowed only
  by hash: the filter hashes the inline scripts of the packaged pages at
  startup, so the front end needs no inline event handlers and no
  `'unsafe-inline'`.
- **Database changes.** Add a Flyway migration
  `backend/src/main/resources/db/migration/V<n>__<what>.sql`; never edit one
  that has been released.
- **Adding a page.** Add the route to `src/app/app.routes.ts` and a
  `RenderMode.Prerender` entry to `src/app/app.routes.server.ts`, and call
  `SeoService.apply` in the page with a unique title and description. The
  back end serves it and lists it in `sitemap.xml` without changes.
- **Dependency overrides.** `backend/pom.xml` overrides a few versions
  managed by Spring Boot to pick up security fixes; remove them when Spring
  Boot catches up.
