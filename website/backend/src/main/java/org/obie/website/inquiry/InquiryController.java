package org.obie.website.inquiry;

import jakarta.servlet.http.HttpServletRequest;
import java.time.Duration;
import java.util.UUID;
import org.springframework.http.CacheControl;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ProblemDetail;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/** The inquiry form's API. */
@RestController
@RequestMapping("/api/inquiries")
public class InquiryController {

  private final InquiryService service;
  private final FormTokens formTokens;
  private final ClientRateLimiter rateLimiter;

  public InquiryController(
      InquiryService service, FormTokens formTokens, ClientRateLimiter rateLimiter) {
    this.service = service;
    this.formTokens = formTokens;
    this.rateLimiter = rateLimiter;
  }

  /** A signed form-render timestamp; the front end fetches it when it shows the form. */
  @GetMapping(path = "/form-token", produces = MediaType.APPLICATION_JSON_VALUE)
  public ResponseEntity<FormTokenResponse> formToken() {
    return ResponseEntity.ok()
        .cacheControl(CacheControl.noStore())
        .body(new FormTokenResponse(formTokens.issue()));
  }

  /** Accepts an inquiry: 202 with its id, 400 with field errors, or 429 when rate-limited. */
  @PostMapping(consumes = MediaType.APPLICATION_JSON_VALUE)
  public ResponseEntity<?> submit(@RequestBody InquiryRequest request, HttpServletRequest http) {
    String clientIp = http.getRemoteAddr();
    Duration wait = rateLimiter.acquire(ClientRateLimiter.networkOf(clientIp));
    if (!wait.isZero()) {
      return tooManyRequests(wait);
    }
    UUID id = service.submit(request, clientIp);
    return ResponseEntity.accepted().body(new SubmissionResponse(id));
  }

  private static ResponseEntity<ProblemDetail> tooManyRequests(Duration wait) {
    ProblemDetail problem =
        ProblemDetail.forStatusAndDetail(
            HttpStatus.TOO_MANY_REQUESTS,
            "Too many inquiries from your network. Please try again later.");
    long seconds = Math.max(1, (wait.toMillis() + 999) / 1000);
    return ResponseEntity.status(HttpStatus.TOO_MANY_REQUESTS)
        .header(HttpHeaders.RETRY_AFTER, Long.toString(seconds))
        .body(problem);
  }

  /** Response to a form-token request. */
  public record FormTokenResponse(String token) {}

  /** Response to an accepted inquiry. */
  public record SubmissionResponse(UUID id) {}
}
