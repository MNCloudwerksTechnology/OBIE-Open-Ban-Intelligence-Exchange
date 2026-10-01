package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class PrerenderedPagesTest {

  @Test
  void listsEveryPrerenderedRouteExceptTheNotFoundPagesHomeFirst() {
    assertThat(PrerenderedPages.publicPaths("classpath:/prerendered-fixture/"))
        .containsExactly("/", "/de", "/docs/guide", "/privacy");
  }
}
