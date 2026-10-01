import { ANALYTICS_HOST } from '../core/analytics/analytics.config';
import { PAGE_PATHS } from '../i18n/languages';
import { ANALYTICS_SECTION_ID, ConsentContent } from './consent.content';

/** German consent copy: CONSENT_CONTENT_EN (consent.content.ts) in German. */
export const CONSENT_CONTENT_DE: ConsentContent = {
  heading: 'Besucherstatistik',
  paragraphs: [
    `Dürfen wir Ihren Besuch zählen? Mit Ihrer Einwilligung erfasst Matomo auf unserem eigenen Server (${ANALYTICS_HOST}), welche Seiten gelesen werden, woher Besucher kommen und welchen Links sie folgen.`,
    'Keine Cookies, keine Weitergabe an Dritte. Ihre Wahl können Sie jederzeit unter „Datenschutz-Einstellungen“ am Ende der Seite ändern.',
  ],
  privacyLink: {
    label: 'Details in der Datenschutzerklärung',
    href: `${PAGE_PATHS.privacy.de}#${ANALYTICS_SECTION_ID}`,
  },
  accept: 'Zustimmen',
  decline: 'Ablehnen',
  current: {
    granted: 'Sie haben der Besucherstatistik zugestimmt.',
    denied: 'Sie haben die Besucherstatistik abgelehnt.',
  },
  settings: 'Datenschutz-Einstellungen',
};
