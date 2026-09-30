// Visitor statistics with Matomo (ADR 0033). Nothing here runs before the
// visitor consents: until then the browser never contacts the statistics
// server. The back end allows exactly this origin in its Content Security
// Policy (`SecurityHeadersFilter.ANALYTICS_ORIGIN`); change both together.

/** Host name of the self-hosted Matomo, as the privacy policy names it. */
export const ANALYTICS_HOST = 'metrics.cloudwerks.de';

/** Origin of the Matomo server: its script and its tracking endpoint. */
export const ANALYTICS_ORIGIN = `https://${ANALYTICS_HOST}`;

/** Matomo's id of this website. */
export const MATOMO_SITE_ID = '5';

/**
 * Local-storage key of the visitor's answer, `granted` or `denied`. The
 * version suffix lets a changed purpose ask everyone again.
 */
export const CONSENT_STORAGE_KEY = 'obie-analytics-consent-v1';
