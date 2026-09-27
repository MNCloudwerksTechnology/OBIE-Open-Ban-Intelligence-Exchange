package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doNothing;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import java.time.Duration;
import java.time.Instant;
import java.time.LocalDate;
import java.util.List;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.obie.website.inquiry.ClientRateLimiterTest.MutableClock;
import org.springframework.mail.MailSendException;

/** Delivery, retry with backoff and giving up, with the mailer and repository mocked. */
class InquiryMailDispatcherTest {

  private final MutableClock clock = new MutableClock();
  private final InquiryRepository repository = mock(InquiryRepository.class);
  private final InquiryMailer mailer = mock(InquiryMailer.class);
  // 4 attempts, retry delay 1 min doubling up to 5 min.
  private final InquiryMailDispatcher dispatcher =
      new InquiryMailDispatcher(repository, mailer, TestProperties.defaults(), clock);

  private Inquiry inquiry;

  @BeforeEach
  void storedInquiry() {
    InquiryRequest request =
        new InquiryRequest(
            InquiryType.WORKSHOP,
            "Grace Hopper",
            "grace@example.org",
            null,
            LocalDate.now().plusDays(10),
            null,
            null,
            "Please run a workshop on OBIE for our team.",
            true,
            null,
            "token");
    inquiry = Inquiry.received(request, "hash", clock.instant());
    when(repository.findTop20ByNextMailAttemptAtLessThanEqualOrderByNextMailAttemptAtAsc(any()))
        .thenAnswer(
            call -> {
              Instant now = call.getArgument(0);
              Instant due = inquiry.getNextMailAttemptAt();
              return due != null && !due.isAfter(now) ? List.of(inquiry) : List.of();
            });
  }

  @Test
  void sendsBothMailsAndMarksThemDelivered() {
    dispatcher.dispatchDueMails();

    verify(mailer).sendNotification(inquiry);
    verify(mailer).sendConfirmation(inquiry);
    verify(repository).save(inquiry);
    assertThat(inquiry.getNotificationSentAt()).isEqualTo(clock.instant());
    assertThat(inquiry.getConfirmationSentAt()).isEqualTo(clock.instant());
    assertThat(inquiry.getNextMailAttemptAt()).isNull();
    assertThat(inquiry.getMailAttempts()).isZero();
  }

  @Test
  void failedMailIsRetriedWithExponentialBackoff() {
    doThrow(new MailSendException("connection refused"))
        .doThrow(new MailSendException("connection refused"))
        .doNothing()
        .when(mailer)
        .sendNotification(inquiry);
    Instant start = clock.instant();

    dispatcher.dispatchDueMails();
    assertThat(inquiry.getMailAttempts()).isEqualTo(1);
    assertThat(inquiry.getNextMailAttemptAt()).isEqualTo(start.plus(Duration.ofMinutes(1)));

    clock.advance(Duration.ofSeconds(59));
    dispatcher.dispatchDueMails();
    assertThat(inquiry.getMailAttempts()).as("not due yet").isEqualTo(1);

    clock.advance(Duration.ofSeconds(1));
    dispatcher.dispatchDueMails();
    assertThat(inquiry.getMailAttempts()).isEqualTo(2);
    assertThat(inquiry.getNextMailAttemptAt())
        .isEqualTo(clock.instant().plus(Duration.ofMinutes(2)));

    clock.advance(Duration.ofMinutes(2));
    dispatcher.dispatchDueMails();
    assertThat(inquiry.getNextMailAttemptAt()).isNull();
    assertThat(inquiry.getNotificationSentAt()).isNotNull();
    assertThat(inquiry.getConfirmationSentAt()).isNotNull();
    verify(mailer, times(3)).sendNotification(inquiry);
    verify(mailer, times(1)).sendConfirmation(inquiry);
  }

  @Test
  void aSentNotificationIsNotSentAgainWhenOnlyTheConfirmationFailed() {
    doThrow(new MailSendException("mailbox unavailable"))
        .doNothing()
        .when(mailer)
        .sendConfirmation(inquiry);

    dispatcher.dispatchDueMails();
    clock.advance(Duration.ofMinutes(1));
    dispatcher.dispatchDueMails();

    verify(mailer, times(1)).sendNotification(inquiry);
    verify(mailer, times(2)).sendConfirmation(inquiry);
    assertThat(inquiry.getNextMailAttemptAt()).isNull();
  }

  @Test
  void givesUpAfterTheLastAttemptButKeepsTheInquiry() {
    doThrow(new MailSendException("connection refused")).when(mailer).sendNotification(inquiry);

    for (int i = 0; i < 10; i++) {
      dispatcher.dispatchDueMails();
      clock.advance(Duration.ofMinutes(5));
    }

    verify(mailer, times(4)).sendNotification(inquiry);
    verify(mailer, never()).sendConfirmation(inquiry);
    verify(repository, never()).delete(any());
    assertThat(inquiry.getMailAttempts()).isEqualTo(4);
    assertThat(inquiry.getNextMailAttemptAt()).isNull();
    assertThat(inquiry.getNotificationSentAt()).isNull();
  }

  @Test
  void retryDelayDoublesUpToTheMaximum() {
    assertThat(dispatcher.retryDelay(1)).isEqualTo(Duration.ofMinutes(1));
    assertThat(dispatcher.retryDelay(2)).isEqualTo(Duration.ofMinutes(2));
    assertThat(dispatcher.retryDelay(3)).isEqualTo(Duration.ofMinutes(4));
    assertThat(dispatcher.retryDelay(4)).isEqualTo(Duration.ofMinutes(5));
    assertThat(dispatcher.retryDelay(1000)).isEqualTo(Duration.ofMinutes(5));
  }

  @Test
  void nothingDueSendsNothing() {
    doNothing().when(mailer).sendNotification(any());
    inquiry.markMailsDelivered();

    dispatcher.dispatchDueMails();

    verify(mailer, never()).sendNotification(any());
    verify(repository, never()).save(any());
  }
}
