# ADR 0034: Visitor statistics with self-hosted Matomo, after consent

- **Status:** Accepted
- **Date:** 2026-09-30
- **Amends:** [ADR 0012](0012-landing-page-content-and-design-system.md) (no storage),
  [ADR 0015](0015-website-seo-and-delivery.md) (no third-party requests)

## Context

The operator wants to know how the website is used: which pages and
sections are read, where visitors come from, which links they follow and
whether they send inquiries. The site so far set no cookies, stored
nothing in the browser and contacted no other server, which is why it had
no consent banner. The operator runs Matomo at `metrics.cloudwerks.de`
(site id 5). German law requires consent before information on the
visitor's device is read or stored for this purpose (§ 25(1) TDDDG), and
the consent must be informed, voluntary and as easy to withdraw as to give
(Art. 7 GDPR).

## Decision

- **Nothing before consent.** Until the visitor accepts, the browser never
  contacts the statistics server: the Matomo script is not in the pages and
  no request is queued. Without JavaScript nothing is measured at all (no
  `<noscript>` pixel).
- **A non-modal dialog, asked once.** On the first visit a dialog at the
  bottom of the window asks in the page's language, with a link to the
  other language, what is measured and by whom, and links to the privacy
  policy. "Decline" and "Accept" look the same. It does not block the
  page, and it takes focus only when the visitor opens it from "Privacy
  settings" in the footer, where the answer can be changed at any time. The
  dialog and the footer button appear only after hydration, never in the
  prerendered HTML. The answer is kept in local storage
  (`obie-analytics-consent-v1`), strictly necessary to respect it
  (§ 25(2) no. 2 TDDDG); a new purpose raises the version and asks again.
- **Cookieless Matomo, consent mode.** After consent the front end queues
  `requireConsent`, `setConsentGiven` and `disableCookies`, then loads
  `matomo.js` from `https://metrics.cloudwerks.de`. Withdrawing consent
  pushes `forgetConsentGiven`, so Matomo sends nothing more from that page
  on.
- **What is measured.** A page view per route (the application is
  prerendered but navigates client-side, so each navigation sets the URL,
  title and referrer), the not-found page under Matomo's `404/URL = …`
  title, links to other sites (link tracking), time on page (heartbeat
  timer), and three events with fixed English names: `Inquiry / sent /
  <type>` (never the content), `Demo / started`, `Demo / completed`.
- **One more origin in the Content Security Policy.** `script-src`,
  `connect-src` and `img-src` allow `https://metrics.cloudwerks.de`, and
  nothing else changes. The origin is a constant in the front end and the
  back end; a test compares the two.
- **Server-side settings are the operator's.** Raw data retention and
  accepting only the site's own URLs are Matomo settings. The privacy policy
  follows [cloudwerks.de/datenschutz](https://cloudwerks.de/datenschutz),
  which the operator declared applicable: Matomo on Cloudwerks' own server,
  nothing passed to third parties; raw visit data are deleted after 14
  months (operator's setting). It does not promise that IP addresses are
  shortened, since neither the operator nor that page confirms it.

## Alternatives considered

- **Cookieless tracking without consent** — Matomo can run without cookies,
  but it still reads device information in the browser, which German
  supervisory authorities treat as needing consent; rejected.
- **Server-side log analysis** — no consent needed, but no link or event
  data and no way to tell pages of the single-page site apart after the
  first load; rejected for now.
- **A modal consent wall** — forces an answer before reading and nudges
  towards accepting; rejected.
- **Matomo cookies after consent** (`_pk_id`, `_pk_ses`) — would recognise
  returning visitors, but adds cookies to explain and delete on withdrawal;
  rejected, the landing page does not need it.

## Consequences

- The site now has a consent dialog and one stored value; the privacy
  policy (both languages) has a section on the statistics, and the README
  documents what the operator configures in Matomo.
- Visitors who decline, or never answer, are not counted; the numbers
  undercount by design.
- Returning visitors are not recognised across days (no cookies).
- A local or staging run whose visitor clicks "Accept" would report to the
  production Matomo unless Matomo accepts only the production URL.
