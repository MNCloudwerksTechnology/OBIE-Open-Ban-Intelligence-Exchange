package org.obie.website.web;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ReadListener;
import jakarta.servlet.ServletException;
import jakarta.servlet.ServletInputStream;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletRequestWrapper;
import jakarta.servlet.http.HttpServletResponse;
import java.io.BufferedReader;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.Charset;
import java.nio.charset.StandardCharsets;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

/**
 * Rejects API requests whose body exceeds {@code obie.web.max-request-body} with 413, before
 * anything parses them. A declared Content-Length is checked up front; bodies without one (chunked)
 * are read up to the limit and then handed on from memory.
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE + 1)
public class RequestBodyLimitFilter extends OncePerRequestFilter {

  static final String TOO_LARGE =
      "{\"type\":\"about:blank\",\"title\":\"Payload Too Large\",\"status\":413,"
          + "\"detail\":\"The request is too large.\"}";

  private final int limit;

  public RequestBodyLimitFilter(WebProperties properties) {
    this.limit = Math.toIntExact(properties.maxRequestBody().toBytes());
  }

  @Override
  protected boolean shouldNotFilter(HttpServletRequest request) {
    return !request.getRequestURI().startsWith("/api/");
  }

  @Override
  protected void doFilterInternal(
      HttpServletRequest request, HttpServletResponse response, FilterChain chain)
      throws ServletException, IOException {
    if (request.getContentLengthLong() > limit) {
      reject(response);
      return;
    }
    byte[] body = request.getInputStream().readNBytes(limit + 1);
    if (body.length > limit) {
      reject(response);
      return;
    }
    chain.doFilter(new BufferedBodyRequest(request, body), response);
  }

  private static void reject(HttpServletResponse response) throws IOException {
    response.setStatus(HttpStatus.PAYLOAD_TOO_LARGE.value());
    response.setContentType(MediaType.APPLICATION_PROBLEM_JSON_VALUE);
    // The rest of the body is not read; closing the connection spares Tomcat from draining it.
    response.setHeader("Connection", "close");
    response.getOutputStream().write(TOO_LARGE.getBytes(StandardCharsets.UTF_8));
  }

  /** The original request with its body replaced by the bytes already read. */
  private static final class BufferedBodyRequest extends HttpServletRequestWrapper {

    private final byte[] body;

    BufferedBodyRequest(HttpServletRequest request, byte[] body) {
      super(request);
      this.body = body.clone();
    }

    @Override
    public ServletInputStream getInputStream() {
      return new ByteArrayServletInputStream(body);
    }

    @Override
    public BufferedReader getReader() {
      String encoding = getCharacterEncoding();
      Charset charset = encoding != null ? Charset.forName(encoding) : StandardCharsets.UTF_8;
      return new BufferedReader(new InputStreamReader(getInputStream(), charset));
    }

    @Override
    public int getContentLength() {
      return body.length;
    }

    @Override
    public long getContentLengthLong() {
      return body.length;
    }
  }

  private static final class ByteArrayServletInputStream extends ServletInputStream {

    private final ByteArrayInputStream in;

    ByteArrayServletInputStream(byte[] body) {
      this.in = new ByteArrayInputStream(body);
    }

    @Override
    public int read() {
      return in.read();
    }

    @Override
    public int read(byte[] buffer, int offset, int length) {
      return in.read(buffer, offset, length);
    }

    @Override
    public boolean isFinished() {
      return in.available() == 0;
    }

    @Override
    public boolean isReady() {
      return true;
    }

    @Override
    public void setReadListener(ReadListener listener) {
      throw new UnsupportedOperationException("Asynchronous reads are not supported");
    }
  }
}
