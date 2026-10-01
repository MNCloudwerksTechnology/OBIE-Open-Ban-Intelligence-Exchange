# ADR 0036: One URL per page, lenient crawler files and site structured data

- **Status:** Accepted
- **Date:** 2026-10-01
- **Amends:** [ADR 0015](0015-website-seo-and-delivery.md) (SEO and delivery),
  [ADR 0033](0033-website-languages.md) (languages)

## Context

An audit of what crawlers receive from the running jar found that the
prerendered pages and their head tags were sound, but:

- every page also answered 200 under other URLs: with a trailing slash
  (`/de/`, `/impressum/`), by its file (`/index.html`,
  `/de/datenschutz/index.html`), and the not-found pages by their own path
  (`/404`, `/de/404`). Only the canonical link tied the duplicates together;
- `/robots.txt` and `/sitemap.xml` answered 406 to clients whose `Accept`
  header did not name `text/plain` or `application/xml` exactly, such as
  `text/xml` or `text/html`;
- the JSON-LD had no `WebSite`, from which Google takes the site name it
  shows in results, and the FAQ section was not marked up;
- the German home page's title (66 characters) and description (175) were
  longer than the limits the English pages are tested against (60 and 160).

## Decision

- **Redirect to the canonical path.** A `GET` or `HEAD` outside `/api/` for
  a path that ends in `/` or `/index.html` is answered with 301 and a
  relative `Location` naming the path without it, query string kept
  (`CanonicalPathFilter`). Relative, so the redirect stays on the host the
  visitor used: the configured origin is already in the canonical link, and
  host canonicalisation (HTTP to HTTPS, `www`) is the reverse proxy's job.
  Paths with an empty segment or a backslash are never redirected, so the
  target can never name another host.
- **Not-found pages are not pages.** `/404` and `/<lang>/404` answer 404
  like any unknown URL; the back end still reads their files to render
  unknown URLs.
- **Crawler files ignore `Accept`.** `robots.txt` and `sitemap.xml` are sent
  with their own content type whatever the client asks for.
- **`WebSite` and `FAQPage` in the home page's JSON-LD.** `WebSite` names the
  site (`OBIE`, alternate name `Open Ban Intelligence Exchange`) and is the
  same node in both languages. `FAQPage` holds exactly the questions and
  answers the page shows, in the page's language (`inLanguage`), so it
  cannot drift from the visible copy.
- **Search-result lengths for every language.** The German pages are tested
  against the same limits as the English ones; the German home page's title
  and description were shortened to fit.

## Consequences

- Google shows FAQ rich results only for a few authoritative government and
  health sites; the `FAQPage` markup is for other search and answer engines
  and costs nothing to keep correct.
- `/` is still served through Spring Boot's welcome-page forward, which sets
  `Content-Language` from the request's `Accept-Language`. Spring's resource
  handler does not serve an empty path, so taking `/` away from the welcome
  page would need a handler of its own; crawlers send no or an English
  `Accept-Language`, so the header is left as it is.
- Links inside the site must keep naming canonical paths (no trailing slash),
  or every click costs a redirect.
