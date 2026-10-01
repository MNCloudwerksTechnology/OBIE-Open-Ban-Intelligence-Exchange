package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.test.context.TestPropertySource;

/**
 * Behind a reverse proxy, the deployment sets {@code SERVER_FORWARD_HEADERS_STRATEGY=native}: the
 * rate limit must then see the visitor's address from {@code X-Forwarded-For}, not the proxy's. The
 * test client connects from 127.0.0.1, which Tomcat trusts as an internal proxy.
 */
@TestPropertySource(
    properties = {"server.forward-headers-strategy=native", "obie.inquiry.rate-limit.capacity=2"})
class ForwardedHeadersTest extends IntegrationTest {

  private static final String VISITOR = "203.0.113.10";
  private static final String OTHER_VISITOR = "198.51.100.20";

  @Autowired private TestRestTemplate http;

  @Test
  void theRateLimitCountsPerForwardedClientAddress() {
    // Every request counts against the limit, rejected ones included.
    assertThat(postEmptyInquiryFrom(VISITOR)).isEqualTo(HttpStatus.BAD_REQUEST);
    assertThat(postEmptyInquiryFrom(VISITOR)).isEqualTo(HttpStatus.BAD_REQUEST);
    assertThat(postEmptyInquiryFrom(VISITOR)).isEqualTo(HttpStatus.TOO_MANY_REQUESTS);

    // Same proxy, another visitor: a fresh allowance.
    assertThat(postEmptyInquiryFrom(OTHER_VISITOR)).isEqualTo(HttpStatus.BAD_REQUEST);
  }

  private HttpStatus postEmptyInquiryFrom(String clientAddress) {
    HttpHeaders headers = new HttpHeaders();
    headers.setContentType(MediaType.APPLICATION_JSON);
    headers.setAccept(List.of(MediaType.APPLICATION_JSON, MediaType.APPLICATION_PROBLEM_JSON));
    headers.set("X-Forwarded-For", clientAddress);
    headers.set("X-Forwarded-Proto", "https");
    return HttpStatus.valueOf(
        http.postForEntity("/api/inquiries", new HttpEntity<>(Map.of(), headers), String.class)
            .getStatusCode()
            .value());
  }
}
