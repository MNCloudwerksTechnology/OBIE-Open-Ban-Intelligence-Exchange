package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.util.List;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import org.junit.jupiter.params.provider.ValueSource;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;

/**
 * What search engines and share previews see: absolute URLs, one URL per page, robots.txt,
 * sitemap.xml.
 */
class SeoTest extends IntegrationTest {

  /** {@code obie.web.site-origin} in application-test.properties. */
  private static final String SITE = "https://obie.example";

  @Autowired private TestRestTemplate http;

  /** Does not follow redirects, so the tests see them. */
  private final HttpClient client = HttpClient.newHttpClient();

  @LocalServerPort private int port;

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

  @ParameterizedTest
  @CsvSource({
    "/de/, /de",
    "/impressum/, /impressum",
    "/de/datenschutz/index.html, /de/datenschutz",
    "/index.html, /",
    "/privacy/?utm_source=feed, /privacy?utm_source=feed"
  })
  void otherUrlsOfAPageRedirectPermanentlyToItsCanonicalUrl(String path, String canonical) {
    HttpResponse<Void> response = send(path);

    assertThat(response.statusCode()).isEqualTo(HttpStatus.MOVED_PERMANENTLY.value());
    assertThat(response.headers().firstValue(HttpHeaders.LOCATION)).hasValue(canonical);
  }

  @ParameterizedTest
  @ValueSource(strings = {"//obie.invalid/", "/%5Cobie.invalid/"})
  void neverRedirectsToAnotherHost(String path) {
    assertThat(send(path).headers().firstValue(HttpHeaders.LOCATION)).isEmpty();
  }

  @ParameterizedTest
  @ValueSource(strings = {"/404", "/de/404"})
  void notFoundPagesAnswer404ByTheirOwnPathToo(String path) {
    ResponseEntity<String> response = getHtml(path);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    assertThat(response.getBody()).contains("<meta name=\"robots\" content=\"noindex\">");
  }

  @ParameterizedTest
  @ValueSource(strings = {"text/html", "text/xml", "application/xml", "text/plain", "*/*"})
  void crawlerFilesAreServedWhateverTheClientAccepts(String accept) {
    HttpResponse<Void> robots = send("/robots.txt", "Accept", accept);
    HttpResponse<Void> sitemap = send("/sitemap.xml", "Accept", accept);

    assertThat(robots.statusCode()).isEqualTo(HttpStatus.OK.value());
    assertThat(robots.headers().firstValue(HttpHeaders.CONTENT_TYPE).orElseThrow())
        .startsWith(MediaType.TEXT_PLAIN_VALUE);
    assertThat(sitemap.statusCode()).isEqualTo(HttpStatus.OK.value());
    assertThat(sitemap.headers().firstValue(HttpHeaders.CONTENT_TYPE).orElseThrow())
        .startsWith(MediaType.APPLICATION_XML_VALUE);
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

  /**
   * A GET as a browser sends it, with extra headers as name-value pairs; never follows redirects.
   */
  private HttpResponse<Void> send(String path, String... headers) {
    HttpRequest.Builder request =
        HttpRequest.newBuilder(URI.create("http://localhost:" + port + path))
            .header("Accept", "text/html,*/*");
    for (int i = 0; i < headers.length; i += 2) {
      request.setHeader(headers[i], headers[i + 1]);
    }
    try {
      return client.send(request.build(), HttpResponse.BodyHandlers.discarding());
    } catch (IOException e) {
      throw new UncheckedIOException(e);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new IllegalStateException(e);
    }
  }
}
