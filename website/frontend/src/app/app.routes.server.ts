import { RenderMode, ServerRoute } from '@angular/ssr';

import { NOT_FOUND_PATH } from './app.routes';

// Every public route is prerendered at build time (output mode "static"), so
// production needs no Node runtime. Unknown URLs never reach the client
// router: the back end answers them with the prerendered 404 page.
export const serverRoutes: ServerRoute[] = [
  { path: '', renderMode: RenderMode.Prerender },
  { path: NOT_FOUND_PATH, renderMode: RenderMode.Prerender },
  { path: '**', renderMode: RenderMode.Client },
];
