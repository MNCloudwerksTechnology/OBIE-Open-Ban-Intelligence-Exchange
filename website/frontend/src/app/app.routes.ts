import { Routes } from '@angular/router';

import { Home } from './pages/home/home';
import type { LegalPageKey } from './pages/legal/legal-page';
import { NotFound } from './pages/not-found/not-found';

/** Path of the not-found page; the back end serves it for unknown URLs. */
export const NOT_FOUND_PATH = '404';

/** Paths of the legal pages, linked from the footer and the inquiry form. */
export const IMPRESSUM_PATH = 'impressum';
export const PRIVACY_PATH = 'privacy';

// Loaded on demand: the legal pages and their long copy are not part of the
// JavaScript the landing page needs to start.
const legalPage = (legalPage: LegalPageKey) => ({
  loadComponent: () => import('./pages/legal/legal-page').then((m) => m.LegalPage),
  data: { legalPage },
});

export const routes: Routes = [
  { path: '', component: Home },
  { path: IMPRESSUM_PATH, ...legalPage('impressum') },
  { path: PRIVACY_PATH, ...legalPage('privacy') },
  { path: NOT_FOUND_PATH, component: NotFound },
  { path: '**', component: NotFound },
];
