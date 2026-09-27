package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;
import static org.awaitility.Awaitility.await;
import static org.obie.website.inquiry.InquiryFixtures.post;
import static org.obie.website.inquiry.InquiryFixtures.validInquiry;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.net.ServerSocket;
import java.time.Duration;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;

/**
 * With the mail server unreachable, inquiries are still accepted and kept for retries. Uses its own
 * database, so the other test contexts' dispatchers (with a working mail server) cannot deliver
 * this test's mails.
 */
class InquiryMailServerDownTest extends IntegrationTest {

  @DynamicPropertySource
  static void unreachableMailServer(DynamicPropertyRegistry registry) {
    registry.add("spring.mail.port", InquiryMailServerDownTest::closedPort);
    registry.add("spring.datasource.url", () -> separateDatabase("mail_server_down"));
  }

  @Autowired private TestRestTemplate http;
  @Autowired private ObjectMapper json;
  @Autowired private InquiryRepository repository;
  @Autowired private InquiryProperties properties;

  @Test
  void inquiryIsStoredAndMailsAreRetried() throws JsonProcessingException {
    ResponseEntity<String> response = post(http, validInquiry(properties));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
    UUID id = UUID.fromString(json.readTree(response.getBody()).get("id").asText());
    await()
        .atMost(Duration.ofSeconds(15))
        .untilAsserted(
            () -> {
              Inquiry stored = repository.findById(id).orElseThrow();
              assertThat(stored.getMailAttempts()).isGreaterThanOrEqualTo(2);
              assertThat(stored.getNotificationSentAt()).isNull();
            });
  }

  /** A local port nobody listens on. */
  private static int closedPort() {
    try (ServerSocket socket = new ServerSocket(0)) {
      return socket.getLocalPort();
    } catch (IOException e) {
      throw new IllegalStateException(e);
    }
  }
}
