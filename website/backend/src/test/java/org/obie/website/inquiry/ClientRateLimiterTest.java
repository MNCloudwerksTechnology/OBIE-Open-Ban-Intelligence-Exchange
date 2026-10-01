package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneId;
import java.time.ZoneOffset;
import org.junit.jupiter.api.Test;

class ClientRateLimiterTest {

  private final MutableClock clock = new MutableClock();
  private final ClientRateLimiter limiter =
      new ClientRateLimiter(TestProperties.withRateLimit(5, Duration.ofHours(1)), clock);

  @Test
  void allowsCapacityAtOnceThenAsksToWaitForTheNextToken() {
    for (int i = 0; i < 5; i++) {
      assertThat(limiter.acquire("203.0.113.7")).as("request %d", i + 1).isZero();
    }

    assertThat(limiter.acquire("203.0.113.7")).isEqualTo(Duration.ofMinutes(12));
  }

  @Test
  void refillsContinuously() {
    for (int i = 0; i < 5; i++) {
      limiter.acquire("203.0.113.7");
    }

    clock.advance(Duration.ofMinutes(11));
    assertThat(limiter.acquire("203.0.113.7")).isEqualTo(Duration.ofMinutes(1));
    clock.advance(Duration.ofMinutes(1));
    assertThat(limiter.acquire("203.0.113.7")).isZero();
    assertThat(limiter.acquire("203.0.113.7")).isPositive();
  }

  @Test
  void deniedRequestsDoNotUseTokens() {
    for (int i = 0; i < 10; i++) {
      limiter.acquire("203.0.113.7");
    }

    clock.advance(Duration.ofMinutes(12));
    assertThat(limiter.acquire("203.0.113.7")).isZero();
  }

  @Test
  void clientsHaveSeparateBuckets() {
    for (int i = 0; i < 5; i++) {
      limiter.acquire("203.0.113.7");
    }

    assertThat(limiter.acquire("198.51.100.1")).isZero();
  }

  @Test
  void evictionKeepsBucketsThatAreNotFullYet() {
    for (int i = 0; i < 5; i++) {
      limiter.acquire("203.0.113.7");
    }
    clock.advance(Duration.ofMinutes(30));

    limiter.evictFullBuckets();

    assertThat(limiter.acquire("203.0.113.7")).isZero();
    assertThat(limiter.acquire("203.0.113.7")).isZero();
    assertThat(limiter.acquire("203.0.113.7")).isPositive();
  }

  @Test
  void evictedFullBucketStartsFull() {
    limiter.acquire("203.0.113.7");
    clock.advance(Duration.ofHours(1));

    limiter.evictFullBuckets();

    for (int i = 0; i < 5; i++) {
      assertThat(limiter.acquire("203.0.113.7")).isZero();
    }
    assertThat(limiter.acquire("203.0.113.7")).isPositive();
  }

  @Test
  void ipv4AddressesAreLimitedIndividually() {
    assertThat(ClientRateLimiter.networkOf("203.0.113.7")).isEqualTo("203.0.113.7");
  }

  @Test
  void ipv6AddressesAreLimitedPerSlash64() {
    String network = ClientRateLimiter.networkOf("2001:db8:1:2:aaaa::1");

    assertThat(network).isEqualTo("20010db800010002::/64");
    assertThat(ClientRateLimiter.networkOf("2001:db8:1:2:ffff:ffff:ffff:ffff")).isEqualTo(network);
    assertThat(ClientRateLimiter.networkOf("2001:db8:1:3::1")).isNotEqualTo(network);
  }

  /** A clock tests can move forward. */
  static final class MutableClock extends Clock {
    private Instant now = Instant.parse("2026-09-28T10:00:00Z");

    void advance(Duration duration) {
      now = now.plus(duration);
    }

    @Override
    public ZoneId getZone() {
      return ZoneOffset.UTC;
    }

    @Override
    public Clock withZone(ZoneId zone) {
      throw new UnsupportedOperationException();
    }

    @Override
    public Instant instant() {
      return now;
    }
  }
}
