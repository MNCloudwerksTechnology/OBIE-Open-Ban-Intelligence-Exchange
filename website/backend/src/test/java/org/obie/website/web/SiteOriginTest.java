package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import org.junit.jupiter.api.Test;
import org.springframework.util.unit.DataSize;

class SiteOriginTest {

  private final SiteOrigin siteOrigin =
      new SiteOrigin(new WebProperties("https://obie.example:8443", DataSize.ofKilobytes(16)));

  @Test
  void replacesEveryPlaceholderInAPage() {
    String html =
        "<link rel=\"canonical\" href=\"https://site-origin.invalid/privacy\">"
            + "<script type=\"application/ld+json\">{\"url\":\"https://site-origin.invalid/\"}</script>";

    assertThat(siteOrigin.applyTo(html))
        .isEqualTo(
            "<link rel=\"canonical\" href=\"https://obie.example:8443/privacy\">"
                + "<script type=\"application/ld+json\">{\"url\":\"https://obie.example:8443/\"}</script>");
  }

  @Test
  void robotsKeepsCrawlersOutOfTheApiAndPointsToTheSitemap() {
    assertThat(CrawlerController.robots(siteOrigin))
        .isEqualTo(
            "User-agent: *\nAllow: /\nDisallow: /api/\n\n"
                + "Sitemap: https://obie.example:8443/sitemap.xml\n");
  }

  @Test
  void sitemapListsTheGivenPagesAsAbsoluteUrls() {
    assertThat(CrawlerController.sitemap(siteOrigin, List.of("/", "/impressum")))
        .isEqualTo(
            "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"
                + "<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n"
                + "  <url><loc>https://obie.example:8443/</loc></url>\n"
                + "  <url><loc>https://obie.example:8443/impressum</loc></url>\n"
                + "</urlset>\n");
  }
}
