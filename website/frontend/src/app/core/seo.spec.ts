import { PLATFORM_ID } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';

import { routes } from '../app.routes';
import { FounderContent } from '../content/landing-content.model';
import { LANDING_CONTENT_EN } from '../content/landing.content';
import { SEO_CONTENT_EN } from '../content/seo.content';
import { SITE_ORIGIN, SITE_ORIGIN_PLACEHOLDER, jsonForScript } from './seo';
import { homeStructuredData } from './structured-data';

const ORIGIN = 'https://obie.example';

/** Every prerendered route; the back end's sitemap lists all but the not-found page. */
const INDEXED_ROUTES = ['/', '/impressum', '/privacy'];

interface Head {
  title: string;
  description: string | null;
  robots: string | null;
  canonical: string | null;
  property: (name: string) => string | null;
  name: (name: string) => string | null;
  jsonLd: { '@context': string; '@graph': Record<string, unknown>[] } | null;
}

function readHead(): Head {
  const head = document.head;
  const content = (selector: string) =>
    head.querySelector(`meta[${selector}]`)?.getAttribute('content') ?? null;
  const jsonLd = head.querySelector('script[type="application/ld+json"]')?.textContent;
  return {
    title: document.title,
    description: content('name="description"'),
    robots: content('name="robots"'),
    canonical: head.querySelector('link[rel="canonical"]')?.getAttribute('href') ?? null,
    property: (name) => content(`property="${name}"`),
    name: (name) => content(`name="${name}"`),
    jsonLd: jsonLd ? JSON.parse(jsonLd) : null,
  };
}

/** One harness per test; later visits navigate the same application, as a visitor does. */
let harness: RouterTestingHarness | undefined;

async function visit(url: string): Promise<Head> {
  harness ??= await RouterTestingHarness.create();
  await harness.navigateByUrl(url);
  return readHead();
}

describe('SEO head tags', () => {
  beforeEach(() => {
    harness = undefined;
    TestBed.configureTestingModule({
      providers: [provideRouter(routes), { provide: SITE_ORIGIN, useValue: ORIGIN }],
    });
  });

  it('gives every route a unique title and description of search-result length', async () => {
    const heads: Head[] = [];
    for (const url of [...INDEXED_ROUTES, '/404']) {
      heads.push(await visit(url));
    }
    const titles = heads.map((head) => head.title);
    const descriptions = heads.map((head) => head.description ?? '');
    expect(new Set(titles).size).toBe(heads.length);
    expect(new Set(descriptions).size).toBe(heads.length);
    for (const title of titles) {
      expect(title.length, title).toBeLessThanOrEqual(60);
    }
    for (const description of descriptions) {
      expect(description.length, description).toBeGreaterThanOrEqual(70);
      expect(description.length, description).toBeLessThanOrEqual(160);
    }
  });

  it.each(INDEXED_ROUTES)('links %s to its canonical URL and lets it be indexed', async (url) => {
    const head = await visit(url);
    expect(head.canonical).toBe(ORIGIN + url);
    expect(head.robots).toBeNull();
    expect(head.property('og:url')).toBe(ORIGIN + url);
  });

  it.each(INDEXED_ROUTES)('describes %s for share previews', async (url) => {
    const head = await visit(url);
    const image = SEO_CONTENT_EN.shareImage;
    expect(head.property('og:type')).toBe('website');
    expect(head.property('og:site_name')).toBe('OBIE');
    expect(head.property('og:title')).toBe(head.title);
    expect(head.property('og:description')).toBe(head.description);
    expect(head.property('og:image')).toBe(`${ORIGIN}/social/obie-share.png`);
    expect(head.property('og:image:width')).toBe('1200');
    expect(head.property('og:image:height')).toBe('630');
    expect(head.property('og:image:alt')).toBe(image.alt);
    expect(head.name('twitter:card')).toBe('summary_large_image');
    expect(head.name('twitter:title')).toBe(head.title);
    expect(head.name('twitter:description')).toBe(head.description);
    expect(head.name('twitter:image')).toBe(`${ORIGIN}/social/obie-share.png`);
  });

  it('keeps the not-found page out of the index and drops the previous page’s canonical', async () => {
    await visit('/impressum');
    const head = await visit('/404');
    expect(head.robots).toBe('noindex');
    expect(head.canonical).toBeNull();
    expect(head.property('og:url')).toBeNull();
    expect(head.jsonLd).toBeNull();
  });

  it('adds structured data to the home page only', async () => {
    expect((await visit('/')).jsonLd?.['@graph'].map((node) => node['@type'])).toEqual([
      'SoftwareSourceCode',
      'Organization',
      'Person',
    ]);
    expect((await visit('/privacy')).jsonLd).toBeNull();
    expect(document.head.querySelectorAll('link[rel="canonical"]').length).toBe(1);
  });
});

