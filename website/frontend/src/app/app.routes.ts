import { Route, Routes } from '@angular/router';

import { loadLegalContent } from './content/legal-content.token';
import { LANG_PREFIX, Lang, PAGE_PATHS } from './i18n/languages';
import { Home } from './pages/home/home';
import type { LegalPageKey } from './pages/legal/legal-page';
import { NOT_FOUND_DATA, NotFound } from './pages/not-found/not-found';

/** Path of the not-found page; the back end serves it for unknown URLs. */
export const NOT_FOUND_PATH = '404';

/** Paths of the English legal pages, linked from the footer and the inquiry form. */
export const IMPRESSUM_PATH = routePath('en', PAGE_PATHS.impressum.en);
export const PRIVACY_PATH = routePath('en', PAGE_PATHS.privacy.en);

// Loaded on demand: the legal pages and their long copy are not part of the
// JavaScript the landing page needs to start.
const legalPage = (legalPage: LegalPageKey): Omit<Route, 'path'> => ({
  loadComponent: () => import('./pages/legal/legal-page').then((m) => m.LegalPage),
  resolve: { legalContent: loadLegalContent },
  data: { legalPage },
});

const notFound: Omit<Route, 'path'> = { component: NotFound, data: NOT_FOUND_DATA };

/** The pages of one language, relative to its prefix (ADR 0033). */
function pages(lang: Lang): Routes {
  return [
    { path: routePath(lang, PAGE_PATHS.home[lang]), component: Home },
    { path: routePath(lang, PAGE_PATHS.impressum[lang]), ...legalPage('impressum') },
    { path: routePath(lang, PAGE_PATHS.privacy[lang]), ...legalPage('privacy') },
    { path: NOT_FOUND_PATH, ...notFound },
  ];
}

export const routes: Routes = [
  ...pages('en'),
  // German under /de; its unknown paths get the German not-found page.
  {
    path: routePath('en', LANG_PREFIX.de),
    children: [...pages('de'), { path: '**', ...notFound }],
  },
  { path: '**', ...notFound },
];

/** A page's path relative to its language's prefix, as the router wants it. */
function routePath(lang: Lang, path: string): string {
  return path.slice(LANG_PREFIX[lang].length).replace(/^\//, '');
}
