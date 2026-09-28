package org.obie.website.project;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.sun.net.httpserver.Headers;
import java.time.Duration;
import java.time.Instant;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.web.client.RestClient;
import org.springframework.web.client.RestClientException;

class GithubClientTest {

  private MockGithub github;

  @BeforeEach
  void startGithub() {
    github = MockGithub.start().healthy();
  }

  @AfterEach
  void stopGithub() {
    github.close();
  }

  private GithubClient client(String token, Duration timeout) {
    return new GithubClient(
        new GithubProperties(
            github.url(),
            MockGithub.REPOSITORY,
            token,
            Duration.ofMinutes(15),
            Duration.ofMinutes(1),
            timeout),
        RestClient.builder());
  }

  private GithubClient client() {
    return client("", Duration.ofSeconds(2));
  }

  @Test
  void readsStarsForksIssuesLatestReleaseAndLastCommit() {
    ProjectInfo info = client().fetch();

    assertThat(info)
        .isEqualTo(
            new ProjectInfo(
                true,
                MockGithub.WEB_URL,
                42,
                7,
                3,
                new ProjectInfo.Release(
                    "v0.1.0",
                    Instant.parse("2026-09-01T12:00:00Z"),
                    MockGithub.WEB_URL + "/releases/tag/v0.1.0"),
                Instant.parse("2026-09-20T10:30:00Z")));
  }

  @Test
  void asksForTheNewestCommitOnTheDefaultBranch() {
    client().fetch();

    assertThat(github.requestedUris())
        .contains(MockGithub.REPOSITORY_PATH + "/commits?sha=main&per_page=1");
  }

  @Test
  void identifiesItselfAndSendsNoTokenWithoutOne() {
    client().fetch();

    Headers request = github.requests().getFirst();
    assertThat(request.getFirst("Accept")).isEqualTo("application/vnd.github+json");
    assertThat(request.getFirst("X-GitHub-Api-Version")).isEqualTo(GithubClient.API_VERSION);
    assertThat(request.getFirst("User-Agent")).isEqualTo(GithubClient.USER_AGENT);
    assertThat(request.containsKey("Authorization")).isFalse();
  }

  @Test
  void sendsTheConfiguredTokenOnEveryRequest() {
    client("ghp_test-token", Duration.ofSeconds(2)).fetch();

    assertThat(github.requests())
        .hasSize(3)
        .allSatisfy(
            request ->
                assertThat(request.getFirst("Authorization")).isEqualTo("Bearer ghp_test-token"));
  }

  @Test
  void latestReleaseIsNullWhenThereIsNone() {
    github.respond(MockGithub.REPOSITORY_PATH + "/releases/latest", 404, "{\"message\":\"x\"}");

    ProjectInfo info = client().fetch();

    assertThat(info.available()).isTrue();
    assertThat(info.latestRelease()).isNull();
    assertThat(info.stars()).isEqualTo(42);
  }

  @Test
  void failsWhenRateLimited() {
    github.rateLimited();

    assertThatThrownBy(() -> client().fetch()).isInstanceOf(RestClientException.class);
  }

  @Test
  void failsOnServerErrors() {
    github.respond(MockGithub.REPOSITORY_PATH + "/commits", 502, "{}");

    assertThatThrownBy(() -> client().fetch()).isInstanceOf(RestClientException.class);
  }

  @Test
  void failsWhenGithubIsTooSlow() {
    github.respondSlowly(MockGithub.REPOSITORY_PATH, MockGithub.REPOSITORY_JSON, 1_500);

    assertThatThrownBy(() -> client("", Duration.ofMillis(300)).fetch())
        .isInstanceOf(RestClientException.class);
  }

  @Test
  void failsOnResponsesOfAnotherShape() {
    github.respond(
        MockGithub.REPOSITORY_PATH, 200, "{\"html_url\": \"" + MockGithub.WEB_URL + "\"}");

    assertThatThrownBy(() -> client().fetch()).isInstanceOf(IllegalStateException.class);
  }

  @Test
  void failsWithoutACommit() {
    github.respond(MockGithub.REPOSITORY_PATH + "/commits", 200, "[]");

    assertThatThrownBy(() -> client().fetch()).isInstanceOf(IllegalStateException.class);
  }

  @Test
  void rejectsLinksThatAreNotHttps() {
    github.respond(
        MockGithub.REPOSITORY_PATH,
        200,
        MockGithub.REPOSITORY_JSON.replace(MockGithub.WEB_URL, "javascript:alert(1)"));

    assertThatThrownBy(() -> client().fetch())
        .isInstanceOf(IllegalStateException.class)
        .hasMessageContaining("not https");
  }

  @Test
  void propertiesNeverPrintTheToken() {
    GithubProperties properties =
        new GithubProperties(
            github.url(),
            MockGithub.REPOSITORY,
            "ghp_secret",
            Duration.ofMinutes(15),
            Duration.ofMinutes(1),
            Duration.ofSeconds(5));

    assertThat(properties.toString()).doesNotContain("ghp_secret").contains("token=***");
  }
}
