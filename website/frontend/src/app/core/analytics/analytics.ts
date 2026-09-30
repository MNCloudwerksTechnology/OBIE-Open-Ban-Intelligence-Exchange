import { DOCUMENT, Injectable, Injector, effect, inject, untracked } from '@angular/core';
import { ActivatedRouteSnapshot, Router } from '@angular/router';

import { NOT_FOUND_DATA } from '../../pages/not-found/not-found';
import { ANALYTICS_ORIGIN, MATOMO_SITE_ID } from './analytics.config';
import { ConsentService } from './consent.service';

/** A command for Matomo's queue, e.g. `['trackPageView']`. */
export type MatomoCommand = [string, ...unknown[]];

/** Matomo reads its commands from `window._paq`, before and after its script loads. */
export interface MatomoWindow extends Window {
  _paq?: MatomoCommand[];
}

/**
 * The actions the site reports besides page views and links to other sites.
 * Categories and actions are fixed English names, so reports read the same
 * in every language; `name` carries a detail such as the inquiry type.
 */
export interface AnalyticsEvent {
  readonly category: 'Inquiry' | 'Demo';
  readonly action: string;
  readonly name?: string;
}

/**
 * Visitor statistics with the self-hosted Matomo (ADR 0033), strictly after
 * consent. Until the visitor accepts, it neither loads Matomo nor sends
 * anything; after they decline, Matomo drops everything it would still send.
 *
 * What it measures: a page view per route the visitor opens (the not-found
 * page as Matomo's "404/URL = …" title), links to other sites (Matomo's link
 * tracking), the time a page stays open (heartbeat) and the events in
 * `AnalyticsEvent`. Matomo runs without cookies.
 */
@Injectable({ providedIn: 'root' })
export class Analytics {
  private readonly document = inject(DOCUMENT);
  private readonly router = inject(Router);
  private readonly consent = inject(ConsentService);
  private readonly injector = inject(Injector);

  private started = false;
  private loaded = false;
  private enabled = false;
  /** Id of the navigation whose page view was sent last. */
  private trackedNavigation: number | null = null;
  /** URL of the previous page view, the referrer of the next one. */
  private previousUrl: string | null = null;
  /** Events sent once per page view, by key. */
  private readonly sentOnce = new Set<string>();

  /** Starts following the consent and the router. Once, in the browser, after the first render. */
  start(): void {
    if (this.started) {
      return;
    }
    this.started = true;
    effect(
      () => {
        const granted = this.consent.decision() === 'granted';
        const navigation = this.router.lastSuccessfulNavigation();
        untracked(() => {
          if (!granted) {
            this.disable();
            return;
          }
          this.enable();
          if (navigation) {
            this.trackPageView(navigation.id);
          }
        });
      },
      { injector: this.injector },
    );
  }

  /** Reports `event` if the visitor consented; with `once`, at most once per page view. */
  track(event: AnalyticsEvent, options: { once?: boolean } = {}): void {
    if (!this.enabled) {
      return;
    }
    const key = [event.category, event.action, event.name ?? ''].join('\u0000');
    if (options.once) {
      if (this.sentOnce.has(key)) {
        return;
      }
      this.sentOnce.add(key);
    }
    const command: MatomoCommand = ['trackEvent', event.category, event.action];
    if (event.name !== undefined) {
      command.push(event.name);
    }
    this.push(command);
  }

  private enable(): void {
    if (this.enabled) {
      return;
    }
    this.enabled = true;
    if (this.loaded) {
      // Consent given again on the same page, after it was withdrawn.
      this.push(['setConsentGiven']);
      return;
    }
    this.loaded = true;
    // Matomo applies these before any queued tracking call.
    this.push(['requireConsent']);
    this.push(['setConsentGiven']);
    this.push(['disableCookies']);
    this.push(['setTrackerUrl', `${ANALYTICS_ORIGIN}/matomo.php`]);
    this.push(['setSiteId', MATOMO_SITE_ID]);
    this.push(['enableLinkTracking']);
    this.push(['enableHeartBeatTimer']);
    const script = this.document.createElement('script');
    script.async = true;
    script.src = `${ANALYTICS_ORIGIN}/matomo.js`;
    this.document.head.appendChild(script);
  }

  private disable(): void {
    if (!this.enabled) {
      return;
    }
    this.enabled = false;
    // Matomo keeps whatever it would send from now on to itself.
    this.push(['forgetConsentGiven']);
  }

  private trackPageView(navigationId: number): void {
    if (navigationId === this.trackedNavigation) {
      return;
    }
    this.trackedNavigation = navigationId;
    this.sentOnce.clear();
    // The router's URL: the address bar may be updated only after the navigation ends.
    const url = new URL(this.router.url, this.document.location.origin).href;
    const referrer = this.previousUrl ?? this.document.referrer;
    if (this.previousUrl !== null) {
      this.push(['setReferrerUrl', this.previousUrl]);
    }
    this.push(['setCustomUrl', url]);
    this.push([
      'setDocumentTitle',
      this.isNotFound() ? notFoundTitle(url, referrer) : this.document.title,
    ]);
    this.push(['trackPageView']);
    this.previousUrl = url;
  }

  private isNotFound(): boolean {
    let route: ActivatedRouteSnapshot = this.router.routerState.snapshot.root;
    while (route.firstChild) {
      route = route.firstChild;
    }
    return route.data['notFound'] === NOT_FOUND_DATA.notFound;
  }

  private push(command: MatomoCommand): void {
    const window = this.document.defaultView as MatomoWindow | null;
    if (window) {
      (window._paq ??= []).push(command);
    }
  }
}

/** Matomo's convention for not-found pages, which its reports list as errors. */
function notFoundTitle(url: string, referrer: string): string {
  const { pathname, search } = new URL(url);
  return `404/URL = ${encodeURIComponent(pathname + search)}/From = ${encodeURIComponent(referrer)}`;
}
