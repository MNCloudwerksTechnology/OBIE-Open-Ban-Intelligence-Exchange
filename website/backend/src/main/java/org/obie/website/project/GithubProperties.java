package org.obie.website.project;

import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import java.net.URI;
import java.time.Duration;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.validation.annotation.Validated;

/**
 * Where the project's live GitHub stats come from ({@code obie.github.*}), bound from the {@code
 * OBIE_GITHUB_*} environment variables in {@code application.properties}.
 *
 * @param apiUrl base URL of the GitHub REST API
 * @param repository {@code owner/name} of the repository
 * @param token optional access token; without one GitHub allows 60 requests per hour and IP
 * @param cacheTtl how long fetched stats are served before GitHub is asked again
 * @param retryDelay after a failed fetch, GitHub is not asked again before this has passed
 * @param timeout connect and read timeout of each request to GitHub
 */
@Validated
@ConfigurationProperties("obie.github")
public record GithubProperties(
    @NotNull URI apiUrl,
    @NotNull
        @Pattern(
            regexp = "[A-Za-z0-9-]+/[A-Za-z0-9._-]+",
            message = "must be owner/name of a GitHub repository")
        String repository,
    String token,
    @NotNull Duration cacheTtl,
    @NotNull Duration retryDelay,
    @NotNull Duration timeout) {

  /** Whether a token is configured; an empty environment variable counts as none. */
  boolean hasToken() {
    return token != null && !token.isBlank();
  }

  /** Keeps the token out of logs and error messages. */
  @Override
  public String toString() {
    return "GithubProperties[apiUrl=%s, repository=%s, token=%s, cacheTtl=%s, retryDelay=%s, timeout=%s]"
        .formatted(apiUrl, repository, hasToken() ? "***" : "none", cacheTtl, retryDelay, timeout);
  }
}
