package org.obie.website.inquiry;

import jakarta.validation.constraints.AssertTrue;
import jakarta.validation.constraints.Email;
import jakarta.validation.constraints.Future;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Positive;
import jakarta.validation.constraints.Size;
import java.time.LocalDate;

/**
 * Body of {@code POST /api/inquiries}. The messages are shown next to the form fields.
 *
 * @param website honeypot: hidden from people, so anything in it comes from a bot
 * @param formToken signed form-render timestamp from {@code GET /api/inquiries/form-token}
 */
public record InquiryRequest(
    @NotNull(message = "Please choose what your inquiry is about.") InquiryType type,
    @NotBlank(message = "Please enter your name.")
        @Size(max = 200, message = "Please use at most 200 characters.")
        @Pattern(regexp = SINGLE_LINE, message = "Please use a single line.")
        String name,
    @NotBlank(message = "Please enter your e-mail address.")
        @Email(message = "Please enter a valid e-mail address.")
        @Size(max = 254, message = "Please use at most 254 characters.")
        String email,
    @Size(max = 200, message = "Please use at most 200 characters.")
        @Pattern(regexp = SINGLE_LINE, message = "Please use a single line.")
        String organisation,
    @Future(message = "Please choose a date in the future.") LocalDate eventDate,
    @Size(max = 200, message = "Please use at most 200 characters.")
        @Pattern(regexp = SINGLE_LINE, message = "Please use a single line.")
        String eventLocation,
    @Positive(message = "Please enter a positive number.")
        @Max(value = 1_000_000, message = "Please enter at most 1,000,000.")
        Integer audienceSize,
    @NotBlank(message = "Please enter a message.")
        @Size(min = 20, max = 5000, message = "Please write between 20 and 5000 characters.")
        String message,
    @NotNull(message = "Please accept the privacy notice.")
        @AssertTrue(message = "Please accept the privacy notice.")
        Boolean consent,
    String website,
    @NotBlank(message = "The form is out of date. Please reload the page.") String formToken) {

  /** No control characters (such as line breaks), which have no place in names or headers. */
  static final String SINGLE_LINE = "[^\\p{Cntrl}]*";
}
