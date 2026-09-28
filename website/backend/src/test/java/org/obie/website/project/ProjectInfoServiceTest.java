package org.obie.website.project;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneId;
import java.time.ZoneOffset;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.function.Supplier;
import org.junit.jupiter.api.Test;

class ProjectInfoServiceTest {

  private static final Duration TTL = Duration.ofMinutes(15);
  private static final Duration RETRY_DELAY = Duration.ofMinutes(1);

  private static final ProjectInfo STATS =
      ProjectInfo.of(MockGithub.WEB_URL, 42, 7, 3, null, Instant.parse("2026-09-20T10:30:00Z"));
  private static final ProjectInfo NEWER_STATS =
      ProjectInfo.of(MockGithub.WEB_URL, 43, 7, 2, null, Instant.parse("2026-09-21T09:00:00Z"));

  private final MutableClock clock = new MutableClock(Instant.parse("2026-09-28T12:00:00Z"));
  private final ScriptedSource source = new ScriptedSource();
  private final ProjectInfoService service =
      new ProjectInfoService(source, clock, TTL, RETRY_DELAY);

  @Test
  void servesCachedStatsWithoutAskingGithubAgainWithinTheCachePeriod() {
    source.next(() -> STATS);

    assertThat(service.current()).isEqualTo(STATS);
    clock.advance(TTL.minusSeconds(1));
    assertThat(service.current()).isEqualTo(STATS);

    assertThat(source.calls).isEqualTo(1);
  }

  @Test
  void fetchesAgainOnceTheCachePeriodIsOver() {
    source.next(() -> STATS).next(() -> NEWER_STATS);

    service.current();
    clock.advance(TTL);

    assertThat(service.current()).isEqualTo(NEWER_STATS);
    assertThat(source.calls).isEqualTo(2);
  }

  @Test
  void servesTheLastGoodStatsWhenGithubFails() {
    source.next(() -> STATS).next(ScriptedSource::rateLimited);

    service.current();
    clock.advance(TTL);

    assertThat(service.current()).isEqualTo(STATS);
    assertThat(source.calls).isEqualTo(2);
  }

  @Test
  void answersUnavailableWhenGithubFailsBeforeTheFirstSuccess() {
    source.next(ScriptedSource::rateLimited);

    ProjectInfo info = service.current();

    assertThat(info.available()).isFalse();
    assertThat(info).isEqualTo(ProjectInfo.unavailable());
  }

  @Test
  void waitsTheRetryDelayAfterAFailureBeforeAskingAgain() {
    source.next(ScriptedSource::rateLimited).next(() -> STATS);

    service.current();
    clock.advance(RETRY_DELAY.minusSeconds(1));
    assertThat(service.current().available()).isFalse();
    assertThat(source.calls).isEqualTo(1);

    clock.advance(Duration.ofSeconds(1));
    assertThat(service.current()).isEqualTo(STATS);
    assertThat(source.calls).isEqualTo(2);
  }

  /** Answers each fetch with the next scripted result. */
  private static final class ScriptedSource implements ProjectSource {

    private final Deque<Supplier<ProjectInfo>> results = new ArrayDeque<>();
    int calls;

    ScriptedSource next(Supplier<ProjectInfo> result) {
      results.add(result);
      return this;
    }

    @Override
    public ProjectInfo fetch() {
      calls++;
      return results.remove().get();
    }

    static ProjectInfo rateLimited() {
      throw new IllegalStateException("403 API rate limit exceeded");
    }
  }

  private static final class MutableClock extends Clock {

    private Instant now;

    MutableClock(Instant now) {
      this.now = now;
    }

    void advance(Duration duration) {
      now = now.plus(duration);
    }

    @Override
    public Instant instant() {
      return now;
    }

    @Override
    public ZoneId getZone() {
      return ZoneOffset.UTC;
    }

    @Override
    public Clock withZone(ZoneId zone) {
      throw new UnsupportedOperationException();
    }
  }
}
