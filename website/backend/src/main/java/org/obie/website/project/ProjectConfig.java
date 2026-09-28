package org.obie.website.project;

import java.time.Clock;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.client.RestClient;

/** Wiring of the project stats: GitHub as the source, behind the cache. */
@Configuration(proxyBeanMethods = false)
@EnableConfigurationProperties(GithubProperties.class)
public class ProjectConfig {

  @Bean
  ProjectInfoService projectInfoService(
      GithubProperties properties, RestClient.Builder restClientBuilder, Clock clock) {
    return new ProjectInfoService(
        new GithubClient(properties, restClientBuilder),
        clock,
        properties.cacheTtl(),
        properties.retryDelay());
  }
}
