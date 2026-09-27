package org.obie.website.inquiry;

import com.fasterxml.jackson.annotation.JsonValue;
import java.util.Arrays;
import java.util.Locale;
import java.util.stream.Collectors;

/** What the visitor asks for; written in lower case in JSON ({@code "talk"}). */
public enum InquiryType {
  TALK,
  WORKSHOP,
  INTERVIEW,
  COLLABORATION,
  OTHER;

  /** The JSON values, comma-separated, for error messages. */
  static final String JSON_VALUES =
      Arrays.stream(values()).map(InquiryType::jsonValue).collect(Collectors.joining(", "));

  @JsonValue
  public String jsonValue() {
    return name().toLowerCase(Locale.ROOT);
  }
}
