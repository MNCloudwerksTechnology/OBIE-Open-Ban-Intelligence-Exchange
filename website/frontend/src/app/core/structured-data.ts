import { FounderContent } from '../content/landing-content.model';
import { FOUNDER_AVATAR_PLACEHOLDER } from '../content/landing.content';
import { SeoContent } from '../content/seo.content';

/**
 * The home page's JSON-LD (schema.org): OBIE as `SoftwareSourceCode`, its
 * founder as `Person` and the company behind both as `Organization`, linked
 * by `@id`. `absolute` turns a site path into a URL on the site's origin.
 *
 * The founder's photo is left out while it is the placeholder avatar, so
 * search engines never show the placeholder as his picture.
 */
export function homeStructuredData(
  seo: SeoContent,
  founder: FounderContent,
  absolute: (path: string) => string,
): object[] {
  const home = absolute('/');
  const organisationId = `${home}#organization`;
  const founderId = `${home}#${founder.id}`;
  const organisation = { '@id': organisationId };
  const hasPhoto = founder.photo.src !== FOUNDER_AVATAR_PLACEHOLDER;

  return [
    {
      '@type': 'SoftwareSourceCode',
      '@id': `${home}#software`,
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
      '@id': organisationId,
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
  ];
}
