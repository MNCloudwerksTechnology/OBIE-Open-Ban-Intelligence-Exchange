package org.obie.website.inquiry;

import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.mail.MailException;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

/**
 * Sends the mails of stored inquiries. It polls for inquiries whose mails are due, sends what is
 * still missing and, when the mail server fails, logs it and retries later with exponential
 * backoff. After the last attempt it gives up with an error in the log; the inquiry itself stays
 * stored either way.
 */
@Component
public class InquiryMailDispatcher {

  private static final Logger LOG = LoggerFactory.getLogger(InquiryMailDispatcher.class);

  private final InquiryRepository repository;
  private final InquiryMailer mailer;
  private final InquiryProperties.Mail settings;
  private final Clock clock;

  public InquiryMailDispatcher(
      InquiryRepository repository,
      InquiryMailer mailer,
      InquiryProperties properties,
      Clock clock) {
    this.repository = repository;
    this.mailer = mailer;
    this.settings = properties.mail();
    this.clock = clock;
  }

  /** Delivers the due mails; runs every {@code obie.inquiry.mail.poll-interval}. */
  @Scheduled(
      fixedDelayString = "${obie.inquiry.mail.poll-interval}",
      initialDelayString = "${obie.inquiry.mail.poll-interval}")
  public void dispatchDueMails() {
    for (Inquiry inquiry :
        repository.findTop20ByNextMailAttemptAtLessThanEqualOrderByNextMailAttemptAtAsc(
            clock.instant())) {
      deliver(inquiry);
      repository.save(inquiry);
    }
  }

  private void deliver(Inquiry inquiry) {
    try {
      if (inquiry.getNotificationSentAt() == null) {
        mailer.sendNotification(inquiry);
        inquiry.markNotificationSent(clock.instant());
      }
      if (inquiry.getConfirmationSentAt() == null) {
        mailer.sendConfirmation(inquiry);
        inquiry.markConfirmationSent(clock.instant());
      }
      inquiry.markMailsDelivered();
      LOG.info("Sent the mails of inquiry {}", inquiry.getId());
    } catch (MailException e) {
      scheduleRetry(inquiry, e);
    }
  }

  private void scheduleRetry(Inquiry inquiry, MailException e) {
    int attempt = inquiry.getMailAttempts() + 1;
    if (attempt >= settings.maxAttempts()) {
      inquiry.markMailAttemptFailed(null);
      LOG.error(
          "Giving up on the mails of inquiry {} after {} attempts; it stays stored",
          inquiry.getId(),
          attempt,
          e);
      return;
    }
    Duration delay = retryDelay(attempt);
    Instant next = clock.instant().plus(delay);
    inquiry.markMailAttemptFailed(next);
    LOG.warn(
        "Sending the mails of inquiry {} failed (attempt {} of {}), retrying in {}: {}",
        inquiry.getId(),
        attempt,
        settings.maxAttempts(),
        delay,
        e.toString());
  }

  /** Initial delay doubled for every further failed attempt, capped at the maximum delay. */
  Duration retryDelay(int attempt) {
    Duration delay = settings.initialRetryDelay();
    for (int i = 1; i < attempt && delay.compareTo(settings.maxRetryDelay()) < 0; i++) {
      delay = delay.multipliedBy(2);
    }
    return delay.compareTo(settings.maxRetryDelay()) < 0 ? delay : settings.maxRetryDelay();
  }
}
