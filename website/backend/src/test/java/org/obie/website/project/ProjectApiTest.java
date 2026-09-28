package org.obie.website.project;

import static org.assertj.core.api.Assertions.assertThat;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;

/**
 * {@code GET /api/project} against a mock GitHub. The cache period is zero here, so every request
 * reaches the mock and a failure shows at once; the cache itself is covered by {@link
 * ProjectInfoServiceTest}.
 */
class ProjectApiTest extends IntegrationTest {

  private static final MockGithub GITHUB = MockGithub.start(); // stopped at JVM exit

  @DynamicPropertySource
  static void github(DynamicPropertyRegistry registry) {
    registry.add("obie.github.api-url", GITHUB::url);
    registry.add("obie.github.cache-ttl", () -> "PT0S");
    registry.add("obie.github.retry-delay", () -> "PT0S");
  }

  @Autowired private TestRestTemplate http;
  @Autowired private ObjectMapper json;

  @BeforeEach
  void healthyGithub() {
    GITHUB.healthy();
  }

  @Test
  void servesTheStatsFromGithub() throws JsonProcessingException {
    ResponseEntity<String> response = http.getForEntity("/api/project", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getHeaders().getCacheControl()).isEqualTo("max-age=60");
    assertThat(json.readTree(response.getBody()))
        .isEqualTo(
            json.readTree(
                """
                {"available": true,
                 "repositoryUrl": "{WEB_URL}",
                 "stars": 42, "forks": 7, "openIssues": 3,
                 "latestRelease": {"tag": "v0.1.0", "publishedAt": "2026-09-01T12:00:00Z",
                                   "url": "{WEB_URL}/releases/tag/v0.1.0"},
                 "lastCommitAt": "2026-09-20T10:30:00Z"}
                """
                    .replace("{WEB_URL}", MockGithub.WEB_URL)));
  }

  @Test
  void servesTheLastGoodStatsWhileGithubIsRateLimited() throws JsonProcessingException {
    JsonNode good = json.readTree(http.getForObject("/api/project", String.class));
    GITHUB.rateLimited();

    ResponseEntity<String> response = http.getForEntity("/api/project", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(json.readTree(response.getBody())).isEqualTo(good);
    assertThat(good.get("available").asBoolean()).isTrue();
  }

  @Test
  void latestReleaseIsNullWithoutARelease() throws JsonProcessingException {
    GITHUB.respond(MockGithub.REPOSITORY_PATH + "/releases/latest", 404, "{}");

    JsonNode body = json.readTree(http.getForObject("/api/project", String.class));

    assertThat(body.get("available").asBoolean()).isTrue();
    assertThat(body.get("latestRelease").isNull()).isTrue();
  }
}
