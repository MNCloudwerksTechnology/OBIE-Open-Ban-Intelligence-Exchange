package org.obie.website.web;

import java.util.List;
import java.util.concurrent.TimeUnit;
import org.springframework.http.CacheControl;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * {@code /robots.txt} and {@code /sitemap.xml} for search engines, built from the configured origin
 * ({@link SiteOrigin}) and the prerendered pages ({@link PrerenderedPages}). Both are generated
 * once at startup.
 */
@RestController
public class CrawlerController {

  private static final CacheControl ONE_HOUR = CacheControl.maxAge(1, TimeUnit.HOURS).cachePublic();

  private final String robots;
  private final String sitemap;

  public CrawlerController(SiteOrigin siteOrigin) {
    this.robots = robots(siteOrigin);
    this.sitemap =
        sitemap(siteOrigin, PrerenderedPages.publicPaths(StaticSiteConfig.STATIC_LOCATION));
  }

  @GetMapping(value = "/robots.txt", produces = MediaType.TEXT_PLAIN_VALUE)
  public ResponseEntity<String> robots() {
    return ResponseEntity.ok().cacheControl(ONE_HOUR).body(robots);
  }

  @GetMapping(value = "/sitemap.xml", produces = MediaType.APPLICATION_XML_VALUE)
  public ResponseEntity<String> sitemap() {
    return ResponseEntity.ok().cacheControl(ONE_HOUR).body(sitemap);
  }

  /** Everything may be crawled except the API. */
  static String robots(SiteOrigin siteOrigin) {
    return String.join(
        "\n",
        "User-agent: *",
        "Allow: /",
        "Disallow: /api/",
        "",
        "Sitemap: " + siteOrigin.url("/sitemap.xml"),
        "");
  }

  /** The sitemap protocol's {@code urlset}; the origin needs no escaping (see WebProperties). */
  static String sitemap(SiteOrigin siteOrigin, List<String> pages) {
    StringBuilder xml =
        new StringBuilder("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
            .append("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n");
    for (String page : pages) {
      xml.append("  <url><loc>").append(siteOrigin.url(page)).append("</loc></url>\n");
    }
    return xml.append("</urlset>\n").toString();
  }
}
