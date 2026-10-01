// The languages the site is published in (ADR 0033). English is the default
// and lives at the root; every other language lives under its own prefix, so
// each page is prerendered once per language and a language switch is a plain
// link to the counterpart page.

/** Every language of the site, the default first. */
export const LANGS = ['en', 'de'] as const;

export type Lang = (typeof LANGS)[number];

export const DEFAULT_LANG: Lang = 'en';

/** Open Graph locale of each language (`og:locale`, `og:locale:alternate`). */
export const OG_LOCALES: Readonly<Record<Lang, string>> = { en: 'en_GB', de: 'de_DE' };

/** Path prefix of each language's pages; the default language has none. */
export const LANG_PREFIX: Readonly<Record<Lang, string>> = { en: '', de: '/de' };

/** The pages that exist in every language, with their path in each. */
export const PAGE_PATHS = {
  home: { en: '/', de: '/de' },
  impressum: { en: '/impressum', de: '/de/impressum' },
  privacy: { en: '/privacy', de: '/de/datenschutz' },
} as const satisfies Record<string, Readonly<Record<Lang, string>>>;

export type PageKey = keyof typeof PAGE_PATHS;

/** The language of a URL path such as `/de/impressum`; unknown prefixes are the default language. */
export function langFromPath(path: string): Lang {
  const pathname = stripQueryAndFragment(path);
  const lang = LANGS.find(
    (candidate) =>
      LANG_PREFIX[candidate] !== '' &&
      (pathname === LANG_PREFIX[candidate] || pathname.startsWith(`${LANG_PREFIX[candidate]}/`)),
  );
  return lang ?? DEFAULT_LANG;
}

/** The page a URL path shows, or `null` for a path that is no page in any language. */
export function pageAt(path: string): PageKey | null {
  const pathname = normalise(stripQueryAndFragment(path));
  const pages = Object.keys(PAGE_PATHS) as PageKey[];
  return pages.find((page) => LANGS.some((lang) => PAGE_PATHS[page][lang] === pathname)) ?? null;
}

/**
 * The path of the page at `path` in language `lang`; the home page of `lang`
 * where the page has no counterpart (the not-found page).
 */
export function counterpartPath(path: string, lang: Lang): string {
  return PAGE_PATHS[pageAt(path) ?? 'home'][lang];
}

/**
 * Link to section `id` of the home page in `lang`. The default language keeps
 * the plain `#id`, which `<base href="/">` resolves to the home page.
 */
export function sectionHref(lang: Lang, id: string): string {
  return `${LANG_PREFIX[lang]}#${id}`;
}

function stripQueryAndFragment(path: string): string {
  return path.split(/[?#]/, 1)[0];
}

/** `/de/` and `/de` are the same page. */
function normalise(pathname: string): string {
  return pathname.length > 1 && pathname.endsWith('/') ? pathname.slice(0, -1) : pathname;
}
