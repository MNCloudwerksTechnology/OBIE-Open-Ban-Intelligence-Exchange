# ADR 0013: Inquiry form of the website

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1675](https://openproject.niew.dev/work_packages/1675)
- **Amends:** [ADR 0010](0010-website-stack-and-build.md) (front-end
  dependencies, checks)

## Context

Visitors book the founder for talks, workshops and interviews, or send
another inquiry, through a form that uses the inquiry API of
[ADR 0012](0012-website-inquiry-backend.md). The site is prerendered to
static HTML and hydrated in the browser. The back end silently discards
inquiries sent less than 3 s after their form token was issued, so a
front end that sends too early loses a person's inquiry without anyone
noticing.

## Decision

- **Angular reactive forms** (`@angular/forms`, part of Angular) and
  `HttpClient` with the fetch backend. No form or UI library.
- **In-page section, not a dialog.** The form lives in the `#contact`
  section, so `/#contact` deep-links to it, it works before the JavaScript
  has loaded, and no focus trapping is needed.
- **Token lifecycle in one service.** `InquiryApi` fetches the form token
  after the first render in the browser (never while prerendering), and
  before sending waits until the token has been held for longer than the
  minimum fill time. When the back end reports an expired token, the
  service fetches a new one and the form asks the visitor to send again;
  entries are never lost.
- **Validation mirrors the back end.** Client validators apply the same
  rules as `InquiryRequest.java` and show the same messages (kept in the
  content file); server field errors are shown next to the same fields.
- **Browser end-to-end test in `./mvnw verify`.** A Testcontainers Selenium
  container (`selenium/standalone-chromium`, pinned to the Selenium version
  Spring Boot manages) drives the packaged site against the running
  application, PostgreSQL and GreenMail. It needs only Docker, which the
  back-end tests already require; no Node-based browser runner is added.

## Consequences

- A change to a validation rule must be made in `InquiryRequest.java`,
  `core/inquiry-validators.ts` and the messages in the content file.
- The browser test proves hydration under the production Content Security
  Policy and the minimum-fill-time wait; it downloads the Chromium image
  once (about 1.5 GB) and adds roughly 15 s to `./mvnw verify`.
- The main bundle grows by the Angular forms and HTTP packages.
