import { AbstractControl, ValidationErrors, ValidatorFn } from '@angular/forms';

// Validators mirroring the back end's rules (InquiryRequest.java), so a
// visitor sees mistakes before sending. The back end strips surrounding
// whitespace before it checks, and so do these. Error keys map to the
// messages in `ValidationMessages`.

/** Largest audience the back end accepts. */
export const MAX_AUDIENCE = 1_000_000;

export const MESSAGE_MIN_LENGTH = 20;
export const MESSAGE_MAX_LENGTH = 5000;

function text(control: AbstractControl): string {
  return typeof control.value === 'string' ? control.value.trim() : '';
}

/** Java's `\p{Cntrl}`: the ASCII control characters. */
function isControlCharacter(char: string): boolean {
  const code = char.charCodeAt(0);
  return code < 0x20 || code === 0x7f;
}

/** Not blank. */
export const requiredText: ValidatorFn = (control) =>
  text(control) === '' ? { required: true } : null;

/** At most `max` characters, not counting surrounding whitespace. */
export function maxTextLength(max: number): ValidatorFn {
  return (control) => (text(control).length > max ? { maxLength: { max } } : null);
}

/** No control characters at all, such as line breaks. */
export const singleLine: ValidatorFn = (control) =>
  [...text(control)].some(isControlCharacter) ? { singleLine: true } : null;

/** Line breaks and tabs are fine, other control characters (such as NUL) are not. */
export const noControlCharacters: ValidatorFn = (control) =>
  [...text(control)].some((char) => isControlCharacter(char) && !'\t\n\r'.includes(char))
    ? { controlCharacters: true }
    : null;

/** Between 20 and 5000 characters, when not blank (blank is `requiredText`'s job). */
export const messageLength: ValidatorFn = (control) => {
  const length = text(control).length;
  return length > 0 && (length < MESSAGE_MIN_LENGTH || length > MESSAGE_MAX_LENGTH)
    ? { messageLength: true }
    : null;
};

/** Today in the visitor's time zone as `YYYY-MM-DD`. */
export function today(now = new Date()): string {
  const month = String(now.getMonth() + 1).padStart(2, '0');
  const day = String(now.getDate()).padStart(2, '0');
  return `${now.getFullYear()}-${month}-${day}`;
}

/** Empty, or a `YYYY-MM-DD` date after today. */
export const futureDate: ValidatorFn = (control): ValidationErrors | null => {
  const value = text(control);
  if (value === '') {
    return null;
  }
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || Number.isNaN(Date.parse(value))) {
    return { dateFormat: true };
  }
  return value <= today() ? { dateInFuture: true } : null;
};

/** Empty, or a whole number from 1 to 1,000,000 (as typed, so a string). */
export const audienceSize: ValidatorFn = (control): ValidationErrors | null => {
  const value = text(control);
  if (value === '') {
    return null;
  }
  if (!/^[+-]?\d+$/.test(value)) {
    return { wholeNumber: true };
  }
  const number = Number(value);
  if (number < 1) {
    return { positiveNumber: true };
  }
  return number > MAX_AUDIENCE ? { maxAudience: true } : null;
};
