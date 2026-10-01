package org.obie.website.inquiry;

import java.time.Clock;
import java.time.Duration;
import java.time.LocalDate;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;

/** Valid inquiries and helpers to post them. */
final class InquiryFixtures {

  private InquiryFixtures() {}

  /** A token for a form rendered long enough ago to pass the minimum fill time. */
  static String oldEnoughToken(InquiryProperties properties) {
    Clock past = Clock.offset(Clock.systemUTC(), Duration.ofSeconds(-30));
    return new FormTokens(properties, past).issue();
  }

  /** A complete, valid inquiry as JSON fields; the name is unique so tests can find it. */
  static Map<String, Object> validInquiry(InquiryProperties properties) {
    Map<String, Object> fields = new LinkedHashMap<>();
    fields.put("type", "talk");
    fields.put("name", "Ada Lovelace " + UUID.randomUUID());
    fields.put("email", "ada@example.org");
    fields.put("organisation", "Analytical Engines Ltd");
    fields.put("eventDate", LocalDate.now().plusMonths(2).toString());
    fields.put("eventLocation", "online");
    fields.put("audienceSize", 120);
    fields.put("message", "Would you give a talk about OBIE at our conference?");
    fields.put("consent", true);
    fields.put("website", "");
    fields.put("formToken", oldEnoughToken(properties));
    return fields;
  }

  static ResponseEntity<String> post(TestRestTemplate http, Object body) {
    return post(http, body, new HttpHeaders());
  }

  static ResponseEntity<String> post(TestRestTemplate http, Object body, HttpHeaders headers) {
    headers.setContentType(MediaType.APPLICATION_JSON);
    headers.setAccept(List.of(MediaType.APPLICATION_JSON, MediaType.APPLICATION_PROBLEM_JSON));
    return http.postForEntity("/api/inquiries", new HttpEntity<>(body, headers), String.class);
  }
}
