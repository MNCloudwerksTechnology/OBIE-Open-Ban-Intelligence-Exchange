package org.obie.website.project;

import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Serves the project's stats from a server-side cache, so visitors never trigger more than one
 * request to GitHub per cache period. When GitHub fails or rate-limits, the last good stats are
 * served, or {@link ProjectInfo#unavailable()} if there are none; this never throws.
 */
public class ProjectInfoService {

  private static final Logger LOG = LoggerFactory.getLogger(ProjectInfoService.class);

  private final ProjectSource source;
  private final Clock clock;
  private final Duration cacheTtl;
  private final Duration retryDelay;

  private ProjectInfo lastGood = ProjectInfo.unavailable();
  private Instant nextFetch = Instant.MIN;

  /**
   * @param cacheTtl how long fetched stats are served before the source is asked again
   * @param retryDelay after a failure, the source is not asked again before this has passed
   */
  public ProjectInfoService(
      ProjectSource source, Clock clock, Duration cacheTtl, Duration retryDelay) {
    this.source = source;
    this.clock = clock;
    this.cacheTtl = cacheTtl;
    this.retryDelay = retryDelay;
  }

  /**
   * The current stats. Synchronized, so concurrent requests with an expired cache cause a single
   * fetch; the others wait for it (at most the client's timeouts).
   */
  public synchronized ProjectInfo current() {
    Instant now = clock.instant();
    if (now.isBefore(nextFetch)) {
      return lastGood;
    }
    try {
      lastGood = source.fetch();
      nextFetch = now.plus(cacheTtl);
    } catch (RuntimeException e) {
      nextFetch = now.plus(retryDelay);
      LOG.warn(
          "Cannot fetch the project stats from GitHub ({}); serving {} until {}",
          e.toString(),
          lastGood.available() ? "the last good stats" : "\"unavailable\"",
          nextFetch);
    }
    return lastGood;
  }
}
