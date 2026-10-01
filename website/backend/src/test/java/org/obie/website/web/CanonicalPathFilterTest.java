package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.net.URI;
import org.junit.jupiter.api.Test;

/** The redirect targets; SeoTest covers the redirects as a crawler receives them. */
class CanonicalPathFilterTest {

  @Test
  void canonicalPathsAreNotRedirected() {
    assertThat(CanonicalPathFilter.canonical("/", null)).isEmpty();
    assertThat(CanonicalPathFilter.canonical("/de", "utm_source=feed")).isEmpty();
    assertThat(CanonicalPathFilter.canonical("/de/index.csr.html", null)).isEmpty();
  }

  @Test
  void theQueryIsKeptWhenItIsValid() {
    assertThat(CanonicalPathFilter.canonical("/de/", "a=1&b=%C3%A4"))
        .hasValue(URI.create("/de?a=1&b=%C3%A4"));
  }

  @Test
  void nothingIsRedirectedToAnInvalidUrl() {
    assertThat(CanonicalPathFilter.canonical("/de/", "a=1\r\nSet-Cookie:x")).isEmpty();
    assertThat(CanonicalPathFilter.canonical("/de/", "a b")).isEmpty();
  }
}
