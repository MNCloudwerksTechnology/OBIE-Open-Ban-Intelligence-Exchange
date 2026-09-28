# ADR 0014: Live GitHub stats on the website, fetched server-side

- **Status:** Accepted
- **Date:** 2026-09-28
- **Work package:** [#1676](https://openproject.niew.dev/work_packages/1676)
- **Amends:** [ADR 0010](0010-website-stack-and-build.md) (API surface)

## Context

The landing page sends engineers to the repository and should show at a
glance that the project is alive: stars, the latest release, the last
commit. The site promises that the visitor's browser makes no third-party
requests (no cookie banner, CSP `connect-src 'self'`), so neither the GitHub
API nor GitHub badges or images may be loaded from the page. Unauthenticated
GitHub API calls are limited to 60 per hour and IP.

## Decision

- **Back-end proxy `GET /api/project`.** The Spring Boot back end reads the
  repository, its latest release and the newest commit on the default
  branch from the GitHub REST API and returns one small JSON object. The
  repository (`OBIE_GITHUB_REPOSITORY`), the API URL and an optional token
  (`OBIE_GITHUB_TOKEN`) are configurable. It uses Spring's `RestClient` on
  the JDK HTTP client with timeouts and without following redirects, so a
  token is never sent to another host. No new dependency.
- **Server-side cache.** Stats are kept in memory for `OBIE_GITHUB_CACHE_TTL`
  (default 15 minutes), so visitors cause at most four GitHub fetches per
  hour. After a failure (error, timeout, rate limit, unexpected shape) the
  last good stats are served and GitHub is not asked again before
  `OBIE_GITHUB_RETRY_DELAY` (default 1 minute). Without any good stats the
  answer is `{"available": false, …}`. The endpoint always answers 200.
- **Front end fetches only after rendering in the browser**, never while
  prerendering, and leaves the stats strip out unless `available` is true.
  The static contributor links (repository, quick start, spec, good first
  issues) and the "View on GitHub" buttons do not depend on the stats.
- **Every link that can leave the site carries `rel="noopener"`**, set in the
  templates; tests check the rendered and the prerendered page.

## Consequences

- The page stays free of third-party requests; the CSP needs no change.
- Stats can be up to one cache period old; after a restart they are missing
  until the first successful fetch.
- The cache is per instance; several instances fetch independently, which is
  well within GitHub's limits, especially with a token.
- GitHub's `open_issues_count` includes open pull requests; the page shows
  only stars, release and last commit.
