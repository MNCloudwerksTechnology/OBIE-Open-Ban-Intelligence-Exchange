package org.obie.website.inquiry;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Clock;
import java.time.Instant;
import java.util.Base64;
import java.util.Optional;
import org.springframework.stereotype.Component;

/**
 * Signed form-render timestamps. The front end fetches one when it renders the form and sends it
 * back with the submission; the server's own clock and signature make the fill time trustworthy
 * whatever the visitor's clock says.
 *
 * <p>A token is {@code <epoch millis>.<base64url HMAC>}.
 */
@Component
public class FormTokens {

  private static final String PURPOSE = "form-token";
  private static final Base64.Encoder ENCODER = Base64.getUrlEncoder().withoutPadding();

  private final HmacSha256 hmac;
  private final Clock clock;

  public FormTokens(InquiryProperties properties, Clock clock) {
    this.hmac = new HmacSha256(properties.secret());
    this.clock = clock;
  }

  /** A token for a form rendered now. */
  public String issue() {
    String issuedAt = Long.toString(clock.millis());
    return issuedAt + '.' + ENCODER.encodeToString(hmac.sign(PURPOSE, issuedAt));
  }

  /** When the token was issued, or empty if it is malformed or not signed by this server. */
  public Optional<Instant> issuedAt(String token) {
    int dot = token.indexOf('.');
    if (dot <= 0) {
      return Optional.empty();
    }
    String issuedAt = token.substring(0, dot);
    byte[] expected =
        ENCODER.encodeToString(hmac.sign(PURPOSE, issuedAt)).getBytes(StandardCharsets.US_ASCII);
    byte[] actual = token.substring(dot + 1).getBytes(StandardCharsets.US_ASCII);
    if (!MessageDigest.isEqual(expected, actual)) {
      return Optional.empty();
    }
    try {
      return Optional.of(Instant.ofEpochMilli(Long.parseLong(issuedAt)));
    } catch (NumberFormatException e) {
      return Optional.empty();
    }
  }
}
