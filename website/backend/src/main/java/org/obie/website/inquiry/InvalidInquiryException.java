package org.obie.website.inquiry;

import java.util.Comparator;
import java.util.List;

/** A submitted inquiry breaks one or more validation rules. */
public class InvalidInquiryException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  /** Field errors sorted by field, so responses are stable. */
  private final FieldError[] errors;

  InvalidInquiryException(List<FieldError> errors) {
    super("Invalid inquiry");
    this.errors =
        errors.stream()
            .sorted(Comparator.comparing(FieldError::field).thenComparing(FieldError::message))
            .toArray(FieldError[]::new);
  }

  public List<FieldError> errors() {
    return List.of(errors);
  }
}
