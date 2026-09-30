import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { ANALYTICS_ORIGIN } from '../core/analytics/analytics.config';
import { PAGE_PATHS } from '../i18n/languages';
import { shapeDifferences } from '../../testing/translation';
import { SEO_CONTENT_DE } from './seo.content.de';
import { SEO_CONTENT_EN } from './seo.content';

describe('German SEO content', () => {
  it('has the shape and the facts of the English data', () => {
    expect(shapeDifferences(SEO_CONTENT_EN, SEO_CONTENT_DE)).toEqual([]);
    expect(SEO_CONTENT_DE.locale).toBe('de_DE');
    expect(SEO_CONTENT_DE.software.codeRepository).toBe(SEO_CONTENT_EN.software.codeRepository);
    expect(SEO_CONTENT_DE.organisation).toEqual(SEO_CONTENT_EN.organisation);
    expect(SEO_CONTENT_DE.founder.sameAs).toEqual(SEO_CONTENT_EN.founder.sameAs);
  });

  it('sends the not-found page home in its own language', () => {
    expect(SEO_CONTENT_EN.notFound.home.href).toBe(PAGE_PATHS.home.en);
    expect(SEO_CONTENT_DE.notFound.home.href).toBe(PAGE_PATHS.home.de);
    expect(SEO_CONTENT_DE.notFound.description.length).toBeLessThanOrEqual(160);
  });
});

describe('Statistics server', () => {
  it('is the origin the back end allows in its Content Security Policy', () => {
    const filter = readFileSync(
      resolve(
        process.cwd(),
        '../backend/src/main/java/org/obie/website/web/SecurityHeadersFilter.java',
      ),
      'utf8',
    );
    expect(/ANALYTICS_ORIGIN = "([^"]+)"/.exec(filter)?.[1]).toBe(ANALYTICS_ORIGIN);
  });
});
