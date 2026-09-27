package org.obie.website.inquiry;

import jakarta.validation.ConstraintViolation;
import java.util.Comparator;
import java.util.List;
import java.util.Set;

/** A submitted inquiry breaks one or more validation rules. */
public class InvalidInquiryException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  /** Field errors sorted by field, so responses are stable. */
  private final FieldError[] errors;

  InvalidInquiryException(Set<? extends ConstraintViolation<?>> violations) {
    super("Invalid inquiry");
    this.errors =
        violations.stream()
            .map(v -> new FieldError(v.getPropertyPath().toString(), v.getMessage()))
            .sorted(Comparator.comparing(FieldError::field).thenComparing(FieldError::message))
            .toArray(FieldError[]::new);
  }

  public List<FieldError> errors() {
    return List.of(errors);
  }
}
