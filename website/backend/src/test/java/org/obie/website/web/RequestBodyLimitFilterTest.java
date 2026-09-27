package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequestWrapper;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.Objects;
import org.junit.jupiter.api.Test;
import org.springframework.mock.web.MockFilterChain;
import org.springframework.mock.web.MockHttpServletRequest;
import org.springframework.mock.web.MockHttpServletResponse;
import org.springframework.util.unit.DataSize;

/** The body limit for requests that declare no length (chunked transfer encoding). */
class RequestBodyLimitFilterTest {

  private final RequestBodyLimitFilter filter =
      new RequestBodyLimitFilter(new WebProperties("https://obie.example", DataSize.ofBytes(16)));

  @Test
  void bodyWithinTheLimitIsPassedOnUnchanged() throws IOException, ServletException {
    MockFilterChain chain = new MockFilterChain();
    MockHttpServletResponse response = new MockHttpServletResponse();

    filter.doFilter(chunked("0123456789abcdef"), response, chain);

    assertThat(response.getStatus()).isEqualTo(200);
    byte[] passedOn = Objects.requireNonNull(chain.getRequest()).getInputStream().readAllBytes();
    assertThat(new String(passedOn, StandardCharsets.UTF_8)).isEqualTo("0123456789abcdef");
  }

  @Test
  void bodyOverTheLimitIsRejectedWithoutCallingTheApplication()
      throws IOException, ServletException {
    MockFilterChain chain = new MockFilterChain();
    MockHttpServletResponse response = new MockHttpServletResponse();

    filter.doFilter(chunked("0123456789abcdefX"), response, chain);

    assertThat(response.getStatus()).isEqualTo(413);
    assertThat(response.getContentType()).isEqualTo("application/problem+json");
    assertThat(response.getContentAsString()).contains("\"status\":413");
    assertThat(chain.getRequest()).isNull();
  }

  @Test
  void pagesAreNotLimited() throws IOException, ServletException {
    MockHttpServletRequest request = new MockHttpServletRequest("GET", "/index.html");
    MockFilterChain chain = new MockFilterChain();

    filter.doFilter(request, new MockHttpServletResponse(), chain);

    assertThat(chain.getRequest()).isSameAs(request);
  }

  /** A POST to the API whose length is unknown up front. */
  private static HttpServletRequestWrapper chunked(String body) {
    MockHttpServletRequest request = new MockHttpServletRequest("POST", "/api/inquiries");
    request.setContent(body.getBytes(StandardCharsets.UTF_8));
    return new HttpServletRequestWrapper(request) {
      @Override
      public int getContentLength() {
        return -1;
      }

      @Override
      public long getContentLengthLong() {
        return -1;
      }
    };
  }
}
