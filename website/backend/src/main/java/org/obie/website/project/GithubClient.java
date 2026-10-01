package org.obie.website.project;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.net.http.HttpClient;
import java.time.Instant;
import java.util.List;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.client.JdkClientHttpRequestFactory;
import org.springframework.web.client.RestClient;

/**
 * Reads the repository's stats from the GitHub REST API: the repository itself, its latest release
 * and the newest commit on its default branch. Every problem, including rate limiting, a timeout or
 * a response of an unexpected shape, surfaces as a {@link RuntimeException}.
 */
public final class GithubClient implements ProjectSource {

  static final String API_VERSION = "2022-11-28";
  static final String USER_AGENT = "obie-website";

  private final RestClient http;
  private final String owner;
  private final String name;

  public GithubClient(GithubProperties properties, RestClient.Builder builder) {
    // Redirects are not followed, so the token is never sent anywhere else.
    HttpClient client = HttpClient.newBuilder().connectTimeout(properties.timeout()).build();
    JdkClientHttpRequestFactory requestFactory = new JdkClientHttpRequestFactory(client);
    requestFactory.setReadTimeout(properties.timeout());
    this.http =
        builder
            .clone()
            .baseUrl(properties.apiUrl().toString())
            .requestFactory(requestFactory)
            .defaultHeaders(
                headers -> {
                  headers.setAccept(List.of(MediaType.valueOf("application/vnd.github+json")));
                  headers.set("X-GitHub-Api-Version", API_VERSION);
                  headers.set(HttpHeaders.USER_AGENT, USER_AGENT);
                  if (properties.hasToken()) {
                    headers.setBearerAuth(properties.token().strip());
                  }
                })
            .build();
    String[] parts = properties.repository().split("/", 2);
    this.owner = parts[0];
    this.name = parts[1];
  }

  @Override
  public ProjectInfo fetch() {
    RepositoryJson repository =
        required(
            http.get()
                .uri("/repos/{owner}/{name}", owner, name)
                .retrieve()
                .body(RepositoryJson.class),
            "repository");
    CommitJson[] commits =
        http.get()
            .uri(
                "/repos/{owner}/{name}/commits?sha={branch}&per_page=1",
                owner,
                name,
                required(repository.defaultBranch(), "default_branch"))
            .retrieve()
            .body(CommitJson[].class);
    if (commits == null || commits.length == 0) {
      throw new IllegalStateException("GitHub returned no commit on the default branch");
    }
    return ProjectInfo.of(
        webUrl(repository.htmlUrl()),
        count(repository.stars(), "stargazers_count"),
        count(repository.forks(), "forks_count"),
        count(repository.openIssues(), "open_issues_count"),
        latestRelease(),
        required(commits[0].committedAt(), "commit date"));
  }

  /** The latest published release; GitHub answers 404 when there is none. */
  private ProjectInfo.Release latestRelease() {
    ReleaseJson release =
        http.get()
            .uri("/repos/{owner}/{name}/releases/latest", owner, name)
            .retrieve()
            .onStatus(status -> status == HttpStatus.NOT_FOUND, (request, response) -> {})
            .body(ReleaseJson.class);
    if (release == null || release.tagName() == null) {
      return null;
    }
    return new ProjectInfo.Release(
        release.tagName(),
        required(release.publishedAt(), "published_at"),
        webUrl(release.htmlUrl()));
  }

  private static <T> T required(T value, String field) {
    if (value == null) {
      throw new IllegalStateException("GitHub response lacks " + field);
    }
    return value;
  }

  private static int count(Integer value, String field) {
    if (required(value, field) < 0) {
      throw new IllegalStateException("GitHub returned a negative " + field);
    }
    return value;
  }

  /** The page links to these URLs, so anything but an https URL is rejected. */
  private static String webUrl(String url) {
    if (required(url, "html_url").startsWith("https://")) {
      return url;
    }
    throw new IllegalStateException("GitHub returned a URL that is not https: " + url);
  }

  @JsonIgnoreProperties(ignoreUnknown = true)
  record RepositoryJson(
      @JsonProperty("html_url") String htmlUrl,
      @JsonProperty("stargazers_count") Integer stars,
      @JsonProperty("forks_count") Integer forks,
      @JsonProperty("open_issues_count") Integer openIssues,
      @JsonProperty("default_branch") String defaultBranch) {}

  @JsonIgnoreProperties(ignoreUnknown = true)
  record ReleaseJson(
      @JsonProperty("tag_name") String tagName,
      @JsonProperty("published_at") Instant publishedAt,
      @JsonProperty("html_url") String htmlUrl) {}

  @JsonIgnoreProperties(ignoreUnknown = true)
  record CommitJson(Commit commit) {

    Instant committedAt() {
      return commit == null || commit.committer() == null ? null : commit.committer().date();
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    record Commit(Signature committer) {}

    @JsonIgnoreProperties(ignoreUnknown = true)
    record Signature(Instant date) {}
  }
}
