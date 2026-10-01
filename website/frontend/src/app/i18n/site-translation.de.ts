import { CONSENT_CONTENT_DE } from '../content/consent.content.de';
import { LANDING_CONTENT_DE } from '../content/landing.content.de';
import { SEO_CONTENT_DE } from '../content/seo.content.de';
import { SiteTranslation } from './site-translation';

/** German; loaded as a chunk of its own, only by German pages. */
export const TRANSLATION_DE: SiteTranslation = {
  landing: LANDING_CONTENT_DE,
  seo: SEO_CONTENT_DE,
  consent: CONSENT_CONTENT_DE,
};
