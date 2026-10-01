package org.obie.website.inquiry;

import com.fasterxml.jackson.databind.JsonMappingException;
import com.fasterxml.jackson.databind.exc.MismatchedInputException;
import com.fasterxml.jackson.databind.exc.UnrecognizedPropertyException;
import java.util.List;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.HttpStatusCode;
import org.springframework.http.ProblemDetail;
import org.springframework.http.ResponseEntity;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;
import org.springframework.web.context.request.WebRequest;
import org.springframework.web.servlet.mvc.method.annotation.ResponseEntityExceptionHandler;

/**
 * Turns errors of the inquiry API into RFC 9457 problem details. Field problems are listed in
 * {@code errors} as {@code {field, message}} for the front end; nothing internal (exception
 * messages, stack traces, SQL) ever reaches the client.
 */
@RestControllerAdvice(basePackageClasses = InquiryController.class)
public class InquiryExceptionHandler extends ResponseEntityExceptionHandler {

  private static final Logger LOG = LoggerFactory.getLogger(InquiryExceptionHandler.class);

  static final String INVALID = "The inquiry is not valid. Please check the marked fields.";

  @ExceptionHandler
  ResponseEntity<ProblemDetail> invalidInquiry(InvalidInquiryException e) {
    return ResponseEntity.badRequest().body(invalid(e.errors()));
  }

  @ExceptionHandler
  ResponseEntity<ProblemDetail> unexpected(Exception e) {
    LOG.error("Inquiry request failed", e);
    ProblemDetail problem =
        ProblemDetail.forStatusAndDetail(
            HttpStatus.INTERNAL_SERVER_ERROR, "Something went wrong. Please try again later.");
    return ResponseEntity.internalServerError().body(problem);
  }

  /** Malformed JSON, unknown fields and values of the wrong type or format. */
  @Override
  protected ResponseEntity<Object> handleHttpMessageNotReadable(
      HttpMessageNotReadableException e,
      HttpHeaders headers,
      HttpStatusCode status,
      WebRequest request) {
    if (e.getCause() instanceof MismatchedInputException mismatch
        && !mismatch.getPath().isEmpty()) {
      return ResponseEntity.badRequest().body(invalid(List.of(fieldError(mismatch))));
    }
    ProblemDetail problem =
        ProblemDetail.forStatusAndDetail(
            HttpStatus.BAD_REQUEST, "The request body is not a valid JSON inquiry.");
    return ResponseEntity.badRequest().body(problem);
  }

  private static FieldError fieldError(MismatchedInputException e) {
    List<JsonMappingException.Reference> path = e.getPath();
    String field = path.get(path.size() - 1).getFieldName();
    if (e instanceof UnrecognizedPropertyException) {
      return new FieldError(field, "Unknown field.");
    }
    String message =
        switch (field) {
          case "type" -> "Please choose one of: " + InquiryType.JSON_VALUES + ".";
          case "eventDate" -> "Please enter a date as YYYY-MM-DD.";
          default -> "This value has the wrong format.";
        };
    return new FieldError(field, message);
  }

  private static ProblemDetail invalid(List<FieldError> errors) {
    ProblemDetail problem = ProblemDetail.forStatusAndDetail(HttpStatus.BAD_REQUEST, INVALID);
    problem.setProperty("errors", errors);
    return problem;
  }
}
