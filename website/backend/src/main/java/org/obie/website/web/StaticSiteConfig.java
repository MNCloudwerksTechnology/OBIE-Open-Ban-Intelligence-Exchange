package org.obie.website.web;

import java.io.IOException;
import java.time.Duration;
import org.springframework.context.annotation.Configuration;
import org.springframework.core.io.Resource;
import org.springframework.http.CacheControl;
import org.springframework.web.servlet.config.annotation.ResourceHandlerRegistration;
import org.springframework.web.servlet.config.annotation.ResourceHandlerRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;
import org.springframework.web.servlet.resource.EncodedResourceResolver;
import org.springframework.web.servlet.resource.PathResourceResolver;

/**
 * Serves the prerendered front end from {@code classpath:/static/}.
 *
 * <p>Angular prerenders every route to {@code <route>/index.html}, so a request for a directory
 * path is answered with that file. Anything else that does not exist is a 404, which {@link
 * NotFoundPageResolver} renders with the prerendered not-found page; there is deliberately no
 * fallback to the index page. Pages are served with the configured origin in their absolute URLs
 * ({@link SiteOrigin}).
 *
 * <p>Files whose names carry the build's content hash (bundles, and everything under {@code
 * media/}) never change, so browsers may keep them for a year. Everything else, pages above all, is
 * revalidated on every use. Where the build wrote a Brotli or gzip variant next to a file, it is
 * served to browsers that accept it (ADR 0015).
 */
@Configuration(proxyBeanMethods = false)
public class StaticSiteConfig implements WebMvcConfigurer {

  static final String STATIC_LOCATION = "classpath:/static/";

  /** Bundles named {@code <name>-<hash>.js|css} by the Angular build ({@code outputHashing}). */
  static final String HASHED_BUNDLES = "/{file:[\\w.-]+-[A-Z0-9]{8}\\.(?:js|css)}";

  static final String HASHED_MEDIA = "/media/**";

  static final CacheControl IMMUTABLE =
      CacheControl.maxAge(Duration.ofDays(365)).cachePublic().immutable();

  private final SiteOrigin siteOrigin;

  public StaticSiteConfig(SiteOrigin siteOrigin) {
    this.siteOrigin = siteOrigin;
  }

  @Override
  public void addResourceHandlers(ResourceHandlerRegistry registry) {
    hashed(registry.addResourceHandler(HASHED_BUNDLES).addResourceLocations(STATIC_LOCATION));
    hashed(
        registry.addResourceHandler(HASHED_MEDIA).addResourceLocations(STATIC_LOCATION + "media/"));
    registry
        .addResourceHandler("/**")
        .addResourceLocations(STATIC_LOCATION)
        .setCacheControl(CacheControl.noCache())
        .resourceChain(true)
        .addResolver(new EncodedResourceResolver())
        .addResolver(new PrerenderedPageResolver())
        .addTransformer(new SiteOriginTransformer(siteOrigin));
  }

  private static void hashed(ResourceHandlerRegistration registration) {
    registration
        .setCacheControl(IMMUTABLE)
        .resourceChain(true)
        .addResolver(new EncodedResourceResolver())
        .addResolver(new PathResourceResolver());
  }

  /** Resolves a path to the file itself or, failing that, to its prerendered index page. */
  static final class PrerenderedPageResolver extends PathResourceResolver {

    private static final String INDEX = "index.html";

    @Override
    protected Resource getResource(String resourcePath, Resource location) throws IOException {
      Resource resource = super.getResource(resourcePath, location);
      if (resource != null) {
        return resource;
      }
      String directory =
          resourcePath.isEmpty() || resourcePath.endsWith("/") ? resourcePath : resourcePath + "/";
      return super.getResource(directory + INDEX, location);
    }
  }
}
