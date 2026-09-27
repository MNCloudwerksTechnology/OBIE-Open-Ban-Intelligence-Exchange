package org.obie.website.web;

import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.servlet.config.annotation.CorsRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

/**
 * CORS for the API: only the site's own origin may call it from a browser, without credentials.
 * Cross-origin requests from any other origin, including preflights, are answered with 403. The API
 * uses no cookies, so there is no CSRF protection beyond this.
 */
@Configuration(proxyBeanMethods = false)
@EnableConfigurationProperties(WebProperties.class)
public class ApiCorsConfig implements WebMvcConfigurer {

  private final WebProperties properties;

  public ApiCorsConfig(WebProperties properties) {
    this.properties = properties;
  }

  @Override
  public void addCorsMappings(CorsRegistry registry) {
    registry
        .addMapping("/api/**")
        .allowedOrigins(properties.siteOrigin())
        .allowedMethods("GET", "POST")
        .allowedHeaders("Content-Type")
        .allowCredentials(false)
        .maxAge(3600);
  }
}
