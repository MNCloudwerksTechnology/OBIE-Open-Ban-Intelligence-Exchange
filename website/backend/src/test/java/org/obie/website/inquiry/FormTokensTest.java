package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Clock;
import java.time.Instant;
import java.time.ZoneOffset;
import org.junit.jupiter.api.Test;

class FormTokensTest {

  private static final Instant NOW = Instant.parse("2026-09-28T10:00:00Z");

  private final FormTokens tokens =
      new FormTokens(TestProperties.defaults(), Clock.fixed(NOW, ZoneOffset.UTC));

  @Test
  void issuedTokenCarriesItsIssueTime() {
    assertThat(tokens.issuedAt(tokens.issue())).contains(NOW);
  }

  @Test
  void changedTimestampIsRejected() {
    String token = tokens.issue();
    String earlier = (NOW.toEpochMilli() - 60_000) + token.substring(token.indexOf('.'));

    assertThat(tokens.issuedAt(earlier)).isEmpty();
  }

  @Test
  void tokenOfAnotherSecretIsRejected() {
    InquiryProperties other =
        new InquiryProperties(
            "a@example.org",
            "b@example.org",
            "another-secret-0123456789abcdefghijkl",
            TestProperties.defaults().minFillTime(),
            TestProperties.defaults().formTokenMaxAge(),
            TestProperties.defaults().rateLimit(),
            TestProperties.defaults().mail(),
            TestProperties.defaults().retention());
    String foreign = new FormTokens(other, Clock.fixed(NOW, ZoneOffset.UTC)).issue();

    assertThat(tokens.issuedAt(foreign)).isEmpty();
  }

  @Test
  void malformedTokensAreRejected() {
    assertThat(tokens.issuedAt("")).isEmpty();
    assertThat(tokens.issuedAt("no-dot")).isEmpty();
    assertThat(tokens.issuedAt(".signature")).isEmpty();
    assertThat(tokens.issuedAt("abc.def")).isEmpty();
  }
}
