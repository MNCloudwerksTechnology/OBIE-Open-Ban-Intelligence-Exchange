package org.obie.website;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

/** Entry point of the OBIE website: static front end plus the API under {@code /api}. */
@SpringBootApplication
public class WebsiteApplication {

  public static void main(String[] args) {
    SpringApplication.run(WebsiteApplication.class, args);
  }
}
