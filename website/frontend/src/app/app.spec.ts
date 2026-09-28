import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';

import { App } from './app';
import { routes } from './app.routes';
import { LANDING_CONTENT_EN } from './content/landing.content';

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

  it('shows no cookie banner or other consent dialog on any page', async () => {
    const fixture = TestBed.createComponent(App);
    const router = TestBed.inject(Router);
    for (const url of ['/', '/impressum', '/privacy']) {
      await router.navigateByUrl(url);
      fixture.detectChanges();
      const root = fixture.nativeElement as HTMLElement;
      expect(root.querySelector('dialog, [role="dialog"], [role="alertdialog"]'), url).toBeNull();
      const outsideMain = Array.from(root.children).filter((child) => child.tagName !== 'MAIN');
      for (const element of outsideMain) {
        expect(element.textContent, url).not.toMatch(/cookie|consent/i);
      }
    }
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
