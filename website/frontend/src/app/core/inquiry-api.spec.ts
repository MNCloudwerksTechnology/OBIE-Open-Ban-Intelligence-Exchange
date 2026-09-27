import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { INQUIRY_API, Inquiry, InquiryApi, MIN_FILL_TIME_MS, SubmitResult } from './inquiry-api';

const INQUIRY: Inquiry = {
  type: 'talk',
  name: 'Ada Lovelace',
  email: 'ada@example.org',
  message: 'Would you give a talk about OBIE at our conference?',
  consent: true,
  website: '',
};

describe('InquiryApi', () => {
  let api: InquiryApi;
  let http: HttpTestingController;

  beforeEach(() => {
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    api = TestBed.inject(InquiryApi);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.useRealTimers();
  });

  /** Lets pending promises settle without moving the clock. */
  const settle = () => vi.advanceTimersByTimeAsync(0);

  function flushToken(token = 'token-1'): void {
    http.expectOne(`${INQUIRY_API}/form-token`).flush({ token });
  }

  /**
   * Starts a submission with a token that has just arrived and waits until it
   * is posted; returns the pending result (wrapped, so awaiting this does not
   * wait for the response).
   */
  async function submitWithFreshToken(): Promise<{ result: Promise<SubmitResult> }> {
    const result = api.submit(INQUIRY);
    flushToken();
    await settle();
    await vi.advanceTimersByTimeAsync(MIN_FILL_TIME_MS);
    return { result };
  }

  it('fetches one form token when the form is shown', () => {
    api.prepare();
    api.prepare();
    const request = http.expectOne(`${INQUIRY_API}/form-token`);
    expect(request.request.method).toBe('GET');
    request.flush({ token: 'token-1' });
  });

  it('sends the inquiry with the form token and reports acceptance', async () => {
    const { result } = await submitWithFreshToken();

    const post = http.expectOne(INQUIRY_API);
    expect(post.request.method).toBe('POST');
    expect(post.request.body).toEqual({ ...INQUIRY, formToken: 'token-1' });
    post.flush({ id: '4d1b7c1e-0000-4000-8000-000000000000' }, { status: 202, statusText: 'OK' });
    expect(await result).toEqual({ kind: 'accepted' });
  });

  it('never sends before the minimum fill time has passed since the token arrived', async () => {
    const result = api.submit(INQUIRY);
    flushToken();
    await settle();

    await vi.advanceTimersByTimeAsync(MIN_FILL_TIME_MS - 1);
    http.expectNone(INQUIRY_API);
    await vi.advanceTimersByTimeAsync(1);
    http.expectOne(INQUIRY_API).flush({ id: 'x' }, { status: 202, statusText: 'OK' });
    expect(await result).toEqual({ kind: 'accepted' });
  });

  it('sends at once when the form has been open long enough', async () => {
    api.prepare();
    flushToken();
    await vi.advanceTimersByTimeAsync(60_000);

    const result = api.submit(INQUIRY);
    await settle();
    http.expectOne(INQUIRY_API).flush({ id: 'x' }, { status: 202, statusText: 'OK' });
    expect(await result).toEqual({ kind: 'accepted' });
  });

  it('passes field errors of a 400 on', async () => {
    const { result } = await submitWithFreshToken();

    const errors = [{ field: 'email', message: 'Please enter a valid e-mail address.' }];
    http
      .expectOne(INQUIRY_API)
      .flush({ status: 400, errors }, { status: 400, statusText: 'Bad Request' });
    expect(await result).toEqual({ kind: 'invalid', errors });
  });

  it('fetches a fresh token when the old one expired', async () => {
    const { result } = await submitWithFreshToken();

    http
      .expectOne(INQUIRY_API)
      .flush(
        { errors: [{ field: 'formToken', message: 'The form is out of date.' }] },
        { status: 400, statusText: 'Bad Request' },
      );
    expect(await result).toEqual({ kind: 'expired' });
    flushToken('token-2');

    await settle();
    const retry = api.submit(INQUIRY);
    await vi.advanceTimersByTimeAsync(MIN_FILL_TIME_MS);
    const post = http.expectOne(INQUIRY_API);
    expect(post.request.body.formToken).toBe('token-2');
    post.flush({ id: 'x' }, { status: 202, statusText: 'OK' });
    expect(await retry).toEqual({ kind: 'accepted' });
  });

  it('reports rate limiting', async () => {
    const { result } = await submitWithFreshToken();

    http
      .expectOne(INQUIRY_API)
      .flush({ detail: 'Too many' }, { status: 429, statusText: 'Too Many Requests' });
    expect(await result).toEqual({ kind: 'rate-limited' });
  });

  it.each([
    [500, { detail: 'Something went wrong.' }],
    [413, null],
    [400, { detail: 'The request body is not a valid JSON inquiry.' }],
  ])('reports a %s without field errors as a failure', async (status, body) => {
    const { result } = await submitWithFreshToken();

    http.expectOne(INQUIRY_API).flush(body, { status, statusText: 'Error' });
    expect(await result).toEqual({ kind: 'failed' });
  });

  it('reports a network error as a failure', async () => {
    const { result } = await submitWithFreshToken();

    http.expectOne(INQUIRY_API).error(new ProgressEvent('error'));
    expect(await result).toEqual({ kind: 'failed' });
  });

  it('fails without a form token and fetches one again on the next attempt', async () => {
    const result = api.submit(INQUIRY);
    http
      .expectOne(`${INQUIRY_API}/form-token`)
      .flush(null, { status: 503, statusText: 'Unavailable' });
    expect(await result).toEqual({ kind: 'failed' });

    const retry = api.submit(INQUIRY);
    flushToken();
    await settle();
    await vi.advanceTimersByTimeAsync(MIN_FILL_TIME_MS);
    http.expectOne(INQUIRY_API).flush({ id: 'x' }, { status: 202, statusText: 'OK' });
    expect(await retry).toEqual({ kind: 'accepted' });
  });
});
