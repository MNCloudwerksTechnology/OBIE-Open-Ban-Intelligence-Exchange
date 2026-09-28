package org.obie.website.project;

import static org.assertj.core.api.Assertions.assertThat;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

/**
 * {@code GET /api/project} when GitHub cannot be reached at all (the test profile points it at a
 * closed port) and has never delivered stats.
 */
class ProjectApiUnavailableTest extends IntegrationTest {

  @Autowired private TestRestTemplate http;
  @Autowired private ObjectMapper json;

  @Test
  void answersUnavailableInsteadOfAnError() throws JsonProcessingException {
    ResponseEntity<String> response = http.getForEntity("/api/project", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(json.readTree(response.getBody()))
        .isEqualTo(
            json.readTree(
                """
                {"available": false, "repositoryUrl": null, "stars": null, "forks": null,
                 "openIssues": null, "latestRelease": null, "lastCommitAt": null}
                """));
  }
}
