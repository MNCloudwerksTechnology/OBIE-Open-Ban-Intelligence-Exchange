package org.obie.website.inquiry;

import java.time.Duration;
import java.time.Period;

/** {@link InquiryProperties} for unit tests. */
final class TestProperties {

  static final String SECRET = "unit-test-secret-0123456789abcdefghij";

  private TestProperties() {}

  static InquiryProperties defaults() {
    return withRateLimit(5, Duration.ofHours(1));
  }

  static InquiryProperties withRateLimit(int capacity, Duration period) {
    return new InquiryProperties(
        "markus@obie.example",
        "website@obie.example",
        SECRET,
        Duration.ofSeconds(3),
        Duration.ofDays(1),
        new InquiryProperties.RateLimit(capacity, period),
        new InquiryProperties.Mail(
            Duration.ofSeconds(5), 4, Duration.ofMinutes(1), Duration.ofMinutes(5)),
        new InquiryProperties.Retention(Period.ofMonths(12), "0 30 3 * * *"));
  }
}
