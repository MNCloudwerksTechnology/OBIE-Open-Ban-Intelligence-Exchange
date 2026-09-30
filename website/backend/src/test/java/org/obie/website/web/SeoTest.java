package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;

/** What search engines and share previews see: absolute URLs, robots.txt, sitemap.xml. */
class SeoTest extends IntegrationTest {

  /** {@code obie.web.site-origin} in application-test.properties. */
  private static final String SITE = "https://obie.example";

  @Autowired private TestRestTemplate http;

  @ParameterizedTest
  @ValueSource(strings = {"/", "/impressum", "/privacy", "/de", "/de/impressum", "/de/datenschutz"})
  void pagesLinkTheirCanonicalUrlOnTheConfiguredOrigin(String path) {
    String html = getHtml(path).getBody();

    assertThat(html)
        .doesNotContain(SiteOrigin.PLACEHOLDER)
        .contains("<link rel=\"canonical\" href=\"" + SITE + path + "\">")
        .contains("<meta property=\"og:url\" content=\"" + SITE + path + "\">")
        .contains("<meta property=\"og:image\" content=\"" + SITE + "/social/obie-share.png\">")
        .contains("<meta name=\"twitter:card\" content=\"summary_large_image\">")
        .doesNotContain("name=\"robots\"");
  }

  @Test
  void pagesLinkTheirCounterpartInTheOtherLanguageOnTheConfiguredOrigin() {
    assertThat(getHtml("/privacy").getBody())
        .contains("<html lang=\"en\"")
        .contains(
            "<link rel=\"alternate\" hreflang=\"en\" href=\"" + SITE + "/privacy\">",
            "<link rel=\"alternate\" hreflang=\"de\" href=\"" + SITE + "/de/datenschutz\">",
            "<link rel=\"alternate\" hreflang=\"x-default\" href=\"" + SITE + "/privacy\">");
    assertThat(getHtml("/de/datenschutz").getBody())
        .contains("<html lang=\"de\"")
        .contains("<link rel=\"alternate\" hreflang=\"en\" href=\"" + SITE + "/privacy\">");
  }

  @Test
  void homePageCarriesTheStructuredDataOnTheConfiguredOrigin() {
    assertThat(getHtml("/").getBody())
        .contains("<script id=\"structured-data\" type=\"application/ld+json\">")
        .contains("\"@type\":\"SoftwareSourceCode\"", "\"@type\":\"Person\"")
        .contains("\"url\":\"" + SITE + "/\"");
  }

  @Test
  void notFoundPageIsNotIndexedAndHasNoPlaceholder() {
    ResponseEntity<String> response = getHtml("/no/such/page");

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    assertThat(response.getBody())
        .contains("<meta name=\"robots\" content=\"noindex\">")
        .doesNotContain(SiteOrigin.PLACEHOLDER)
        .doesNotContain("rel=\"canonical\"");
  }

  @Test
  void unknownGermanUrlGetsTheGermanNotFoundPage() {
    ResponseEntity<String> response = getHtml("/de/gibt/es/nicht");

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    assertThat(response.getBody())
        .contains("<html lang=\"de\"")
        .contains("Seite nicht gefunden")
        .contains("<meta name=\"robots\" content=\"noindex\">")
        .doesNotContain(SiteOrigin.PLACEHOLDER);
    // Only the first segment can name a language; everything else is English.
    assertThat(getHtml("/deutsch/seite").getBody()).contains("<html lang=\"en\"");
    assertThat(getHtml("/xy/page").getBody()).contains("<html lang=\"en\"");
  }

  @Test
  void robotsTxtPointsToTheSitemap() {
    ResponseEntity<String> response = http.getForEntity("/robots.txt", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getHeaders().getContentType().isCompatibleWith(MediaType.TEXT_PLAIN))
        .isTrue();
    assertThat(response.getBody())
        .contains("User-agent: *", "Disallow: /api/", "Sitemap: " + SITE + "/sitemap.xml");
  }

  @Test
  void sitemapListsThePublicPagesButNotTheNotFoundPage() {
    ResponseEntity<String> response = http.getForEntity("/sitemap.xml", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getHeaders().getContentType().isCompatibleWith(MediaType.APPLICATION_XML))
        .isTrue();
    assertThat(response.getBody())
        .contains("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">")
        .contains(
            "<loc>" + SITE + "/</loc>",
            "<loc>" + SITE + "/impressum</loc>",
            "<loc>" + SITE + "/privacy</loc>",
            "<loc>" + SITE + "/de</loc>",
            "<loc>" + SITE + "/de/impressum</loc>",
            "<loc>" + SITE + "/de/datenschutz</loc>")
        .doesNotContain("404");
  }

  @Test
  void shareImageIsServedAsPng() {
    ResponseEntity<byte[]> response = http.getForEntity("/social/obie-share.png", byte[].class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getHeaders().getContentType()).isEqualTo(MediaType.IMAGE_PNG);
  }

  private ResponseEntity<String> getHtml(String path) {
    HttpHeaders headers = new HttpHeaders();
    headers.setAccept(List.of(MediaType.TEXT_HTML));
    return http.exchange(path, HttpMethod.GET, new HttpEntity<>(headers), String.class);
  }
}
