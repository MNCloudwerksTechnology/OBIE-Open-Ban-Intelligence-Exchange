package org.obie.website.project;

import java.time.Duration;
import org.springframework.http.CacheControl;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * The project's live GitHub stats for the page. Always 200: when there are no stats, the body says
 * {@code "available": false}. The visitor's browser only talks to this endpoint, never to GitHub.
 */
@RestController
public class ProjectController {

  /** Browsers may reuse an answer briefly; the server-side cache does the real work. */
  static final Duration BROWSER_MAX_AGE = Duration.ofMinutes(1);

  private final ProjectInfoService service;

  public ProjectController(ProjectInfoService service) {
    this.service = service;
  }

  @GetMapping(path = "/api/project", produces = MediaType.APPLICATION_JSON_VALUE)
  public ResponseEntity<ProjectInfo> project() {
    return ResponseEntity.ok()
        .cacheControl(CacheControl.maxAge(BROWSER_MAX_AGE))
        .body(service.current());
  }
}
