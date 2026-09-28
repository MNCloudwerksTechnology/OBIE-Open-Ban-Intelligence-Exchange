package org.obie.website.project;

import java.time.Instant;

/**
 * The project's live stats as {@code GET /api/project} returns them. When no stats could be fetched
 * yet, {@code available} is {@code false} and every other field is {@code null}.
 *
 * @param available whether the other fields hold stats from GitHub
 * @param repositoryUrl web page of the repository
 * @param stars number of stargazers
 * @param forks number of forks
 * @param openIssues open issues as GitHub counts them (including pull requests)
 * @param latestRelease the latest published release, {@code null} if there is none
 * @param lastCommitAt commit date of the newest commit on the default branch
 */
public record ProjectInfo(
    boolean available,
    String repositoryUrl,
    Integer stars,
    Integer forks,
    Integer openIssues,
    Release latestRelease,
    Instant lastCommitAt) {

  private static final ProjectInfo UNAVAILABLE =
      new ProjectInfo(false, null, null, null, null, null, null);

  /** Stats fetched from GitHub. */
  static ProjectInfo of(
      String repositoryUrl,
      int stars,
      int forks,
      int openIssues,
      Release latestRelease,
      Instant lastCommitAt) {
    return new ProjectInfo(
        true, repositoryUrl, stars, forks, openIssues, latestRelease, lastCommitAt);
  }

  /** The answer when GitHub has not delivered stats yet. */
  static ProjectInfo unavailable() {
    return UNAVAILABLE;
  }

  /**
   * @param tag the release's tag, e.g. {@code v0.1.0}
   * @param publishedAt when the release was published
   * @param url web page of the release
   */
  public record Release(String tag, Instant publishedAt, String url) {}
}
