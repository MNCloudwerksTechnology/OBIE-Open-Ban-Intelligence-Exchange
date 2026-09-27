package org.obie.website.inquiry;

import java.time.Instant;
import java.util.List;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Modifying;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.transaction.annotation.Transactional;

/** Stored inquiries. */
public interface InquiryRepository extends JpaRepository<Inquiry, UUID> {

  /** Inquiries whose mails are due, oldest first, at most 20 per call. */
  List<Inquiry> findTop20ByNextMailAttemptAtLessThanEqualOrderByNextMailAttemptAtAsc(Instant now);

  /** Deletes every inquiry created before {@code cutoff}; returns how many. */
  @Transactional
  @Modifying
  @Query("delete from Inquiry i where i.createdAt < :cutoff")
  int deleteCreatedBefore(@Param("cutoff") Instant cutoff);
}
