package org.obie.website.inquiry;

import jakarta.validation.ConstraintViolation;
import jakarta.validation.Validator;
import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.util.Optional;
import java.util.Set;
import java.util.UUID;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

/**
 * Accepts inquiries: recognises bots, validates and stores the rest. Mails are sent later by the
 * {@link InquiryMailDispatcher}, so a submission never waits for or fails because of the mail
 * server.
 */
@Service
public class InquiryService {

  private static final Logger LOG = LoggerFactory.getLogger(InquiryService.class);

  private final InquiryRepository repository;
  private final Validator validator;
  private final FormTokens formTokens;
  private final ClientIpHasher ipHasher;
  private final Duration minFillTime;
  private final Clock clock;

  public InquiryService(
      InquiryRepository repository,
      Validator validator,
      FormTokens formTokens,
      ClientIpHasher ipHasher,
      InquiryProperties properties,
      Clock clock) {
    this.repository = repository;
    this.validator = validator;
    this.formTokens = formTokens;
    this.ipHasher = ipHasher;
    this.minFillTime = properties.minFillTime();
    this.clock = clock;
  }

  /**
   * Stores a genuine inquiry. Bots get a made-up id and nothing is stored, so they cannot tell that
   * they were caught.
   *
   * @return the id to report to the client
   * @throws InvalidInquiryException if a person made a mistake in the form
   */
  public UUID submit(InquiryRequest request, String clientIp) {
    Instant now = clock.instant();
    Optional<String> botReason = botReason(request, now);
    if (botReason.isPresent()) {
      LOG.info("Discarded an inquiry as automated: {}", botReason.get());
      return UUID.randomUUID();
    }
    Set<ConstraintViolation<InquiryRequest>> violations = validator.validate(request);
    if (!violations.isEmpty()) {
      throw new InvalidInquiryException(violations);
    }
    Inquiry inquiry = repository.save(Inquiry.received(request, ipHasher.hash(clientIp), now));
    LOG.info("Stored inquiry {}", inquiry.getId());
    return inquiry.getId();
  }

  /**
   * Why the request looks automated: a filled honeypot, a form token this server did not sign, or a
   * form filled faster than a person can. A missing token is left to validation, so a broken front
   * end shows up as an error instead of silently losing inquiries.
   */
  private Optional<String> botReason(InquiryRequest request, Instant now) {
    if (request.website() != null && !request.website().isEmpty()) {
      return Optional.of("honeypot filled");
    }
    if (request.formToken() == null || request.formToken().isBlank()) {
      return Optional.empty();
    }
    Optional<Instant> renderedAt = formTokens.issuedAt(request.formToken());
    if (renderedAt.isEmpty()) {
      return Optional.of("invalid form token");
    }
    if (Duration.between(renderedAt.get(), now).compareTo(minFillTime) < 0) {
      return Optional.of("form filled too fast");
    }
    return Optional.empty();
  }
}
