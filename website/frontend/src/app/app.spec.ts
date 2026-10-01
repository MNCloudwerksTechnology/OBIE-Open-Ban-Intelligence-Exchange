import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';

import { renderPrerendered } from '../testing/prerender';
import { App } from './app';
import { routes } from './app.routes';
import { CONSENT_CONTENT_EN } from './content/consent.content';
import { LANDING_CONTENT_EN } from './content/landing.content';
import { CONSENT_STORAGE_KEY } from './core/analytics/analytics.config';

describe('App routing', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [App], providers: [provideRouter(routes)] });
  });

  it('renders the home page at /', async () => {
    const harness = await RouterTestingHarness.create('/');
    expect(harness.routeNativeElement?.querySelector('h1')?.textContent).toBe(
      'A neighbourhood watch for servers.',
    );
  });

  it('renders the not-found page for unknown URLs', async () => {
    const harness = await RouterTestingHarness.create('/does/not/exist');
    expect(harness.routeNativeElement?.querySelector('h1')?.textContent).toBe('Page not found');
  });

  it('renders the not-found page at its own prerendered path', async () => {
    const harness = await RouterTestingHarness.create('/404');
    expect(harness.routeNativeElement?.querySelector('h1')?.textContent).toBe('Page not found');
  });

  it('serves every footer link within this site as a page of its own', async () => {
    const internal = LANDING_CONTENT_EN.footer.links.filter((link) => link.href.startsWith('/'));
    expect(internal.map((link) => link.href)).toEqual(['/impressum', '/privacy']);
    const harness = await RouterTestingHarness.create();
    for (const link of internal) {
      await harness.navigateByUrl(link.href);
      expect(harness.routeNativeElement?.querySelector('h1')?.textContent, link.href).not.toBe(
        'Page not found',
      );
    }
  });

  it('links the inquiry consent to the privacy page', async () => {
    const harness = await RouterTestingHarness.create(
      LANDING_CONTENT_EN.contact.form.consent.link.href,
    );
    expect(harness.routeNativeElement?.querySelector('h1 [lang="de"]')?.textContent).toBe(
      'Datenschutzerklärung',
    );
  });

  it('asks about visitor statistics once in the browser, before the header and outside main', async () => {
    localStorage.removeItem(CONSENT_STORAGE_KEY);
    const fixture = TestBed.createComponent(App);
    await TestBed.inject(Router).navigateByUrl('/');
    fixture.detectChanges();
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const dialogs = root.querySelectorAll('[role="dialog"]');
    expect(dialogs.length).toBe(1);
    expect(dialogs[0].closest('main')).toBeNull();
    expect(dialogs[0].textContent).toContain(CONSENT_CONTENT_EN.heading);
    expect(root.querySelector('app-consent-dialog')?.nextElementSibling?.tagName).toBe(
      'APP-SITE-HEADER',
    );
  });

  it('leaves the question and the privacy settings out of the prerendered page', async () => {
    TestBed.configureTestingModule({ providers: [provideRouter(routes)] });
    const page = await renderPrerendered(App);
    expect(page.querySelector('[role="dialog"]')).toBeNull();
    expect(page.textContent).not.toContain(CONSENT_CONTENT_EN.heading);
    expect(page.textContent).not.toContain(CONSENT_CONTENT_EN.settings);
  });

  it('wraps pages in banner, main and content-info landmarks', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();
    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('header')).not.toBeNull();
    expect(root.querySelector('main')).not.toBeNull();
    expect(root.querySelector('footer')).not.toBeNull();
  });

  it('starts with a skip link that moves focus to the main landmark', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();
    const root = fixture.nativeElement as HTMLElement;
    document.body.appendChild(root);

    const skipLink = root.querySelector('a') as HTMLAnchorElement;
    expect(skipLink.classList).toContain('skip-link');
    expect(skipLink.getAttribute('href')).toBe('#main');

    skipLink.click();
    expect(document.activeElement).toBe(root.querySelector('main#main'));
    root.remove();
  });
});
