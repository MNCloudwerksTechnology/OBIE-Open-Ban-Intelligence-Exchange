package org.obie.website.inquiry;

import java.io.Serializable;

/**
 * A message the front end shows next to one form field.
 *
 * @param field JSON name of the field
 * @param message human-readable explanation
 */
public record FieldError(String field, String message) implements Serializable {}
