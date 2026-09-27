package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Clock;
import java.time.Instant;
import java.time.ZoneOffset;
import org.junit.jupiter.api.Test;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;

/** The retention job against the real database. */
class InquiryRetentionTest extends IntegrationTest {

  @Autowired private InquiryRepository repository;
  @Autowired private InquiryRetentionJob job;
  @Autowired private Clock clock;

  @Test
  void deletesInquiriesOlderThanTwelveMonthsAndKeepsTheRest() {
    Instant now = clock.instant();
    Inquiry expired = store(now.atOffset(ZoneOffset.UTC).minusMonths(12).minusDays(1).toInstant());
    Inquiry almostExpired =
        store(now.atOffset(ZoneOffset.UTC).minusMonths(12).plusDays(1).toInstant());
    Inquiry recent = store(now);

    job.deleteExpired();

    assertThat(repository.existsById(expired.getId())).isFalse();
    assertThat(repository.existsById(almostExpired.getId())).isTrue();
    assertThat(repository.existsById(recent.getId())).isTrue();
  }

  private Inquiry store(Instant createdAt) {
    InquiryRequest request =
        new InquiryRequest(
            InquiryType.OTHER,
            "Retention Test",
            "retention@example.org",
            null,
            null,
            null,
            null,
            "An inquiry to test the retention job.",
            true,
            null,
            "token");
    Inquiry inquiry = Inquiry.received(request, "hash", createdAt);
    // Mails are not the subject here; keep the dispatcher away from these rows.
    inquiry.markMailsDelivered();
    return repository.save(inquiry);
  }
}
