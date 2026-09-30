import { Translation } from '@jsverse/transloco';

import { CONSENT_CONTENT_EN, ConsentContent } from '../content/consent.content';
import { LandingContent } from '../content/landing-content.model';
import { LANDING_CONTENT_EN } from '../content/landing.content';
import { SEO_CONTENT_EN, SeoContent } from '../content/seo.content';

/**
 * Everything one language needs on every page, registered with Transloco as
 * that language's translation. The legal pages' copy is a separate scope,
 * `legal`, loaded only on those pages.
 */
export interface SiteTranslation {
  readonly landing: LandingContent;
  readonly seo: SeoContent;
  readonly consent: ConsentContent;
}

/** English, the default language; part of the main bundle. */
export const TRANSLATION_EN: SiteTranslation = {
  landing: LANDING_CONTENT_EN,
  seo: SEO_CONTENT_EN,
  consent: CONSENT_CONTENT_EN,
};

/** A typed translation as the plain object Transloco stores. */
export function asTranslation(translation: object): Translation {
  return translation as Translation;
}
