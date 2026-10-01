package org.obie.website.inquiry;

import java.time.Clock;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.scheduling.annotation.EnableScheduling;

/** Wiring of the inquiry feature: settings, a clock, and the scheduled mail and retention jobs. */
@Configuration(proxyBeanMethods = false)
@EnableScheduling
@EnableConfigurationProperties(InquiryProperties.class)
public class InquiryConfig {

  @Bean
  Clock clock() {
    return Clock.systemUTC();
  }
}
