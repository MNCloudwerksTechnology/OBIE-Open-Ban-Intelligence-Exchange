import { ComponentFixture, TestBed } from '@angular/core/testing';

import { Inquiry, InquiryApi, SubmitResult } from '../core/inquiry-api';
import { LANDING_CONTENT_EN } from '../content/landing.content';
import { Contact } from './contact';

const copy = LANDING_CONTENT_EN.contact.form;
const MESSAGE = 'Would you give a talk about OBIE at our conference?';

/** A promise the test resolves when it wants the back end to answer. */
function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

function nextYear(): string {
  return `${new Date().getFullYear() + 1}-06-15`;
}

describe('Contact', () => {
  let fixture: ComponentFixture<Contact>;
  let host: HTMLElement;
  let api: { prepare: ReturnType<typeof vi.fn>; submit: ReturnType<typeof vi.fn> };

  beforeEach(async () => {
    api = {
      prepare: vi.fn(),
      submit: vi.fn(async (): Promise<SubmitResult> => ({ kind: 'accepted' })),
    };
    TestBed.configureTestingModule({ providers: [{ provide: InquiryApi, useValue: api }] });
    fixture = TestBed.createComponent(Contact);
    host = fixture.nativeElement as HTMLElement;
    await fixture.whenStable();
  });

  const field = (name: string) =>
    host.querySelector<HTMLInputElement & HTMLTextAreaElement>(`#inquiry-${name}`);
  const errorOf = (name: string) => host.querySelector(`#inquiry-${name}-error`)?.textContent;
  const status = () => host.querySelector('[role="status"]')?.textContent?.trim();
  const submitButton = () => host.querySelector<HTMLButtonElement>('button[type="submit"]');

  async function type(name: string, value: string): Promise<void> {
    const input = field(name) as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    input.dispatchEvent(new Event('blur'));
    await fixture.whenStable();
  }

  async function chooseType(label: string): Promise<void> {
    const option = Array.from(host.querySelectorAll('.choice')).find(
      (choice) => choice.textContent?.trim() === label,
    );
    option?.querySelector('input')?.click();
    await fixture.whenStable();
  }

  async function checkConsent(): Promise<void> {
    field('consent')?.click();
    await fixture.whenStable();
  }

  async function fillValidForm(): Promise<void> {
    await type('name', '  Ada Lovelace ');
    await type('email', 'ada@example.org');
    await type('message', MESSAGE);
    await checkConsent();
  }

  async function send(): Promise<void> {
    submitButton()?.click();
    await fixture.whenStable();
  }

  it('fetches the form token once the form is rendered', () => {
    expect(api.prepare).toHaveBeenCalledTimes(1);
  });

  describe('inquiry type', () => {
    it('offers the five inquiry types as a labelled radio group, "Talk" preselected', () => {
      const group = host.querySelector('fieldset.types');
      expect(group?.querySelector('legend')?.textContent).toBe(copy.typeLegend);
      const radios = Array.from(group?.querySelectorAll<HTMLInputElement>('input') ?? []);
      expect(radios.map((radio) => radio.type)).toEqual(Array(5).fill('radio'));
      expect(radios.map((radio) => radio.closest('label')?.textContent?.trim())).toEqual([
        'Talk',
        'Workshop',
        'Interview or press',
        'Collaboration or research',
        'Other',
      ]);
      expect(radios[0].checked).toBe(true);
    });

    it.each(['Talk', 'Workshop', 'Interview or press'])(
      'asks for date, location and audience of a %s',
      async (label) => {
        await chooseType(label);
        expect(host.querySelector('fieldset.event legend')?.textContent).toBe(copy.eventLegend);
        for (const name of ['eventDate', 'eventLocation', 'audienceSize']) {
          expect(field(name), name).not.toBeNull();
        }
      },
    );

    it.each(['Collaboration or research', 'Other'])(
      'hides the event fields for %s',
      async (label) => {
        await chooseType(label);
        expect(host.querySelector('fieldset.event')).toBeNull();
        expect(field('eventDate')).toBeNull();
      },
    );

    it('does not send event details once another type is chosen, but keeps them', async () => {
      api.submit.mockResolvedValue({ kind: 'failed' });
      await fillValidForm();
      await type('eventLocation', 'Berlin');
      await chooseType('Other');
      await send();
      expect(api.submit).toHaveBeenCalledWith(
        expect.not.objectContaining({ eventLocation: expect.anything() }),
      );

      await chooseType('Workshop');
      expect(field('eventLocation')?.value).toBe('Berlin');
    });
  });

  describe('validation', () => {
    it('labels every visible field', () => {
      for (const control of Array.from(host.querySelectorAll('input, textarea'))) {
        if (control.closest('[aria-hidden="true"]') || control.getAttribute('type') === 'radio') {
          continue;
        }
        const label = host.querySelector(`label[for="${control.id}"]`);
        expect(label?.textContent?.trim(), control.id).toBeTruthy();
      }
    });

    it('shows a mistake next to the field once the visitor leaves it', async () => {
      expect(errorOf('name')).toBeUndefined();
      await type('name', '   ');

      expect(errorOf('name')).toBe('Please enter your name.');
      expect(field('name')?.getAttribute('aria-invalid')).toBe('true');
      expect(field('name')?.getAttribute('aria-describedby')).toBe('inquiry-name-error');
    });

    it.each([
      ['email', 'ada@', 'Please enter a valid e-mail address.'],
      ['name', 'x'.repeat(201), 'Please use at most 200 characters.'],
      ['organisation', 'x'.repeat(201), 'Please use at most 200 characters.'],
      ['message', 'Too short.', 'Please write between 20 and 5000 characters.'],
      ['message', `${'x'.repeat(20)}\u0000`, 'Please remove special control characters.'],
      ['eventDate', '2020-01-01', 'Please choose a date in the future.'],
      ['eventLocation', 'x'.repeat(201), 'Please use at most 200 characters.'],
      ['audienceSize', '0', 'Please enter a positive number.'],
      ['audienceSize', '1.5', 'Please enter a whole number.'],
      ['audienceSize', '1000001', 'Please enter at most 1,000,000.'],
    ])('rejects %s "%s" like the back end', async (name, value, message) => {
      await type(name, value);
      expect(errorOf(name)).toBe(message);
    });

    it.each([
      ['eventDate', nextYear()],
      ['audienceSize', '1000000'],
      ['message', `${MESSAGE}\nThanks!`],
    ])('accepts %s "%s"', async (name, value) => {
      await type(name, value);
      expect(errorOf(name)).toBeUndefined();
    });

    it('keeps the hint of a field in its description', async () => {
      expect(field('email')?.getAttribute('aria-describedby')).toBe('inquiry-email-hint');
      await type('email', 'nope');
      expect(field('email')?.getAttribute('aria-describedby')).toBe(
        'inquiry-email-hint inquiry-email-error',
      );
    });

    it('does not send an invalid form; shows, announces and focuses the mistakes', async () => {
      await send();

      expect(api.submit).not.toHaveBeenCalled();
      expect(errorOf('name')).toBe('Please enter your name.');
      expect(errorOf('email')).toBe('Please enter your e-mail address.');
      expect(errorOf('message')).toBe('Please enter a message.');
      expect(errorOf('consent')).toBe('Please accept the privacy notice.');
      expect(status()).toBe(copy.invalid);
      expect(document.activeElement).toBe(field('name'));
    });

    it('links the consent to the privacy page', () => {
      const link = host.querySelector<HTMLAnchorElement>('label[for="inquiry-consent"] a');
      expect(link?.getAttribute('href')).toBe('/privacy');
      expect(link?.textContent).toBe('privacy notice');
    });
  });

  describe('sending', () => {
    it('sends the trimmed inquiry without empty optional fields', async () => {
      await fillValidForm();
      await type('eventDate', nextYear());
      await type('audienceSize', '120');
      await send();

      const expected: Inquiry = {
        type: 'talk',
        name: 'Ada Lovelace',
        email: 'ada@example.org',
        eventDate: nextYear(),
        audienceSize: 120,
        message: MESSAGE,
        consent: true,
        website: '',
      };
      expect(api.submit).toHaveBeenCalledWith(expected);
    });

    it('sends the honeypot as filled in, hidden from people and keyboards', async () => {
      const honeypot = field('website') as HTMLInputElement;
      expect(honeypot.closest('[aria-hidden="true"]')).not.toBeNull();
      expect(honeypot.tabIndex).toBe(-1);
      await fillValidForm();
      await type('website', 'https://spam.example');
      await send();
      expect(api.submit).toHaveBeenCalledWith(
        expect.objectContaining({ website: 'https://spam.example' }),
      );
    });

    it('disables the submit button while sending and sends only once', async () => {
      const answer = deferred<SubmitResult>();
      api.submit.mockReturnValue(answer.promise);
      await fillValidForm();
      await send();

      expect(submitButton()?.disabled).toBe(true);
      expect(submitButton()?.textContent?.trim()).toBe(copy.sending);
      expect(host.querySelector('form')?.getAttribute('aria-busy')).toBe('true');
      host.querySelector('form')?.dispatchEvent(new Event('submit'));
      expect(api.submit).toHaveBeenCalledTimes(1);

      answer.resolve({ kind: 'accepted' });
      await fixture.whenStable();
    });

    it('thanks the visitor, announces it and moves focus to the confirmation', async () => {
      await fillValidForm();
      await send();

      expect(host.querySelector('form')).toBeNull();
      const confirmation = host.querySelector('#inquiry-success');
      expect(confirmation?.parentElement?.textContent).toContain(
        'Thanks, Markus will get back to you within a few days.',
      );
      expect(status()).toBe(copy.success.text);
      expect(document.activeElement).toBe(confirmation);
    });

    it('shows the back end’s field errors next to the fields', async () => {
      api.submit.mockResolvedValue({
        kind: 'invalid',
        errors: [
          { field: 'email', message: 'Please enter a valid e-mail address.' },
          { field: 'eventDate', message: 'Please choose a date in the future.' },
        ],
      });
      await fillValidForm();
      await send();

      expect(errorOf('email')).toBe('Please enter a valid e-mail address.');
      expect(errorOf('eventDate')).toBe('Please choose a date in the future.');
      expect(status()).toBe(copy.invalid);
      expect(document.activeElement).toBe(field('email'));

      await type('email', 'ada@example.com');
      expect(errorOf('email')).toBeUndefined();
    });

    it('treats errors for fields the form does not have as a failure', async () => {
      api.submit.mockResolvedValue({
        kind: 'invalid',
        errors: [{ field: 'website', message: 'Unknown field.' }],
      });
      await fillValidForm();
      await send();

      expect(host.querySelector('[role="alert"]')?.textContent).toContain(copy.error.text);
    });
  });

  describe('errors', () => {
    it.each([
      ['failed', copy.error.text],
      ['expired', copy.error.expired],
      ['rate-limited', copy.error.rateLimited],
    ] as const)('explains a %s submission and keeps the entries', async (kind, text) => {
      api.submit.mockResolvedValue({ kind });
      await fillValidForm();
      await send();

      const alert = host.querySelector('[role="alert"]');
      expect(alert?.querySelector('h3')?.textContent).toBe(copy.error.heading);
      expect(alert?.textContent).toContain(text);
      expect(field('name')?.value).toBe('  Ada Lovelace ');
      expect(field('message')?.value).toBe(MESSAGE);
      expect(field('consent')?.checked).toBe(true);
      expect(submitButton()?.disabled).toBe(false);
    });

    it('lets the visitor try again with the same entries', async () => {
      api.submit.mockResolvedValueOnce({ kind: 'failed' });
      await fillValidForm();
      await send();
      expect(submitButton()?.textContent?.trim()).toBe(copy.error.retry);

      await send();

      expect(api.submit).toHaveBeenCalledTimes(2);
      expect(api.submit.mock.calls[1][0]).toEqual(api.submit.mock.calls[0][0]);
      expect(host.querySelector('#inquiry-success')).not.toBeNull();
    });
  });
});
