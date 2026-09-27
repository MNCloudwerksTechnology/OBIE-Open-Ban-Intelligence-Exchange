# ADR 0010: Landing page content file and design system

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1673](https://openproject.niew.dev/work_packages/1673)

## Context

The landing page (epic #1671) explains OBIE to non-specialists. It must be
English now and translatable to German later without touching templates,
follow the OBIE brand (Cloudwerks visual system: Inter and Source Code Pro,
primary orange `#e59631`), offer light and dark themes, meet WCAG 2.1 AA and
make no request to any third-party origin. [ADR 0009](0009-website-stack-and-build.md)
chose "system fonts only"; the brand now asks for two named typefaces.

## Decision

- **Content file.** All landing page copy, link targets and labels live in
  one typed TypeScript module, `website/frontend/src/app/content/landing.content.ts`,
  exported as a `LandingContent` object. Templates only bind to it and hold
  no user-visible text. German is added later as a second object of the same
  type and chosen per locale; the type makes a missing string a compile
  error. We use a plain module instead of Angular i18n (`$localize`, XLIFF)
  because the page is one route, the copy is long structured data (lists of
  steps, cards, questions) rather than template strings, and Angular i18n
  builds one bundle per locale, which is a decision for the multi-language
  work package, not this one.
- **Fonts.** Inter (400/600/700) and Source Code Pro (400/700) are
  self-hosted from the `@fontsource/*` npm packages (SIL Open Font License).
  Only the Latin subset is included; the files are bundled by the Angular
  build and served by the jar from the site's own origin. This supersedes the
  "system fonts only" clause of ADR 0009; the no-third-party-request rule is
  unchanged.
- **Design tokens.** Colours, type sizes, spacing (a 4-pt base scale:
  4, 8, 12, 16, 24, 32, 48, 64, 96 px), radii and motion are CSS custom
  properties in `src/styles.scss`. Components use tokens only.
- **Themes.** Light is the default, dark applies under
  `prefers-color-scheme: dark`. A header toggle may override the system
  choice for the current visit by setting `data-theme` on `<html>`; the
  choice is not stored (no cookie, no local storage), so the site needs no
  consent banner. Without JavaScript the page still follows the system
  theme.
- **Brand assets.** The OBIE logos are committed under
  `website/frontend/public/brand/` and the mark is inlined in the header so it
  can follow the theme.

## Alternatives considered

- **Angular i18n (`@angular/localize`)** — the standard mechanism, but it
  splits the build per locale and extracts text from templates, which suits
  UI strings better than long structured copy. Can still be adopted later;
  the content object would then become its source.
- **JSON content file** — no type checking of structure or completeness
  between languages; rejected in favour of a typed module.
- **Fonts from Google Fonts or a CDN** — third-party requests; rejected.
- **Persisting the theme choice in local storage** — would need a consent
  assessment under German law (TTDSG §25) for a cosmetic preference; not
  worth it for a single page.

## Consequences

- Copy changes are made in one file; reviewers can check tone and claims
  without reading templates.
- The page transfers the five Latin-subset font files (~100 KB in total) on
  first visit; `font-display: swap` keeps text visible while they load.
- The multi-language work package decides how a locale is selected and
  served; it adds a second `LandingContent` object.
