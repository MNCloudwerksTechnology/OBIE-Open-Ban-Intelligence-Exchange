import { LandingContent } from '../content/landing-content.model';
import { FOUNDER_AVATAR_PLACEHOLDER } from '../content/landing.content';
import { SeoContent } from '../content/seo.content';
import { Lang, PAGE_PATHS } from '../i18n/languages';

/**
 * The home page's JSON-LD (schema.org): the site as `WebSite` (search
 * engines take the site name they show in results from it), OBIE as
 * `SoftwareSourceCode`, its founder as `Person`, the company behind both as
 * `Organization`, and the FAQ section of the page in `lang` as `FAQPage`, all
 * linked by `@id`. `absolute` turns a site path into a URL on the site's
 * origin.
 *
 * The founder's photo is left out while it is the placeholder avatar, so
 * search engines never show the placeholder as his picture.
 */
export function homeStructuredData(
  seo: SeoContent,
  landing: Pick<LandingContent, 'meta' | 'founder' | 'faq'>,
  lang: Lang,
  absolute: (path: string) => string,
): object[] {
  const { founder, faq } = landing;
  const home = absolute('/');
  const website = { '@id': `${home}#website` };
  const software = { '@id': `${home}#software` };
  const organisation = { '@id': `${home}#organization` };
  const founderId = `${home}#${founder.id}`;
  const hasPhoto = founder.photo.src !== FOUNDER_AVATAR_PLACEHOLDER;

  return [
    {
      '@type': 'WebSite',
      ...website,
      url: home,
      name: seo.siteName,
      alternateName: seo.siteAlternateName,
      publisher: organisation,
      about: software,
    },
    {
      '@type': 'SoftwareSourceCode',
      ...software,
      name: seo.software.name,
      description: seo.software.description,
      url: home,
      codeRepository: seo.software.codeRepository,
      license: seo.software.license,
      programmingLanguage: seo.software.programmingLanguage,
      author: organisation,
      copyrightHolder: organisation,
    },
    {
      '@type': 'Organization',
      ...organisation,
      name: seo.organisation.name,
      url: seo.organisation.url,
    },
    {
      '@type': 'Person',
      '@id': founderId,
      name: founder.name,
      jobTitle: seo.founder.jobTitle,
      worksFor: organisation,
      url: founderId,
      sameAs: seo.founder.sameAs,
      ...(hasPhoto ? { image: absolute(founder.photo.src) } : {}),
    },
    {
      '@type': 'FAQPage',
      '@id': `${absolute(PAGE_PATHS.home[lang])}#${faq.id}`,
      inLanguage: landing.meta.locale,
      isPartOf: website,
      about: software,
      // Exactly the questions and answers the section shows.
      mainEntity: faq.items.map(({ question, answer }) => ({
        '@type': 'Question',
        name: question,
        acceptedAnswer: { '@type': 'Answer', text: answer },
      })),
    },
  ];
}
