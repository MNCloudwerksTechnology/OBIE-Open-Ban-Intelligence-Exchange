package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class PrerenderedPagesTest {

  @Test
  void listsEveryPrerenderedRouteExceptTheNotFoundPageHomeFirst() {
    assertThat(PrerenderedPages.publicPaths("classpath:/prerendered-fixture/"))
        .containsExactly("/", "/docs/guide", "/privacy");
  }
}
