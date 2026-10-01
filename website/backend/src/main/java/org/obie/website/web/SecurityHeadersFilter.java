package org.obie.website.web;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import java.util.Set;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

/**
 * Sets the security headers on every response, pages and API alike, including error responses.
 *
 * <p>The Content Security Policy allows only the site's own resources, plus the self-hosted Matomo
 * ({@link #ANALYTICS_ORIGIN}) for scripts, tracking requests and its fallback pixel: the front end
 * loads it only after the visitor consents to visitor statistics (ADR 0034). Scripts are restricted
 * to files from the site and Matomo plus the hashes of the prerendered pages' inline scripts, never
 * {@code 'unsafe-inline'}; styles need {@code 'unsafe-inline'} because Angular inlines critical and
 * component styles.
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE)
public class SecurityHeadersFilter extends OncePerRequestFilter {

  /**
   * Origin of the self-hosted Matomo. Keep it in step with {@code ANALYTICS_ORIGIN} in the front
   * end's {@code core/analytics/analytics.config.ts}; a front-end test compares the two.
   */
  static final String ANALYTICS_ORIGIN = "https://metrics.cloudwerks.de";

  static final String PERMISSIONS_POLICY =
      "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), "
          + "microphone=(), payment=(), usb=()";

  private final String contentSecurityPolicy;

  public SecurityHeadersFilter() {
    this(InlineScriptHashes.of(StaticSiteConfig.STATIC_LOCATION));
  }

  SecurityHeadersFilter(Set<String> inlineScriptHashes) {
    this.contentSecurityPolicy = contentSecurityPolicy(inlineScriptHashes);
  }

  static String contentSecurityPolicy(Set<String> inlineScriptHashes) {
    String scriptSources = String.join(" ", inlineScriptHashes);
    return String.join(
        "; ",
        "default-src 'self'",
        ("script-src 'self' " + ANALYTICS_ORIGIN + " " + scriptSources).strip(),
        "style-src 'self' 'unsafe-inline'",
        "img-src 'self' data: " + ANALYTICS_ORIGIN,
        "font-src 'self'",
        "connect-src 'self' " + ANALYTICS_ORIGIN,
        "object-src 'none'",
        "base-uri 'self'",
        "form-action 'self'",
        "frame-ancestors 'none'");
  }

  @Override
  protected void doFilterInternal(
      HttpServletRequest request, HttpServletResponse response, FilterChain chain)
      throws ServletException, IOException {
    response.setHeader("Content-Security-Policy", contentSecurityPolicy);
    response.setHeader("Strict-Transport-Security", "max-age=31536000; includeSubDomains");
    response.setHeader("X-Content-Type-Options", "nosniff");
    response.setHeader("X-Frame-Options", "DENY");
    response.setHeader("Referrer-Policy", "no-referrer");
    response.setHeader("Permissions-Policy", PERMISSIONS_POLICY);
    response.setHeader("Cross-Origin-Opener-Policy", "same-origin");
    chain.doFilter(request, response);
  }

  /** Error pages are rendered in a separate dispatch; they need the headers too. */
  @Override
  protected boolean shouldNotFilterErrorDispatch() {
    return false;
  }
}
