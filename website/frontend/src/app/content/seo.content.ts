import { InjectionToken } from '@angular/core';

import { REPOSITORY_URL } from './landing.content';

// Search engine and share-preview data (WP #1678): the share image, the copy
// of pages without a content file of their own, and the facts behind the
// structured data (JSON-LD). Every fact about OBIE and its founder was
// supplied by the operator; nothing here is invented.

/** Shape of the SEO data, so a German version is a second object. */
export interface SeoContent {
  /** `og:site_name`. */
  readonly siteName: string;
  /** Open Graph locale, e.g. `en_GB`. */
  readonly locale: string;
  /** The share image, a static asset in `public/` (1200 × 630 px). */
  readonly shareImage: {
    readonly path: string;
    readonly width: number;
    readonly height: number;
    readonly alt: string;
  };
  /** Title and description of the not-found page. */
  readonly notFound: { readonly title: string; readonly description: string };
  /** Facts behind the `SoftwareSourceCode` JSON-LD. */
  readonly software: {
    readonly name: string;
    readonly description: string;
    readonly codeRepository: string;
    /** SPDX licence URL. */
    readonly license: string;
    readonly programmingLanguage: string;
  };
  /** Author and copyright holder of OBIE, the founder's employer. */
  readonly organisation: { readonly name: string; readonly url: string };
  /** Facts behind the founder's `Person` JSON-LD besides name and photo (from the founder content). */
  readonly founder: { readonly jobTitle: string; readonly sameAs: readonly string[] };
}

/** Path of the share image; `scripts/share-image.mjs` generates it. */
export const SHARE_IMAGE_PATH = '/social/obie-share.png';

export const SEO_CONTENT_EN: SeoContent = {
  siteName: 'OBIE',
  locale: 'en_GB',
  shareImage: {
    path: SHARE_IMAGE_PATH,
    width: 1200,
    height: 630,
    alt: 'OBIE, Open Ban Intelligence Exchange: Shared Intelligence, Sovereign Enforcement.',
  },
  notFound: {
    title: 'Page not found · OBIE',
    description:
      'This page does not exist on the OBIE website. The home page explains OBIE, the Open Ban Intelligence Exchange.',
  },
  software: {
    name: 'OBIE (Open Ban Intelligence Exchange)',
    description:
      'OBIE is an open, leaderless protocol for sharing cryptographically signed attacker signals between servers, where every node keeps the final say over what it blocks.',
    codeRepository: REPOSITORY_URL,
    license: 'https://spdx.org/licenses/MIT.html',
    programmingLanguage: 'Go',
  },
  organisation: { name: 'Cloudwerks Technology GmbH', url: 'https://cloudwerks.de' },
  founder: {
    jobTitle: 'Founder of OBIE, software architect',
    sameAs: ['https://www.linkedin.com/in/niewerth/', 'https://github.com/MNCloudwerksTechnology'],
  },
};

/** The SEO data in effect; override it to provide another language. */
export const SEO_CONTENT = new InjectionToken<SeoContent>('SEO_CONTENT', {
  providedIn: 'root',
  factory: () => SEO_CONTENT_EN,
});
