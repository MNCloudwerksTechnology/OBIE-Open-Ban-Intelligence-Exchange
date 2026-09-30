import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { CONSENT_CONTENT_EN } from '../content/consent.content';
import { CONSENT_STORAGE_KEY } from '../core/analytics/analytics.config';
import { ConsentService } from '../core/analytics/consent.service';
import { ConsentDialog } from './consent-dialog';

describe('ConsentDialog', () => {
  let consent: ConsentService;
  let host: HTMLElement;

  async function render(): Promise<void> {
    const fixture = TestBed.createComponent(ConsentDialog);
    host = fixture.nativeElement as HTMLElement;
    document.body.appendChild(host);
    consent = TestBed.inject(ConsentService);
    consent.start();
    await fixture.whenStable();
  }

  const dialog = () => host.querySelector<HTMLElement>('[role="dialog"]');
  const buttons = () => Array.from(host.querySelectorAll<HTMLButtonElement>('button'));

  beforeEach(() => {
    localStorage.removeItem(CONSENT_STORAGE_KEY);
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
  });

  afterEach(() => {
    host.remove();
    localStorage.removeItem(CONSENT_STORAGE_KEY);
  });

  it('asks in a labelled, non-modal dialog that says what is measured and by whom', async () => {
    await render();
    const element = dialog();
    expect(element).not.toBeNull();
    expect(element?.getAttribute('aria-modal')).toBeNull();
    expect(host.querySelector(`#${element?.getAttribute('aria-labelledby')}`)?.textContent).toBe(
      CONSENT_CONTENT_EN.heading,
    );
    const description = host.querySelector(`#${element?.getAttribute('aria-describedby')}`);
    expect(description?.textContent).toContain('Matomo');
    // Asked by the site, the dialog does not take focus from the page.
    expect(document.activeElement).not.toBe(element);
  });

  it('offers declining exactly like accepting, declining first', async () => {
    await render();
    expect(buttons().map((button) => button.textContent?.trim())).toEqual([
      CONSENT_CONTENT_EN.decline,
      CONSENT_CONTENT_EN.accept,
    ]);
    const [decline, accept] = buttons();
    expect(decline.className).toBe(accept.className);
  });

  it('links to the details in the privacy policy and to the question in German', async () => {
    await render();
    const links = Array.from(host.querySelectorAll('a'));
    expect(links.map((link) => [link.textContent?.trim(), link.getAttribute('href')])).toEqual([
      ['Deutsch', '/de'],
      [CONSENT_CONTENT_EN.privacyLink.label, '/privacy#analytics'],
    ]);
    expect(links[0].getAttribute('lang')).toBe('de');
  });

  it('closes and keeps the answer', async () => {
    await render();
    buttons()[1].click();
    TestBed.tick();
    expect(dialog()).toBeNull();
    expect(localStorage.getItem(CONSENT_STORAGE_KEY)).toBe('granted');
  });

  it('shows the answer in effect when the visitor reopens it, and takes focus', async () => {
    localStorage.setItem(CONSENT_STORAGE_KEY, 'denied');
    await render();
    expect(dialog()).toBeNull();

    consent.openSettings();
    TestBed.tick();

    expect(dialog()?.textContent).toContain(CONSENT_CONTENT_EN.current.denied);
    expect(document.activeElement).toBe(dialog());
  });
});
