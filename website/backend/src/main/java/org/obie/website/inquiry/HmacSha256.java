package org.obie.website.inquiry;

import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

/**
 * HMAC-SHA-256 keyed with the server-side secret. Each use passes its own purpose, which is mixed
 * into the input, so a MAC made for one purpose is never valid for another.
 */
final class HmacSha256 {

  private static final String ALGORITHM = "HmacSHA256";

  private final SecretKeySpec key;

  HmacSha256(String secret) {
    this.key = new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), ALGORITHM);
  }

  byte[] sign(String purpose, String value) {
    try {
      Mac mac = Mac.getInstance(ALGORITHM);
      mac.init(key);
      return mac.doFinal((purpose + ':' + value).getBytes(StandardCharsets.UTF_8));
    } catch (GeneralSecurityException e) {
      // Every Java runtime ships HmacSHA256, so this cannot happen.
      throw new IllegalStateException(ALGORITHM + " is not available", e);
    }
  }
}
