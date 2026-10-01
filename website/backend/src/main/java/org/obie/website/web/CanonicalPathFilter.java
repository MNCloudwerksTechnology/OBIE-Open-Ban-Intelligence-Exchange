package org.obie.website.web;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.Optional;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

/**
 * Keeps every page at the one URL its canonical link and the sitemap name, so search engines never
 * see the same page under several URLs (ADR 0036).
 *
 * <p>A page requested with a trailing slash ({@code /de/}) or by its file ({@code
 * /impressum/index.html}) is redirected permanently to its canonical path, with the query string
 * kept. The redirect is relative, so it stays on the host the visitor used. The not-found pages
 * ({@code /404}, {@code /de/404}) answer 404 when requested by their own path, as any unknown URL
 * does. The API is left alone.
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE + 2)
public class CanonicalPathFilter extends OncePerRequestFilter {

  /**
   * A path ending in a slash or {@code /index.html}; group 1 is the path without it. Segments may
   * not be empty or hold a backslash, so the target can never be read as another host ({@code
   * //host}, {@code /\host}).
   */
  private static final Pattern NON_CANONICAL =
      Pattern.compile("^((?:/[^/\\\\]+)*)/(?:index\\.html)?$");

  /** The not-found page of the default language or of one under its prefix (ADR 0033). */
  private static final Pattern NOT_FOUND_PAGE = Pattern.compile("^(?:/[a-z]{2})?/404$");

  @Override
  protected boolean shouldNotFilter(HttpServletRequest request) {
    String method = request.getMethod();
    return !(method.equals("GET") || method.equals("HEAD"))
        || request.getRequestURI().startsWith("/api/");
  }

  @Override
  protected void doFilterInternal(
      HttpServletRequest request, HttpServletResponse response, FilterChain chain)
      throws ServletException, IOException {
    String path = request.getRequestURI();
    Optional<URI> canonical = canonical(path, request.getQueryString());
    if (canonical.isPresent()) {
      response.setStatus(HttpStatus.MOVED_PERMANENTLY.value());
      response.setHeader(HttpHeaders.LOCATION, canonical.get().toASCIIString());
      return;
    }
    if (NOT_FOUND_PAGE.matcher(path).matches()) {
      response.sendError(HttpStatus.NOT_FOUND.value());
      return;
    }
    chain.doFilter(request, response);
  }

  /**
   * The relative URL to redirect a non-canonical {@code path} to, with {@code query} kept; empty
   * when the path is canonical or the result would not be a valid URI.
   */
  static Optional<URI> canonical(String path, String query) {
    Matcher nonCanonical = NON_CANONICAL.matcher(path);
    if (path.equals("/") || !nonCanonical.matches()) {
      return Optional.empty();
    }
    String target = nonCanonical.group(1).isEmpty() ? "/" : nonCanonical.group(1);
    try {
      return Optional.of(new URI(query == null ? target : target + "?" + query));
    } catch (URISyntaxException e) {
      return Optional.empty();
    }
  }
}
