package org.obie.website.inquiry;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;
import java.time.LocalDate;
import java.util.UUID;

/**
 * A stored inquiry. Besides the form fields it tracks the delivery of its two mails (notification
 * to the operator, confirmation to the sender), so the table doubles as the mail outbox.
 */
@Entity
@Table(name = "inquiry")
public class Inquiry {

  @Id private UUID id;

  @Column(name = "created_at", nullable = false)
  private Instant createdAt;

  @Enumerated(EnumType.STRING)
  @Column(nullable = false)
  private InquiryType type;

  @Column(nullable = false)
  private String name;

  @Column(nullable = false)
  private String email;

  private String organisation;

  @Column(name = "event_date")
  private LocalDate eventDate;

  @Column(name = "event_location")
  private String eventLocation;

  @Column(name = "audience_size")
  private Integer audienceSize;

  @Column(nullable = false)
  private String message;

  @Enumerated(EnumType.STRING)
  @Column(nullable = false)
  private InquiryStatus status;

  @Column(name = "client_ip_hash", nullable = false)
  private String clientIpHash;

  @Column(name = "notification_sent_at")
  private Instant notificationSentAt;

  @Column(name = "confirmation_sent_at")
  private Instant confirmationSentAt;

  @Column(name = "mail_attempts", nullable = false)
  private int mailAttempts;

  /** When the dispatcher should (re)try the pending mails; {@code null} once nothing is due. */
  @Column(name = "next_mail_attempt_at")
  private Instant nextMailAttemptAt;

  /** For JPA. */
  protected Inquiry() {}

  /** A new inquiry from a validated request, with both mails due at once. */
  static Inquiry received(InquiryRequest request, String clientIpHash, Instant now) {
    Inquiry inquiry = new Inquiry();
    inquiry.id = UUID.randomUUID();
    inquiry.createdAt = now;
    inquiry.type = request.type();
    inquiry.name = request.name().strip();
    inquiry.email = request.email().strip();
    inquiry.organisation = blankToNull(request.organisation());
    inquiry.eventDate = request.eventDate();
    inquiry.eventLocation = blankToNull(request.eventLocation());
    inquiry.audienceSize = request.audienceSize();
    inquiry.message = request.message().strip();
    inquiry.status = InquiryStatus.NEW;
    inquiry.clientIpHash = clientIpHash;
    inquiry.nextMailAttemptAt = now;
    return inquiry;
  }

  private static String blankToNull(String value) {
    return value == null || value.isBlank() ? null : value.strip();
  }

  void markNotificationSent(Instant now) {
    notificationSentAt = now;
  }

  void markConfirmationSent(Instant now) {
    confirmationSentAt = now;
  }

  /** Both mails went out: nothing left to deliver. */
  void markMailsDelivered() {
    nextMailAttemptAt = null;
  }

  /** Counts a failed delivery and schedules the next attempt, or none to give up. */
  void markMailAttemptFailed(Instant nextAttempt) {
    mailAttempts++;
    nextMailAttemptAt = nextAttempt;
  }

  public UUID getId() {
    return id;
  }

  public Instant getCreatedAt() {
    return createdAt;
  }

  public InquiryType getType() {
    return type;
  }

  public String getName() {
    return name;
  }

  public String getEmail() {
    return email;
  }

  public String getOrganisation() {
    return organisation;
  }

  public LocalDate getEventDate() {
    return eventDate;
  }

  public String getEventLocation() {
    return eventLocation;
  }

  public Integer getAudienceSize() {
    return audienceSize;
  }

  public String getMessage() {
    return message;
  }

  public InquiryStatus getStatus() {
    return status;
  }

  public String getClientIpHash() {
    return clientIpHash;
  }

  public Instant getNotificationSentAt() {
    return notificationSentAt;
  }

  public Instant getConfirmationSentAt() {
    return confirmationSentAt;
  }

  public int getMailAttempts() {
    return mailAttempts;
  }

  public Instant getNextMailAttemptAt() {
    return nextMailAttemptAt;
  }
}
