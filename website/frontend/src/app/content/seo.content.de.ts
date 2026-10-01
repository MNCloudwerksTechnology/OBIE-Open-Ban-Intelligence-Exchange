import { PAGE_PATHS } from '../i18n/languages';
import { SEO_CONTENT_EN, SeoContent } from './seo.content';

/** German SEO data: SEO_CONTENT_EN (seo.content.ts) in German, the same facts. */
export const SEO_CONTENT_DE: SeoContent = {
  siteName: 'OBIE',
  siteAlternateName: SEO_CONTENT_EN.siteAlternateName,
  locale: 'de_DE',
  shareImage: {
    ...SEO_CONTENT_EN.shareImage,
    // The image itself carries the English claim.
    alt: 'OBIE, Open Ban Intelligence Exchange: Shared Intelligence, Sovereign Enforcement.',
  },
  notFound: {
    title: 'Seite nicht gefunden · OBIE',
    description:
      'Diese Seite gibt es auf der OBIE-Website nicht. Die Startseite erklärt OBIE, den Open Ban Intelligence Exchange.',
    heading: 'Seite nicht gefunden',
    text: 'Die gesuchte Seite gibt es nicht.',
    home: { label: 'Zur Startseite', href: PAGE_PATHS.home.de },
  },
  software: {
    ...SEO_CONTENT_EN.software,
    description:
      'OBIE ist ein offenes Protokoll ohne zentrale Instanz, mit dem Server kryptografisch signierte Hinweise auf Angreifer austauschen. Jeder Knoten entscheidet selbst, was er sperrt.',
  },
  organisation: SEO_CONTENT_EN.organisation,
  founder: {
    ...SEO_CONTENT_EN.founder,
    jobTitle: 'Gründer von OBIE, Softwarearchitekt',
  },
};
