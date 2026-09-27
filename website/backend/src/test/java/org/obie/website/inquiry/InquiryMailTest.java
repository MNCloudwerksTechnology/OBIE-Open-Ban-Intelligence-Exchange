package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;
import static org.awaitility.Awaitility.await;
import static org.obie.website.inquiry.InquiryFixtures.post;
import static org.obie.website.inquiry.InquiryFixtures.validInquiry;

import com.fasterxml.jackson.databind.ObjectMapper;
import jakarta.mail.Address;
import jakarta.mail.Message.RecipientType;
import jakarta.mail.MessagingException;
import jakarta.mail.internet.InternetAddress;
import jakarta.mail.internet.MimeMessage;
import java.io.IOException;
import java.time.Duration;
import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;

/** The mails of an accepted inquiry, received by the GreenMail SMTP server. */
class InquiryMailTest extends IntegrationTest {

  @Autowired private TestRestTemplate http;
  @Autowired private ObjectMapper json;
  @Autowired private InquiryRepository repository;
  @Autowired private InquiryProperties properties;

  @Test
  void operatorGetsTheInquiryAndSenderGetsAConfirmation() throws IOException, MessagingException {
    Map<String, Object> fields = validInquiry(properties);
    String email = "ada+" + UUID.randomUUID() + "@example.org";
    fields.put("email", email);
    String name = (String) fields.get("name");

    UUID id = UUID.fromString(json.readTree(post(http, fields).getBody()).get("id").asText());

    MimeMessage notification =
        awaitMail(properties.recipient(), "[OBIE inquiry] talk from " + name);
    assertThat(addresses(notification.getFrom())).containsExactly("website@obie.example");
    assertThat(addresses(notification.getReplyTo())).containsExactly(email);
    assertThat(((InternetAddress) notification.getReplyTo()[0]).getPersonal()).isEqualTo(name);
    assertThat(notification.getContentType()).startsWith("text/plain");
    assertThat((String) notification.getContent())
        .contains(
            name, email, "Analytical Engines Ltd", "online", "120", (String) fields.get("message"))
        .contains(id.toString());

    MimeMessage confirmation = awaitMail(email, InquiryMailer.CONFIRMATION_SUBJECT);
    assertThat(addresses(confirmation.getFrom())).containsExactly("website@obie.example");
    assertThat(confirmation.getContentType()).startsWith("text/plain");
    assertThat(((String) confirmation.getContent()).strip())
        .isEqualToNormalizingNewlines(InquiryMailer.CONFIRMATION_TEXT.strip())
        .doesNotContain(name);

    await()
        .atMost(Duration.ofSeconds(10))
        .untilAsserted(
            () -> {
              Inquiry stored = repository.findById(id).orElseThrow();
              assertThat(stored.getNotificationSentAt()).isNotNull();
              assertThat(stored.getConfirmationSentAt()).isNotNull();
              assertThat(stored.getNextMailAttemptAt()).isNull();
            });
  }

  private static MimeMessage awaitMail(String recipient, String subject) {
    return await()
        .atMost(Duration.ofSeconds(10))
        .until(() -> find(recipient, subject), message -> message != null);
  }

  private static MimeMessage find(String recipient, String subject)
      throws IOException, MessagingException {
    for (MimeMessage message : SMTP.getReceivedMessages()) {
      if (subject.equals(message.getSubject())
          && addresses(message.getRecipients(RecipientType.TO)).contains(recipient)) {
        return message;
      }
    }
    return null;
  }

  private static List<String> addresses(Address[] addresses) {
    return Arrays.stream(addresses).map(a -> ((InternetAddress) a).getAddress()).toList();
  }
}
