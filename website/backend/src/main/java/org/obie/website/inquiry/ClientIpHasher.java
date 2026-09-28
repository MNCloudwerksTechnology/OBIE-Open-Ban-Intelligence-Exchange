package org.obie.website.inquiry;

import java.util.HexFormat;
import org.springframework.stereotype.Component;

/**
 * Turns a client IP into a salted hash, so inquiries from the same address can be recognised
 * without the address ever being stored.
 */
@Component
public class ClientIpHasher {

  private static final String PURPOSE = "client-ip";

  private final HmacSha256 hmac;

  public ClientIpHasher(InquiryProperties properties) {
    this.hmac = new HmacSha256(properties.secret());
  }

  /** The hash as 64 lower-case hex digits. */
  public String hash(String clientIp) {
    return HexFormat.of().formatHex(hmac.sign(PURPOSE, clientIp));
  }
}
