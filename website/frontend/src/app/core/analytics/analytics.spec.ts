import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';

import { NOT_FOUND_DATA } from '../../pages/not-found/not-found';
import { ANALYTICS_ORIGIN, CONSENT_STORAGE_KEY, MATOMO_SITE_ID } from './analytics.config';
import { Analytics, MatomoCommand, MatomoWindow } from './analytics';
import { ConsentService } from './consent.service';

@Component({ template: '' })
class Page {}

const matomo = window as MatomoWindow;

/** The commands queued for Matomo so far. */
function queued(): MatomoCommand[] {
  return matomo._paq ?? [];
}

function names(): string[] {
  return queued().map(([name]) => name);
}

function scripts(): HTMLScriptElement[] {
  return Array.from(document.querySelectorAll<HTMLScriptElement>('script[src]')).filter((script) =>
    script.src.startsWith(ANALYTICS_ORIGIN),
  );
}

describe('Analytics (Matomo)', () => {
  let consent: ConsentService;
  let analytics: Analytics;
  let router: Router;

  async function visit(url: string): Promise<void> {
    await router.navigateByUrl(url);
    TestBed.tick();
  }

  beforeEach(async () => {
    localStorage.removeItem(CONSENT_STORAGE_KEY);
    delete matomo._paq;
    TestBed.configureTestingModule({
      providers: [
        provideRouter([
          { path: '', component: Page, title: 'Home' },
          { path: 'impressum', component: Page, title: 'Impressum' },
          { path: '**', component: Page, data: NOT_FOUND_DATA },
        ]),
      ],
    });
    consent = TestBed.inject(ConsentService);
    analytics = TestBed.inject(Analytics);
    router = TestBed.inject(Router);
    consent.start();
    analytics.start();
    await visit('/');
  });

  afterEach(() => {
    scripts().forEach((script) => script.remove());
    delete matomo._paq;
    localStorage.removeItem(CONSENT_STORAGE_KEY);
  });

  it('loads nothing and sends nothing before the visitor answers', async () => {
    await visit('/impressum');
    analytics.track({ category: 'Demo', action: 'started' });
    expect(queued()).toEqual([]);
    expect(scripts()).toEqual([]);
  });

  it('loads nothing and sends nothing after the visitor declines', async () => {
    consent.deny();
    TestBed.tick();
    await visit('/impressum');
    expect(queued()).toEqual([]);
    expect(scripts()).toEqual([]);
  });

  it('once the visitor accepts, loads Matomo without cookies and counts the current page', () => {
    consent.grant();
    TestBed.tick();

    expect(names()).toEqual([
      'requireConsent',
      'setConsentGiven',
      'disableCookies',
      'setTrackerUrl',
      'setSiteId',
      'enableLinkTracking',
      'enableHeartBeatTimer',
      'setCustomUrl',
      'setDocumentTitle',
      'trackPageView',
    ]);
    expect(queued()).toContainEqual(['setTrackerUrl', `${ANALYTICS_ORIGIN}/matomo.php`]);
    expect(queued()).toContainEqual(['setSiteId', MATOMO_SITE_ID]);
    expect(queued()).toContainEqual(['setCustomUrl', `${location.origin}/`]);
    const [script] = scripts();
    expect(scripts().length).toBe(1);
    expect(script.src).toBe(`${ANALYTICS_ORIGIN}/matomo.js`);
    expect(script.async).toBe(true);
  });

  it('counts every further page once, with the previous page as referrer', async () => {
    consent.grant();
    TestBed.tick();
    matomo._paq = [];

    await visit('/impressum');
    TestBed.tick();

    expect(queued()).toEqual([
      ['setReferrerUrl', `${location.origin}/`],
      ['setCustomUrl', `${location.origin}/impressum`],
      ['setDocumentTitle', 'Impressum'],
      ['trackPageView'],
    ]);
    expect(scripts().length).toBe(1);
  });

  it('reports the not-found page the way Matomo lists errors', async () => {
    consent.grant();
    TestBed.tick();
    await visit('/no/such/page?x=1');

    const [, title] =
      queued()
        .filter(([name]) => name === 'setDocumentTitle')
        .at(-1) ?? [];
    expect(title).toBe(
      `404/URL = ${encodeURIComponent('/no/such/page?x=1')}/From = ${encodeURIComponent(`${location.origin}/`)}`,
    );
  });

  it('reports events, those marked once only once per page', async () => {
    consent.grant();
    TestBed.tick();
    matomo._paq = [];

    analytics.track({ category: 'Inquiry', action: 'sent', name: 'talk' });
    analytics.track({ category: 'Demo', action: 'started' }, { once: true });
    analytics.track({ category: 'Demo', action: 'started' }, { once: true });
    expect(queued()).toEqual([
      ['trackEvent', 'Inquiry', 'sent', 'talk'],
      ['trackEvent', 'Demo', 'started'],
    ]);

    await visit('/impressum');
    matomo._paq = [];
    analytics.track({ category: 'Demo', action: 'started' }, { once: true });
    expect(queued()).toEqual([['trackEvent', 'Demo', 'started']]);
  });

  it('stops at once when the visitor withdraws consent; accepting again counts the page', async () => {
    consent.grant();
    TestBed.tick();
    matomo._paq = [];

    consent.deny();
    TestBed.tick();
    await visit('/impressum');
    analytics.track({ category: 'Demo', action: 'started' });
    expect(queued()).toEqual([['forgetConsentGiven']]);

    consent.grant();
    TestBed.tick();
    expect(names()).toEqual([
      'forgetConsentGiven',
      'setConsentGiven',
      'setReferrerUrl',
      'setCustomUrl',
      'setDocumentTitle',
      'trackPageView',
    ]);
    expect(queued()).toContainEqual(['setCustomUrl', `${location.origin}/impressum`]);
    expect(scripts().length).toBe(1);
  });
});
