# ADR 0015: Website SEO, delivery performance and quality gates

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1678](https://openproject.niew.dev/work_packages/1678)
- **Amends:** [ADR 0010](0010-website-stack-and-build.md) (build and serving),
  [ADR 0012](0012-landing-page-content-and-design-system.md) (fonts)

## Context

The landing page has to be found for searches such as "open source threat
intelligence sharing", preview well when shared, and score at least 95 in
every Lighthouse category on a phone. Search engines and share previews need
absolute URLs (canonical link, `og:image`, JSON-LD, sitemap), but the pages
are prerendered at build time while the site's origin is deployment
configuration (`OBIE_SITE_ORIGIN`). The page must keep making no third-party
requests, and the Content Security Policy allows inline scripts only by hash.

## Decision

- **Head tags from the content files.** One front-end service (`core/seo.ts`)
  writes title, description, canonical link, `robots`, Open Graph and Twitter
  card tags per route, and the home page's JSON-LD (`SoftwareSourceCode`,
  `Organization`, `Person`). The facts come from `content/seo.content.ts`
  (operator-supplied); the founder's `image` is left out while the photo is
  the placeholder. The not-found page is `noindex` and has no canonical link.
- **Origin placeholder, filled in by the back end.** Pages are prerendered
  with `https://site-origin.invalid`; the back end replaces it with the
  configured origin when it serves a page (a resource transformer, cached by
  the resource chain, and the 404 resolver). `OBIE_SITE_ORIGIN` is therefore
  restricted to scheme, host and port. One jar serves any domain.
- **`robots.txt` and `sitemap.xml` from the back end**, generated at startup
  from the origin and the prerendered `<route>/index.html` files on the
  classpath (the not-found page excluded), so a new page needs no back-end
  change. JSON-LD is a data block, not a script, and needs no CSP hash.
- **Share image as a static asset.** `public/social/obie-share.png`
  (1200 × 630) is generated from the brand mark and Inter with headless
  Chrome (`npm run share-image`) and committed.
- **Post-build step instead of new build tooling.** `scripts/postbuild.mjs`
  (Node only, run by `npm run build`) fails the build when initial JavaScript
  exceeds 150 KB gzip or an `<img>` lacks explicit dimensions or a modern
  format, preloads the Inter weights of the first screen, inlines the
  `@font-face` rules (Angular loads the global stylesheet only after the first
  paint, which otherwise paints the text twice), and writes Brotli and gzip
  variants of the text assets. Pages are not precompressed because the back
  end rewrites them.
- **Serving.** Spring serves the precompressed variants
  (`EncodedResourceResolver`) and gzips dynamic responses
  (`server.compression`); Tomcat's built-in compression has no Brotli.
  Files with the build hash in their name (bundles, `media/`) are sent with
  `Cache-Control: public, max-age=31536000, immutable`, everything else with
  `no-cache` (revalidated via `Last-Modified`).
- **Smaller first load.** The legal pages are lazy routes, and the inquiry
  form is an incremental-hydration `@defer (hydrate on idle)` block: it is
  still prerendered in full, but its code (reactive forms) loads once the
  browser is idle after the first paint. Not `hydrate on viewport`: a form
  submitted before hydration would reload the page and lose what was typed,
  so the form must be ready before the visitor reaches it.
- **Quality gates.** axe-core runs in the front-end tests on every route in
  both themes (jsdom cannot measure contrast; the colour tokens are checked
  separately and by Lighthouse). `make -C website lighthouse` runs Lighthouse
  CI, pinned and run with `npx` rather than added to the front end's
  dependencies, against the jar started with a throwaway PostgreSQL, and
  fails below 0.95 (median of three runs) in any category. In CI it is a
  separate, non-blocking job because scores depend on the runner's load.

## Consequences

- The jar stays environment-independent; a wrong `OBIE_SITE_ORIGIN` shows
  up in canonical links and the sitemap, so it must be the public origin.
- Lighthouse on `http://localhost` receives gzip, not Brotli (browsers
  advertise Brotli only over HTTPS), so the local measurement is slightly
  pessimistic compared with production.
- Preloading fonts trades a little first-paint time for no layout shift; the
  preloaded weights are listed in the post-build script.
- Adding a heavy dependency to the landing page will fail the build on the
  JavaScript budget before it can lower the Lighthouse score.
