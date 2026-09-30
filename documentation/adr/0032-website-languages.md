# ADR 0032: English and German website with Transloco

- **Status:** Accepted
- **Date:** 2026-09-30
- **Amends:** [ADR 0012](0012-landing-page-content-and-design-system.md) (content file),
  [ADR 0015](0015-website-seo-and-delivery.md) (SEO and delivery)

## Context

The website is English, with the German legal terms shown alongside the
legal pages. The operator wants a German version of every page and a switch
between the two. ADR 0012 already keeps every user-visible string in a
typed content object, so that a language is "a second object of the same
type". The pages are prerendered to static HTML and hydrated; they must
keep working without JavaScript, stay within the performance budget
(150 KB gzip initial JavaScript) and make no third-party request. The
operator asked for Transloco with the translations compiled into the
application.

## Decision

- **One URL per page and language.** English stays at the root (`/`,
  `/impressum`, `/privacy`), German lives under `/de` (`/de`,
  `/de/impressum`, `/de/datenschutz`). `PAGE_PATHS` in
  `i18n/languages.ts` is the single list; routes and prerendered routes
  follow from it. Each page is prerendered per language with
  `<html lang>`, its own canonical URL, `hreflang` links to the other
  language and `x-default` to English. There is no redirect by
  `Accept-Language`.
- **The language comes from the URL and never changes while the
  application runs.** An app initializer reads it from the path, makes it
  Transloco's active language, loads its translation and sets
  `<html lang>` before the first component is created, both while
  prerendering and in the browser, so hydration always meets the same
  language. The header's switch is a plain link to the same page in the
  other language; the browser loads that prerendered page. Nothing about
  the language is stored.
- **Transloco holds typed content objects.** Each language's translation is
  `{ landing, seo, consent }`, the typed objects of ADR 0012 (the German
  ones in `*.de.ts`); the legal pages' copy is the scope `legal`, loaded by
  a route resolver on those pages only. Components read their object with
  `translateObject` through the existing injection tokens
  (`LANDING_CONTENT`, `SEO_CONTENT`, `LEGAL_CONTENT`, `CONSENT_CONTENT`);
  templates did not change. A missing or misspelt German field is a
  compile error, and tests compare the German objects with the English
  ones (shape, links, placeholders, caption lengths, voice rules).
- **Compiled in, split by language.** A loader maps each translation to a
  module: English is part of the main bundle and loads synchronously, so
  English pages start as before; German and each language's legal copy are
  chunks loaded only where needed. No translation is fetched as JSON.
- **Numbers and dates** follow the content's locale (`de-DE`), as the
  existing `Intl` formatting already did.
- **Unknown URLs under `/de/`** get the prerendered German not-found page
  (`/de/404`) from the back end; the sitemap lists every page of every
  language and no not-found page.

## Alternatives considered

- **Key-per-string Transloco (`'hero.heading' | transloco`)** — the usual
  Transloco style, but it would replace every typed binding with an
  unchecked string key, rewrite every template and lose the content tests
  of ADR 0012; rejected in favour of typed objects in Transloco.
- **Angular's built-in i18n (`$localize`)** — one build per language and
  extraction tooling, a poor fit for copy that is structured data (lists,
  demo steps, legal blocks); rejected.
- **Switching the language in place, remembered in the browser** — the
  prerendered HTML would be English for everyone, German visitors would see
  it flip after hydration, search engines would see no German page, and the
  choice would need storage; rejected.

## Consequences

- Every copy change is made twice, in English and in German; the parity
  tests fail until both agree in shape.
- The back end still answers in English: its validation messages (shown
  only when a request bypasses the form's own checks) and the confirmation
  e-mail to the visitor. Translating them needs the inquiry's language in
  the request and the database.
- Tests load Transloco, a partially compiled library, through the JIT
  compiler (`polyfills` of the `test` build configuration); the application
  build links it ahead of time as usual.
- The initial JavaScript grew by about 12 KB gzip, to 125.5 KB of the
  150 KB budget: Transloco, the loader and the language switch, together
  with the consent dialog and statistics code of ADR 0033.
