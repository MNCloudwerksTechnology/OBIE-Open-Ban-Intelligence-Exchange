package org.obie.website.inquiry;

import java.time.Clock;
import java.time.Duration;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.concurrent.TimeUnit;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

/**
 * Per-client token bucket, in memory: each client may submit {@code capacity} inquiries at once,
 * and the bucket refills continuously, one token every {@code period / capacity}.
 *
 * <p>The bucket is kept in its equivalent "generic cell rate" form: per client only the time at
 * which the bucket will be full again, in whole milliseconds, so there is no rounding. Clients
 * whose bucket is full again are dropped regularly, so memory stays proportional to recently active
 * clients.
 */
@Component
public class ClientRateLimiter {

  /** Time one token takes to refill. */
  private final long refillMillis;

  /** How far in the future "full again" may lie while a token is still left. */
  private final long burstMillis;

  private final Clock clock;
  private final ConcurrentMap<String, Bucket> buckets = new ConcurrentHashMap<>();

  public ClientRateLimiter(InquiryProperties properties, Clock clock) {
    int capacity = properties.rateLimit().capacity();
    this.refillMillis = Math.max(1, properties.rateLimit().period().toMillis() / capacity);
    this.burstMillis = refillMillis * (capacity - 1);
    this.clock = clock;
  }

  /**
   * Takes one token from the client's bucket.
   *
   * @return {@link Duration#ZERO} if the request may proceed, otherwise how long the client has to
   *     wait for the next token
   */
  public Duration acquire(String client) {
    long now = clock.millis();
    // compute() is atomic per key and buckets are immutable, so the returned bucket is exactly
    // the outcome of this call, even with concurrent requests or eviction.
    Bucket bucket =
        buckets.compute(
            client, (key, existing) -> take(existing != null ? existing.fullAt() : now, now));
    return Duration.ofMillis(bucket.waitMillis());
  }

  private Bucket take(long fullAt, long now) {
    long start = Math.max(fullAt, now);
    long wait = start - now - burstMillis;
    return wait > 0 ? new Bucket(fullAt, wait) : new Bucket(start + refillMillis, 0);
  }

  /** Drops buckets that are full again; they behave exactly like a new one. */
  @Scheduled(fixedDelay = 10, timeUnit = TimeUnit.MINUTES)
  public void evictFullBuckets() {
    long now = clock.millis();
    for (String client : buckets.keySet()) {
      buckets.computeIfPresent(client, (key, bucket) -> bucket.fullAt() <= now ? null : bucket);
    }
  }

  /**
   * @param fullAt epoch millis at which the bucket is full again
   * @param waitMillis for the call that produced this bucket: 0 if it got a token, otherwise the
   *     wait for the next one
   */
  private record Bucket(long fullAt, long waitMillis) {}
}
