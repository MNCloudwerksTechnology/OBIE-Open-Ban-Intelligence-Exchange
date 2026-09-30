import { InjectionToken, inject } from '@angular/core';
import { TranslocoService } from '@jsverse/transloco';

import { ANALYTICS_HOST } from '../core/analytics/analytics.config';
import { PAGE_PATHS } from '../i18n/languages';
import { translated } from '../i18n/translated';
import { Link } from './landing-content.model';

// Copy of the question about visitor statistics (ADR 0033). German consent
// rules apply (§ 25 TDDDG, Art. 7 GDPR): say what is measured and by whom,
// offer declining as prominently as accepting, and say how to change the
// choice later. Keep it in step with the privacy policy's "analytics" section.

/** Shape of the consent dialog's copy; the German version is a second object. */
export interface ConsentContent {
  /** The dialog's heading and accessible name. */
  readonly heading: string;
  /** Short paragraphs: what is measured, then that it is optional and can be changed. */
  readonly paragraphs: readonly string[];
  /** The privacy policy's section on visitor statistics. */
  readonly privacyLink: Link;
  readonly accept: string;
  readonly decline: string;
  /** The choice in effect, shown when the visitor reopens the dialog. */
  readonly current: { readonly granted: string; readonly denied: string };
  /** Footer button that reopens the dialog. */
  readonly settings: string;
}

/** Anchor of the privacy policy's section on visitor statistics. */
export const ANALYTICS_SECTION_ID = 'analytics';

/** English consent copy. */
export const CONSENT_CONTENT_EN: ConsentContent = {
  heading: 'Visitor statistics',
  paragraphs: [
    `May we count your visit? With your consent, Matomo on our own server (${ANALYTICS_HOST}) records which pages are read, where visitors come from and which links they follow.`,
    'No cookies, nothing passed on to third parties. You can change your choice at any time under “Privacy settings” at the bottom of the page.',
  ],
  privacyLink: {
    label: 'Details in the privacy policy',
    href: `${PAGE_PATHS.privacy.en}#${ANALYTICS_SECTION_ID}`,
  },
  accept: 'Accept',
  decline: 'Decline',
  current: {
    granted: 'You currently allow visitor statistics.',
    denied: 'You currently decline visitor statistics.',
  },
  settings: 'Privacy settings',
};

/** The consent copy in the page's language. */
export const CONSENT_CONTENT = new InjectionToken<ConsentContent>('CONSENT_CONTENT', {
  providedIn: 'root',
  factory: () => translated<ConsentContent>(inject(TranslocoService), 'consent'),
});