describe('SITE_ORIGIN', () => {
  it('is the placeholder the back end replaces while prerendering', () => {
    TestBed.configureTestingModule({ providers: [{ provide: PLATFORM_ID, useValue: 'server' }] });
    expect(TestBed.inject(SITE_ORIGIN)).toBe(SITE_ORIGIN_PLACEHOLDER);
  });

  it('is the origin of the served canonical link in the browser', () => {
    const link = document.createElement('link');
    link.rel = 'canonical';
    link.href = 'https://obie.example/privacy';
    document.head.appendChild(link);
    try {
      expect(TestBed.inject(SITE_ORIGIN)).toBe('https://obie.example');
    } finally {
      link.remove();
    }
  });
});

describe('Home page structured data', () => {
  const absolute = (path: string) => ORIGIN + path;
  const byType = (graph: object[], type: string) =>
    graph.find((node) => (node as Record<string, unknown>)['@type'] === type) as Record<
      string,
      unknown
    >;

  it('describes OBIE as source code with the operator-supplied facts', () => {
    const software = byType(
      homeStructuredData(SEO_CONTENT_EN, LANDING_CONTENT_EN.founder, absolute),
      'SoftwareSourceCode',
    );
    expect(software).toMatchObject({
      name: 'OBIE (Open Ban Intelligence Exchange)',
      url: `${ORIGIN}/`,
      codeRepository:
        'https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange',
      license: 'https://spdx.org/licenses/MIT.html',
      programmingLanguage: 'Go',
      author: { '@id': `${ORIGIN}/#organization` },
      copyrightHolder: { '@id': `${ORIGIN}/#organization` },
    });
  });

  it('describes the founder without a picture while the photo is the placeholder', () => {
    const graph = homeStructuredData(SEO_CONTENT_EN, LANDING_CONTENT_EN.founder, absolute);
    const person = byType(graph, 'Person');
    expect(person).toMatchObject({
      name: 'Markus Niewerth',
      jobTitle: 'Founder of OBIE, software architect',
      worksFor: { '@id': `${ORIGIN}/#organization` },
      sameAs: [
        'https://www.linkedin.com/in/niewerth/',
        'https://github.com/MNCloudwerksTechnology',
      ],
    });
    expect(person).not.toHaveProperty('image');
    expect(byType(graph, 'Organization')).toMatchObject({
      name: 'Cloudwerks Technology GmbH',
      url: 'https://cloudwerks.de',
    });
  });

  it('adds the founder’s photo once the operator supplied one', () => {
    const founder: FounderContent = {
      ...LANDING_CONTENT_EN.founder,
      photo: { src: '/founder/markus-niewerth.webp', alt: 'Markus Niewerth' },
    };
    const person = byType(homeStructuredData(SEO_CONTENT_EN, founder, absolute), 'Person');
    expect(person['image']).toBe(`${ORIGIN}/founder/markus-niewerth.webp`);
  });
});

describe('jsonForScript', () => {
  it('escapes "<" so data cannot close the script element', () => {
    const json = jsonForScript({ text: '</script><script>alert(1)</script>' });
    expect(json).not.toContain('<');
    expect(JSON.parse(json)).toEqual({ text: '</script><script>alert(1)</script>' });
  });
});
