package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;
import static org.obie.website.inquiry.InquiryFixtures.post;
import static org.obie.website.inquiry.InquiryFixtures.validInquiry;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.test.context.TestPropertySource;

/** The per-IP rate limit, with a small bucket so the test does not need many requests. */
@TestPropertySource(properties = "obie.inquiry.rate-limit.capacity=2")
class InquiryRateLimitTest extends IntegrationTest {

  @Autowired private TestRestTemplate http;
  @Autowired private ObjectMapper json;
  @Autowired private InquiryRepository repository;
  @Autowired private InquiryProperties properties;

  @Test
  void submissionsBeyondTheLimitAreRejectedWith429AndNotStored() throws JsonProcessingException {
    assertThat(post(http, validInquiry(properties)).getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
    // Bot submissions count against the limit, too.
    Map<String, Object> bot = validInquiry(properties);
    bot.put("website", "filled");
    assertThat(post(http, bot).getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);

    Map<String, Object> fields = validInquiry(properties);
    ResponseEntity<String> response = post(http, fields);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.TOO_MANY_REQUESTS);
    // 2 per hour: the next token is due in 30 minutes.
    assertThat(Long.parseLong(response.getHeaders().getFirst(HttpHeaders.RETRY_AFTER)))
        .isBetween(1700L, 1800L);
    assertThat(json.readTree(response.getBody()).get("status").asInt()).isEqualTo(429);
    assertThat(repository.findAll())
        .noneMatch(inquiry -> inquiry.getName().equals(fields.get("name")));
  }
}
