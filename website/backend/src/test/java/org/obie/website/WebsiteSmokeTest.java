package org.obie.website;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.regex.Pattern;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.SpringBootTest.WebEnvironment;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;

/** Boots the whole application and checks what a visitor and a monitor see. */
@SpringBootTest(webEnvironment = WebEnvironment.RANDOM_PORT)
class WebsiteSmokeTest {

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
    assertThat(response.getBody()).containsPattern(heading("OBIE")).contains(PRERENDERED);
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
        .doesNotContainPattern(heading("OBIE"));
  }

  @Test
  void prerenderedRouteIsServedFromItsDirectory() {
    ResponseEntity<String> response = getHtml("/404");

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getBody()).containsPattern(heading("Page not found")).contains(PRERENDERED);
  }

  @Test
  void staticAssetsAreServed() {
    assertThat(http.getForEntity("/favicon.ico", byte[].class).getStatusCode())
        .isEqualTo(HttpStatus.OK);
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
