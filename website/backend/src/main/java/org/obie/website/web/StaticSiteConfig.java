package org.obie.website.web;

import java.io.IOException;
import org.springframework.context.annotation.Configuration;
import org.springframework.core.io.Resource;
import org.springframework.web.servlet.config.annotation.ResourceHandlerRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;
import org.springframework.web.servlet.resource.PathResourceResolver;

/**
 * Serves the prerendered front end from {@code classpath:/static/}.
 *
 * <p>Angular prerenders every route to {@code <route>/index.html}, so a request for a directory
 * path is answered with that file. Anything else that does not exist is a 404, which {@link
 * NotFoundPageResolver} renders with the prerendered not-found page; there is deliberately no
 * fallback to the index page.
 */
@Configuration(proxyBeanMethods = false)
public class StaticSiteConfig implements WebMvcConfigurer {

  static final String STATIC_LOCATION = "classpath:/static/";

  @Override
  public void addResourceHandlers(ResourceHandlerRegistry registry) {
    registry
        .addResourceHandler("/**")
        .addResourceLocations(STATIC_LOCATION)
        .resourceChain(true)
        .addResolver(new PrerenderedPageResolver());
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
