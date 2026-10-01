import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { TranslocoService } from '@jsverse/transloco';

import { provideLang, whenI18nReady } from '../../testing/i18n';
import { App } from '../app';
import { routes } from '../app.routes';
import { CONSENT_CONTENT_DE } from '../content/consent.content.de';
import { LANDING_CONTENT_DE } from '../content/landing.content.de';
import { LEGAL_CONTENT_DE } from '../content/legal.content.de';
import { SEO_CONTENT_DE } from '../content/seo.content.de';
import { CONSENT_STORAGE_KEY } from '../core/analytics/analytics.config';
import { SITE_ORIGIN } from '../core/seo';

const ORIGIN = 'https://obie.example';

/** Text with runs of whitespace collapsed, as a reader sees it. */
function visibleText(element: Element | null | undefined): string {
  return (element?.textContent ?? '').replace(/\s+/g, ' ').trim();
}

describe('German pages', () => {
  let root: HTMLElement;

  async function open(url: string): Promise<void> {
    const fixture = TestBed.createComponent(App);
    await TestBed.inject(Router).navigateByUrl(url);
    fixture.detectChanges();
    await fixture.whenStable();
    root = fixture.nativeElement as HTMLElement;
  }

  const alternates = () =>
    Object.fromEntries(
      Array.from(document.head.querySelectorAll('link[rel="alternate"][hreflang]')).map((link) => [
        link.getAttribute('hreflang'),
        link.getAttribute('href'),
      ]),
    );

  beforeEach(async () => {
    localStorage.removeItem(CONSENT_STORAGE_KEY);
    TestBed.configureTestingModule({
      imports: [App],
      providers: [
        provideRouter(routes),
        provideLang('de'),
        { provide: SITE_ORIGIN, useValue: ORIGIN },
      ],
    });
    await whenI18nReady();
  });

  afterEach(() => {
    document.documentElement.lang = 'en';
  });

  it('load the German translation before anything renders and say so in <html lang>', () => {
    expect(TestBed.inject(TranslocoService).getActiveLang()).toBe('de');
    expect(document.documentElement.lang).toBe('de');
  });

  it('show the home page in German at /de, its sections linked under /de', async () => {
    await open('/de');
    expect(root.querySelector('main h1')?.textContent).toBe(LANDING_CONTENT_DE.hero.heading);
    const nav = Array.from(root.querySelectorAll('app-site-header nav a'));
    expect(nav.map((link) => link.getAttribute('href'))).toEqual(
      ['problem', 'how-it-works', 'principles', 'status', 'get-started', 'faq'].map(
        (id) => `/de#${id}`,
      ),
    );
    expect(root.querySelector('app-site-header .brand')?.getAttribute('href')).toBe('/de#top');
    expect(root.querySelector('.skip-link')?.textContent).toBe(LANDING_CONTENT_DE.a11y.skipLink);
    expect(document.title).toBe(LANDING_CONTENT_DE.meta.title);
  });

  it('switch back to the same page in English', async () => {
    await open('/de/datenschutz');
    const link = root.querySelector('app-site-header app-language-switch a');
    expect(link?.getAttribute('href')).toBe('/privacy');
    expect(link?.getAttribute('lang')).toBe('en');
    expect(link?.textContent?.trim()).toBe('EN');
  });

  it('show the German legal pages without English headings', async () => {
    await open('/de/impressum');
    expect(visibleText(root.querySelector('main h1'))).toBe('Impressum');
    await open('/de/datenschutz');
    expect(visibleText(root.querySelector('main h1'))).toBe('Datenschutzerklärung');
    expect(root.querySelector('main [lang="de"]')).toBeNull();
    expect(document.title).toBe(LEGAL_CONTENT_DE.privacy.meta.title);
  });

  it('give every page a unique title and description of search-result length', async () => {
    const heads: { title: string; description: string }[] = [];
    for (const url of ['/de', '/de/impressum', '/de/datenschutz', '/de/404']) {
      await open(url);
      const description = document.head.querySelector('meta[name="description"]');
      heads.push({
        title: document.title,
        description: description?.getAttribute('content') ?? '',
      });
    }
    expect(new Set(heads.map((head) => head.title)).size).toBe(heads.length);
    expect(new Set(heads.map((head) => head.description)).size).toBe(heads.length);
    for (const { title, description } of heads) {
      expect(title.length, title).toBeLessThanOrEqual(60);
      expect(description.length, description).toBeGreaterThanOrEqual(70);
      expect(description.length, description).toBeLessThanOrEqual(160);
    }
  });

  it('link each page to its English counterpart for search engines', async () => {
    await open('/de/datenschutz');
    expect(document.head.querySelector('link[rel="canonical"]')?.getAttribute('href')).toBe(
      `${ORIGIN}/de/datenschutz`,
    );
    expect(alternates()).toEqual({
      en: `${ORIGIN}/privacy`,
      de: `${ORIGIN}/de/datenschutz`,
      'x-default': `${ORIGIN}/privacy`,
    });
    const locale = (property: string) =>
      Array.from(document.head.querySelectorAll(`meta[property="${property}"]`)).map((meta) =>
        meta.getAttribute('content'),
      );
    expect(locale('og:locale')).toEqual(['de_DE']);
    expect(locale('og:locale:alternate')).toEqual(['en_GB']);
  });

  it('show unknown pages under /de as the German not-found page, never indexed', async () => {
    await open('/de/gibt/es/nicht');
    expect(root.querySelector('main h1')?.textContent).toBe(SEO_CONTENT_DE.notFound.heading);
    expect(root.querySelector('main a')?.getAttribute('href')).toBe('/de');
    expect(alternates()).toEqual({});
    expect(document.head.querySelector('meta[name="robots"]')?.getAttribute('content')).toBe(
      'noindex',
    );
  });

  it('ask about visitor statistics in German, offering the question in English', async () => {
    await open('/de');
    const dialog = root.querySelector('[role="dialog"]');
    expect(dialog?.textContent).toContain(CONSENT_CONTENT_DE.heading);
    expect(
      Array.from(dialog?.querySelectorAll('button') ?? []).map((b) => b.textContent?.trim()),
    ).toEqual([CONSENT_CONTENT_DE.decline, CONSENT_CONTENT_DE.accept]);
    const english = dialog?.querySelector('app-language-switch a');
    expect(english?.textContent?.trim()).toBe('English');
    expect(english?.getAttribute('href')).toBe('/');
    expect(visibleText(root.querySelector('footer nav button'))).toBe(CONSENT_CONTENT_DE.settings);
  });
});
