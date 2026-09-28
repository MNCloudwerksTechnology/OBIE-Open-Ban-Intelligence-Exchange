package org.obie.website;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;

/** Boots the whole application and checks what a visitor and a monitor see. */
class WebsiteSmokeTest extends IntegrationTest {

  /** The landing page's h1, from the front end's content file. */
  private static final String HOME_HEADING = "A neighbourhood watch for servers.";

  /** Marker Angular writes into pages rendered at build time (static site generation). */
  private static final String PRERENDERED = "ng-server-context=\"ssg\"";

  @Autowired private TestRestTemplate http;

  @Test
  void homePageIsThePrerenderedHtml() {
    ResponseEntity<String> response = getHtml("/");

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getHeaders().getContentType()).isNotNull();
    assertThat(response.getHeaders().getContentType().isCompatibleWith(MediaType.TEXT_HTML))
        .isTrue();
    // Only prerendered output contains the rendered heading; the client-side
    // shell would contain just an empty <app-root>.
    assertThat(response.getBody()).containsPattern(heading(HOME_HEADING)).contains(PRERENDERED);
  }

  @Test
  void homePageLoadsNothingFromThirdPartyOriginsAndOpensExternalLinksSafely() {
    String html = getHtml("/").getBody();

    assertThat(html)
        .doesNotContainPattern("\\ssrcset=\"[^\"]*(https?:)?//")
        .doesNotContainPattern("\\ssrc=\"(https?:)?//")
        // The canonical link points to the site itself (obie.web.site-origin) and loads nothing.
        .doesNotContainPattern("<link\\s[^>]*href=\"(https?:)?//(?!obie\\.example/)")
        .doesNotContainPattern("url\\((['\"])?(https?:)?//");
    Matcher anchors = Pattern.compile("<a\\s[^>]*href=\"https?://[^>]*>").matcher(html);
    int external = 0;
    while (anchors.find()) {
      external++;
      assertThat(anchors.group()).contains("rel=\"noopener\"");
    }
    assertThat(external).isGreaterThan(6);
  }

  @Test
  void healthIsUp() {
    ResponseEntity<String> response = http.getForEntity("/api/health", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getBody()).isEqualTo("{\"status\":\"UP\"}");
  }

  @Test
  void unknownRouteGetsThe404PageNotTheIndex() {
    ResponseEntity<String> response = getHtml("/no/such/page");

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    assertThat(response.getBody())
        .containsPattern(heading("Page not found"))
        .doesNotContainPattern(heading(HOME_HEADING));
  }

  @Test
  void prerenderedRouteIsServedFromItsDirectory() {
    ResponseEntity<String> response = getHtml("/404");

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getBody()).containsPattern(heading("Page not found")).contains(PRERENDERED);
  }

  @Test
  void legalPagesArePrerendered() {
    Map<String, String> legalTerms =
        Map.of("/impressum", "Impressum", "/privacy", "Datenschutzerklärung");
    legalTerms.forEach(
        (path, legalTerm) -> {
          ResponseEntity<String> response = getHtml(path);

          assertThat(response.getStatusCode()).as(path).isEqualTo(HttpStatus.OK);
          assertThat(response.getBody())
              .as(path)
              .containsPattern("<h1[^>]*>\\s*<span[^>]*lang=\"de\"[^>]*>" + legalTerm + "</span>")
              .contains(PRERENDERED);
        });
  }

  @Test
  void staticAssetsAreServed() {
    for (String path : List.of("/favicon.ico", "/brand/obie-logo-solo.svg")) {
      assertThat(http.getForEntity(path, byte[].class).getStatusCode())
          .as(path)
          .isEqualTo(HttpStatus.OK);
    }
  }

  @Test
  void otherActuatorEndpointsAreNotExposed() {
    for (String path : List.of("/api", "/api/env", "/api/beans", "/api/info", "/actuator")) {
      ResponseEntity<String> response = http.getForEntity(path, String.class);
      assertThat(response.getStatusCode()).as(path).isEqualTo(HttpStatus.NOT_FOUND);
    }
  }

  @Test
  void unknownApiPathIsNotAnsweredWithHtmlForApiClients() {
    ResponseEntity<String> response = http.getForEntity("/api/no-such-endpoint", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    assertThat(response.getBody()).doesNotContain("<html");
  }

  /** Matches an {@code <h1>} with the given text, whatever attributes Angular added. */
  private static Pattern heading(String text) {
    return Pattern.compile("<h1[^>]*>" + Pattern.quote(text) + "</h1>");
  }

  private ResponseEntity<String> getHtml(String path) {
    HttpHeaders headers = new HttpHeaders();
    headers.setAccept(List.of(MediaType.TEXT_HTML));
    return http.exchange(path, HttpMethod.GET, new HttpEntity<>(headers), String.class);
  }
}
