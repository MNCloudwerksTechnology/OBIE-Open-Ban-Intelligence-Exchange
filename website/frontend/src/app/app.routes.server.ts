import { RenderMode, ServerRoute } from '@angular/ssr';

import { NOT_FOUND_PATH } from './app.routes';
import { LANGS, LANG_PREFIX, PAGE_PATHS } from './i18n/languages';

// Every public route is prerendered at build time (output mode "static"), so
// production needs no Node runtime: each page once per language, and the
// not-found page per language as well. Unknown URLs never reach the client
// router: the back end answers them with the prerendered 404 page of their
// language.
export const serverRoutes: ServerRoute[] = [
  ...LANGS.flatMap((lang) =>
    [
      ...Object.values(PAGE_PATHS).map((paths) => paths[lang]),
      `${LANG_PREFIX[lang]}/${NOT_FOUND_PATH}`,
    ].map((path): ServerRoute => ({ path: path.slice(1), renderMode: RenderMode.Prerender })),
  ),
  { path: '**', renderMode: RenderMode.Client },
];
