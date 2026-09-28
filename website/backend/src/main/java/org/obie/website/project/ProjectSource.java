package org.obie.website.project;

/** Fetches the project's current stats; {@link GithubClient} in production. */
@FunctionalInterface
interface ProjectSource {

  /**
   * Fetches fresh stats.
   *
   * @throws RuntimeException when the stats cannot be fetched, whatever the reason
   */
  ProjectInfo fetch();
}
