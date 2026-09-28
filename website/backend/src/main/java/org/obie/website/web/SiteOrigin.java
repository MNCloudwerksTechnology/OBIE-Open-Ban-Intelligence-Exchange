package org.obie.website.web;

import org.springframework.stereotype.Component;

/**
 * The site's configured origin ({@code obie.web.site-origin}) and its substitution into the
 * prerendered pages.
 *
 * <p>The origin is deployment configuration, unknown when the front end is built. The pages are
 * therefore prerendered with {@link #PLACEHOLDER} in their absolute URLs (canonical link, Open
 * Graph tags, JSON-LD), and every page is served with the placeholder replaced (ADR 0015).
 */
@Component
public class SiteOrigin {

  /** Written by the front end while prerendering; keep in sync with {@code core/seo.ts}. */
  static final String PLACEHOLDER = "https://site-origin.invalid";

  private final String origin;

  public SiteOrigin(WebProperties properties) {
    this.origin = properties.siteOrigin();
  }

  /** The origin, e.g. {@code https://obie.example}. */
  public String origin() {
    return origin;
  }

  /** {@code path} (starting with a slash) as an absolute URL on the site. */
  public String url(String path) {
    return origin + path;
  }

  /** The page with the configured origin in place of the placeholder. */
  public String applyTo(String html) {
    return html.replace(PLACEHOLDER, origin);
  }
}
