package org.obie.website.inquiry;

import jakarta.validation.Valid;
import jakarta.validation.constraints.Email;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Positive;
import jakarta.validation.constraints.Size;
import java.time.Duration;
import java.time.Period;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.validation.annotation.Validated;

/**
 * Settings of the inquiry form ({@code obie.inquiry.*}), bound from the {@code OBIE_*} environment
 * variables in {@code application.properties}. The application refuses to start when a required one
 * is missing.
 *
 * @param recipient address that receives every inquiry
 * @param mailFrom sender address of all mails the website sends
 * @param secret server-side secret for hashing client IPs and signing form tokens
 * @param minFillTime submissions faster than this after the form was rendered count as bots
 * @param rateLimit per-IP limit of submissions
 * @param mail delivery and retry of the notification and confirmation mails
 * @param retention how long inquiries are kept
 */
@Validated
@ConfigurationProperties("obie.inquiry")
public record InquiryProperties(
    @NotBlank @Email String recipient,
    @NotBlank @Email String mailFrom,
    @NotBlank @Size(min = 32) String secret,
    @NotNull Duration minFillTime,
    @NotNull @Valid RateLimit rateLimit,
    @NotNull @Valid Mail mail,
    @NotNull @Valid Retention retention) {

  /**
   * @param capacity submissions allowed per IP within {@code period}
   * @param period time in which a used-up bucket refills completely
   */
  public record RateLimit(@Positive int capacity, @NotNull Duration period) {}

  /**
   * @param pollInterval how often the dispatcher looks for due mails
   * @param maxAttempts delivery attempts before a mail is given up (and logged as an error)
   * @param initialRetryDelay delay after the first failure, doubled after every further one
   * @param maxRetryDelay upper bound of the retry delay
   */
  public record Mail(
      @NotNull Duration pollInterval,
      @Positive int maxAttempts,
      @NotNull Duration initialRetryDelay,
      @NotNull Duration maxRetryDelay) {}

  /**
   * @param maxAge inquiries older than this are deleted
   * @param cron when the deletion job runs (UTC)
   */
  public record Retention(@NotNull Period maxAge, @NotBlank String cron) {}
}
