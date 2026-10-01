import {
  DEFAULT_LANG,
  LANGS,
  LANG_PREFIX,
  PAGE_PATHS,
  counterpartPath,
  langFromPath,
  pageAt,
  sectionHref,
} from './languages';

describe('Languages and paths', () => {
  it('publishes English at the root and German under /de', () => {
    expect(LANGS).toEqual(['en', 'de']);
    expect(DEFAULT_LANG).toBe('en');
    expect(LANG_PREFIX).toEqual({ en: '', de: '/de' });
  });

  it('puts every page of a language under its prefix', () => {
    for (const lang of LANGS) {
      for (const paths of Object.values(PAGE_PATHS)) {
        const path = paths[lang];
        expect(
          path === (LANG_PREFIX[lang] || '/') || path.startsWith(`${LANG_PREFIX[lang]}/`),
        ).toBe(true);
      }
    }
  });

  it('reads the language from the path', () => {
    expect(langFromPath('/')).toBe('en');
    expect(langFromPath('/impressum')).toBe('en');
    expect(langFromPath('/de')).toBe('de');
    expect(langFromPath('/de/')).toBe('de');
    expect(langFromPath('/de/datenschutz#analytics')).toBe('de');
    expect(langFromPath('/de?utm_source=x')).toBe('de');
    // Only the whole first segment counts.
    expect(langFromPath('/deutsch')).toBe('en');
    expect(langFromPath('/no/such/page')).toBe('en');
  });

  it('finds the page at a path in any language, ignoring query, fragment and a trailing slash', () => {
    expect(pageAt('/')).toBe('home');
    expect(pageAt('/#faq')).toBe('home');
    expect(pageAt('/de/')).toBe('home');
    expect(pageAt('/privacy?x=1')).toBe('privacy');
    expect(pageAt('/de/datenschutz')).toBe('privacy');
    expect(pageAt('/de/impressum')).toBe('impressum');
    expect(pageAt('/404')).toBeNull();
    expect(pageAt('/de/privacy')).toBeNull();
  });

  it('maps a page to its counterpart, and unknown pages to the home page', () => {
    expect(counterpartPath('/', 'de')).toBe('/de');
    expect(counterpartPath('/de', 'en')).toBe('/');
    expect(counterpartPath('/privacy', 'de')).toBe('/de/datenschutz');
    expect(counterpartPath('/de/datenschutz', 'en')).toBe('/privacy');
    expect(counterpartPath('/impressum', 'de')).toBe('/de/impressum');
    expect(counterpartPath('/de/no/such/page', 'en')).toBe('/');
  });

  it('links sections of the home page in each language', () => {
    expect(sectionHref('en', 'faq')).toBe('#faq');
    expect(sectionHref('de', 'faq')).toBe('/de#faq');
  });
});
