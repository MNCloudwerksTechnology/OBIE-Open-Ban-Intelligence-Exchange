package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Arrays;
import java.util.List;
import java.util.Map;
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

/** Security headers on pages, API and error responses, and CORS of the API. */
class SecurityHeadersTest extends IntegrationTest {

  private static final String SITE = "https://obie.example";
  private static final String OTHER_SITE = "https://evil.example";

  @Autowired private TestRestTemplate http;

  @ParameterizedTest
  @ValueSource(strings = {"/", "/no/such/page", "/api/health", "/api/inquiries/form-token"})
  void everyResponseCarriesTheSecurityHeaders(String path) {
    HttpHeaders headers = get(path, MediaType.TEXT_HTML).getHeaders();

    assertThat(headers.getFirst("Strict-Transport-Security"))
        .isEqualTo("max-age=31536000; includeSubDomains");
    assertThat(headers.getFirst("X-Content-Type-Options")).isEqualTo("nosniff");
    assertThat(headers.getFirst("X-Frame-Options")).isEqualTo("DENY");
    assertThat(headers.getFirst("Referrer-Policy")).isEqualTo("no-referrer");
    assertThat(headers.getFirst("Permissions-Policy")).contains("camera=()", "geolocation=()");
    assertThat(directives(headers.getFirst("Content-Security-Policy")))
        .containsEntry("default-src", "'self'")
        .containsEntry("frame-ancestors", "'none'")
        .containsEntry("object-src", "'none'")
        .containsEntry("base-uri", "'self'");
  }

  @Test
  void scriptsAreRestrictedToOwnFilesAndTheHashesOfThePagesInlineScripts() {
    ResponseEntity<String> page = get("/", MediaType.TEXT_HTML);
    String scriptSrc =
        directives(page.getHeaders().getFirst("Content-Security-Policy")).get("script-src");

    assertThat(scriptSrc).startsWith("'self'").doesNotContain("unsafe-inline", "unsafe-eval");
    // Every inline script the browser would run on the page is allowed by its hash.
    assertThat(InlineScriptHashes.inHtml(page.getBody()))
        .isNotEmpty()
        .allSatisfy(hash -> assertThat(scriptSrc).contains(hash));
  }

  @Test
  void onlyTheSelfHostedMatomoIsAllowedBesidesTheSite() {
    Map<String, String> csp =
        directives(get("/", MediaType.TEXT_HTML).getHeaders().getFirst("Content-Security-Policy"));
    String matomo = "https://metrics.cloudwerks.de";

    assertThat(csp.get("script-src")).startsWith("'self' " + matomo + " ");
    assertThat(csp.get("connect-src")).isEqualTo("'self' " + matomo);
    assertThat(csp.get("img-src")).isEqualTo("'self' data: " + matomo);
    assertThat(csp)
        .containsEntry("default-src", "'self'")
        .containsEntry("font-src", "'self'")
        .containsEntry("style-src", "'self' 'unsafe-inline'")
        .containsEntry("form-action", "'self'");
  }

  @Test
  void preflightFromTheSiteIsAllowed() {
    ResponseEntity<String> response = preflight(SITE);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getHeaders().getAccessControlAllowOrigin()).isEqualTo(SITE);
    assertThat(response.getHeaders().getAccessControlAllowCredentials()).isFalse();
  }

  @Test
  void preflightFromAnotherOriginIsRejected() {
    ResponseEntity<String> response = preflight(OTHER_SITE);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.FORBIDDEN);
    assertThat(response.getHeaders().getAccessControlAllowOrigin()).isNull();
  }

  @Test
  void requestFromAnotherOriginIsRejected() {
    HttpHeaders headers = new HttpHeaders();
    headers.setOrigin(OTHER_SITE);
    headers.setContentType(MediaType.APPLICATION_JSON);
    ResponseEntity<String> response =
        http.exchange(
            "/api/inquiries", HttpMethod.POST, new HttpEntity<>("{}", headers), String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.FORBIDDEN);
  }

  private ResponseEntity<String> preflight(String origin) {
    HttpHeaders headers = new HttpHeaders();
    headers.setOrigin(origin);
    headers.setAccessControlRequestMethod(HttpMethod.POST);
    headers.setAccessControlRequestHeaders(List.of("Content-Type"));
    return http.exchange(
        "/api/inquiries", HttpMethod.OPTIONS, new HttpEntity<>(headers), String.class);
  }

  private ResponseEntity<String> get(String path, MediaType accept) {
    HttpHeaders headers = new HttpHeaders();
    headers.setAccept(List.of(accept, MediaType.APPLICATION_JSON));
    return http.exchange(path, HttpMethod.GET, new HttpEntity<>(headers), String.class);
  }

  /** CSP directive name to its value. */
  private static Map<String, String> directives(String policy) {
    assertThat(policy).isNotNull();
    return Arrays.stream(policy.split(";\\s*"))
        .map(directive -> directive.split(" ", 2))
        .collect(
            java.util.stream.Collectors.toMap(
                parts -> parts[0], parts -> parts.length > 1 ? parts[1] : ""));
  }
}
