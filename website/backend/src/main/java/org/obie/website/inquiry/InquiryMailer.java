package org.obie.website.inquiry;

import jakarta.mail.internet.InternetAddress;
import java.nio.charset.StandardCharsets;
import java.util.Objects;
import org.springframework.boot.autoconfigure.mail.MailProperties;
import org.springframework.mail.MailException;
import org.springframework.mail.javamail.JavaMailSender;
import org.springframework.mail.javamail.MimeMessageHelper;
import org.springframework.stereotype.Component;

/** Writes and sends the two plain-text mails of an inquiry. */
@Component
public final class InquiryMailer {

  static final String CONFIRMATION_SUBJECT = "Your inquiry to OBIE has been received";

  /**
   * Deliberately contains nothing the visitor typed: anyone can enter someone else's address, and
   * the confirmation must not become a way to send them arbitrary text.
   */
  static final String CONFIRMATION_TEXT =
      """
      Hello,

      thank you for your inquiry. It has reached Markus Niewerth, who reads every \
      inquiry personally and will get back to you, usually within a few days.

      If you did not send an inquiry through the OBIE website, someone else entered \
      your address; please ignore this mail. You will not receive further mails.

      OBIE, the Open Ban Intelligence Exchange
      """;

  private final JavaMailSender mailSender;
  private final InquiryProperties properties;

  public InquiryMailer(
      JavaMailSender mailSender, MailProperties mailProperties, InquiryProperties properties) {
    // Without a host every mail would fail only later, one retry at a time; fail at startup.
    if (mailProperties.getHost() == null || mailProperties.getHost().isBlank()) {
      throw new IllegalStateException("No SMTP host configured: set OBIE_SMTP_HOST");
    }
    this.mailSender = mailSender;
    this.properties = properties;
  }

  /** Sends the inquiry to the operator, with Reply-To set to the visitor. */
  public void sendNotification(Inquiry inquiry) throws MailException {
    mailSender.send(
        message -> {
          MimeMessageHelper mail = new MimeMessageHelper(message, StandardCharsets.UTF_8.name());
          mail.setFrom(properties.mailFrom());
          mail.setTo(properties.recipient());
          mail.setReplyTo(
              new InternetAddress(
                  inquiry.getEmail(), inquiry.getName(), StandardCharsets.UTF_8.name()));
          mail.setSubject(notificationSubject(inquiry));
          mail.setText(notificationText(inquiry));
        });
  }

  /** Sends the visitor a short confirmation. */
  public void sendConfirmation(Inquiry inquiry) throws MailException {
    mailSender.send(
        message -> {
          MimeMessageHelper mail = new MimeMessageHelper(message, StandardCharsets.UTF_8.name());
          mail.setFrom(properties.mailFrom());
          mail.setTo(inquiry.getEmail());
          mail.setSubject(CONFIRMATION_SUBJECT);
          mail.setText(CONFIRMATION_TEXT);
        });
  }

  static String notificationSubject(Inquiry inquiry) {
    return "[OBIE inquiry] " + inquiry.getType().jsonValue() + " from " + inquiry.getName();
  }

  static String notificationText(Inquiry inquiry) {
    return String.join(
        "\n",
        "New inquiry through the OBIE website. Reply to this mail to answer "
            + inquiry.getName()
            + ".",
        "",
        "Type:           " + inquiry.getType().jsonValue(),
        "Name:           " + inquiry.getName(),
        "E-mail:         " + inquiry.getEmail(),
        "Organisation:   " + orDash(inquiry.getOrganisation()),
        "Event date:     " + orDash(inquiry.getEventDate()),
        "Event location: " + orDash(inquiry.getEventLocation()),
        "Audience size:  " + orDash(inquiry.getAudienceSize()),
        "",
        "Message:",
        "",
        inquiry.getMessage(),
        "",
        "--",
        "Inquiry " + inquiry.getId() + ", received " + inquiry.getCreatedAt(),
        "");
  }

  private static String orDash(Object value) {
    return Objects.toString(value, "-");
  }
}
