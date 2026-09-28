package org.obie.website.web;

import jakarta.servlet.http.HttpServletRequest;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import org.springframework.core.io.Resource;
import org.springframework.http.HttpHeaders;
import org.springframework.web.servlet.resource.HttpResource;
import org.springframework.web.servlet.resource.ResourceTransformer;
import org.springframework.web.servlet.resource.ResourceTransformerChain;
import org.springframework.web.servlet.resource.TransformedResource;

/**
 * Serves the prerendered pages with the configured origin in their absolute URLs ({@link
 * SiteOrigin}). Only HTML files are touched; the resource chain caches the result, so each page is
 * rewritten once.
 */
final class SiteOriginTransformer implements ResourceTransformer {

  private final SiteOrigin siteOrigin;

  SiteOriginTransformer(SiteOrigin siteOrigin) {
    this.siteOrigin = siteOrigin;
  }

  @Override
  public Resource transform(
      HttpServletRequest request, Resource resource, ResourceTransformerChain chain)
      throws IOException {
    Resource resolved = chain.transform(request, resource);
    String filename = resolved.getFilename();
    if (filename == null || !filename.endsWith(".html") || isEncoded(resolved)) {
      return resolved;
    }
    String html;
    try (InputStream in = resolved.getInputStream()) {
      html = new String(in.readAllBytes(), StandardCharsets.UTF_8);
    }
    return new TransformedResource(
        resolved, siteOrigin.applyTo(html).getBytes(StandardCharsets.UTF_8));
  }

  /** A precompressed variant cannot be rewritten; pages are never precompressed. */
  private static boolean isEncoded(Resource resource) {
    return resource instanceof HttpResource http
        && http.getResponseHeaders().containsKey(HttpHeaders.CONTENT_ENCODING);
  }
}
