package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.Mockito.mock;

import org.junit.jupiter.api.Test;
import org.springframework.boot.autoconfigure.mail.MailProperties;
import org.springframework.mail.javamail.JavaMailSender;

class InquiryMailerTest {

  @Test
  void refusesToStartWithoutSmtpHost() {
    MailProperties noHost = new MailProperties();

    assertThatThrownBy(
            () -> new InquiryMailer(mock(JavaMailSender.class), noHost, TestProperties.defaults()))
        .isInstanceOf(IllegalStateException.class)
        .hasMessageContaining("OBIE_SMTP_HOST");
  }
}
