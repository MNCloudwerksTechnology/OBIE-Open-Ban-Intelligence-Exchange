import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { InquiryType } from '../content/landing-content.model';

/** Base path of the inquiry API (website/README.md, "Inquiry API"). */
export const INQUIRY_API = '/api/inquiries';

/**
 * The back end discards inquiries sent less than 3 s after their form token
 * was issued as automated, without telling the client. Waiting this long after
 * receiving the token guarantees that a person's inquiry is never mistaken for
 * a bot's; the margin covers timer granularity.
 */
export const MIN_FILL_TIME_MS = 3_100;

/** An inquiry as the form collects it; optional fields are left out when empty. */
export interface Inquiry {
  readonly type: InquiryType;
  readonly name: string;
  readonly email: string;
  readonly organisation?: string;
  readonly eventDate?: string;
  readonly eventLocation?: string;
  readonly audienceSize?: number;
  readonly message: string;
  readonly consent: boolean;
  /** Honeypot: empty unless a bot filled it in. */
  readonly website: string;
}

/** A message the back end wants shown next to one form field. */
export interface FieldError {
  readonly field: string;
  readonly message: string;
}

/** Outcome of a submission; nothing is thrown. */
export type SubmitResult =
  | { readonly kind: 'accepted' }
  | { readonly kind: 'invalid'; readonly errors: readonly FieldError[] }
  /** The form token was too old; a fresh one is being fetched, so sending again works. */
  | { readonly kind: 'expired' }
  | { readonly kind: 'rate-limited' }
  | { readonly kind: 'failed' };

interface FormToken {
  readonly value: string;
  /** `Date.now()` when the token arrived. */
  readonly receivedAt: number;
}

/**
 * Client of the inquiry API. It fetches the signed form-render timestamp
 * (form token) when the form is shown and sends it with the inquiry.
 */
@Injectable({ providedIn: 'root' })
export class InquiryApi {
  private readonly http = inject(HttpClient);
  private token?: Promise<FormToken | undefined>;

  /** Fetches a form token unless one is ready; call when the form is shown. */
  prepare(): void {
    this.token ??= this.fetchToken();
  }

  /** Sends an inquiry, at the earliest the minimum fill time after the form token arrived. */
  async submit(inquiry: Inquiry): Promise<SubmitResult> {
    this.prepare();
    const token = await this.token;
    if (!token) {
      this.token = undefined; // fetch again on the next attempt
      return { kind: 'failed' };
    }
    await delay(token.receivedAt + MIN_FILL_TIME_MS - Date.now());
    try {
      await firstValueFrom(this.http.post(INQUIRY_API, { ...inquiry, formToken: token.value }));
      return { kind: 'accepted' };
    } catch (error) {
      return this.failure(error);
    }
  }

  private async fetchToken(): Promise<FormToken | undefined> {
    try {
      const { token } = await firstValueFrom(
        this.http.get<{ token: string }>(`${INQUIRY_API}/form-token`),
      );
      return { value: token, receivedAt: Date.now() };
    } catch {
      return undefined;
    }
  }

  private failure(error: unknown): SubmitResult {
    if (!(error instanceof HttpErrorResponse)) {
      return { kind: 'failed' };
    }
    if (error.status === 429) {
      return { kind: 'rate-limited' };
    }
    const errors = fieldErrors(error);
    if (error.status !== 400 || errors.length === 0) {
      return { kind: 'failed' };
    }
    if (errors.some((e) => e.field === 'formToken')) {
      this.token = this.fetchToken();
      return { kind: 'expired' };
    }
    return { kind: 'invalid', errors };
  }
}

/** The `errors` of a 400 problem detail, or none if the body has another shape. */
function fieldErrors(error: HttpErrorResponse): FieldError[] {
  const errors: unknown = (error.error as { errors?: unknown } | null)?.errors;
  if (!Array.isArray(errors)) {
    return [];
  }
  return errors.filter(
    (e): e is FieldError => typeof e?.field === 'string' && typeof e?.message === 'string',
  );
}

function delay(ms: number): Promise<void> {
  return ms > 0 ? new Promise((resolve) => setTimeout(resolve, ms)) : Promise.resolve();
}
