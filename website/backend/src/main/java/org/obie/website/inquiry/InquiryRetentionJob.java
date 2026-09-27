package org.obie.website.inquiry;

import java.time.Clock;
import java.time.Instant;
import java.time.Period;
import java.time.ZoneOffset;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

/** Deletes inquiries older than {@code obie.inquiry.retention.max-age} (the privacy promise). */
@Component
public class InquiryRetentionJob {

  private static final Logger LOG = LoggerFactory.getLogger(InquiryRetentionJob.class);

  private final InquiryRepository repository;
  private final Period maxAge;
  private final Clock clock;

  public InquiryRetentionJob(
      InquiryRepository repository, InquiryProperties properties, Clock clock) {
    this.repository = repository;
    this.maxAge = properties.retention().maxAge();
    this.clock = clock;
  }

  /** Runs on {@code obie.inquiry.retention.cron} (daily by default). */
  @Scheduled(cron = "${obie.inquiry.retention.cron}", zone = "UTC")
  public void deleteExpired() {
    Instant cutoff = clock.instant().atOffset(ZoneOffset.UTC).minus(maxAge).toInstant();
    int deleted = repository.deleteCreatedBefore(cutoff);
    if (deleted > 0) {
      LOG.info("Deleted {} inquiries received before {}", deleted, cutoff);
    }
  }
}
