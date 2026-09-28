import { Routes } from '@angular/router';

import { Home } from './pages/home/home';
import { LegalPage, LegalPageKey } from './pages/legal/legal-page';
import { NotFound } from './pages/not-found/not-found';

/** Path of the not-found page; the back end serves it for unknown URLs. */
export const NOT_FOUND_PATH = '404';

/** Paths of the legal pages, linked from the footer and the inquiry form. */
export const IMPRESSUM_PATH = 'impressum';
export const PRIVACY_PATH = 'privacy';

const legalPage = (legalPage: LegalPageKey) => ({ component: LegalPage, data: { legalPage } });

export const routes: Routes = [
  { path: '', component: Home },
  { path: IMPRESSUM_PATH, ...legalPage('impressum') },
  { path: PRIVACY_PATH, ...legalPage('privacy') },
  { path: NOT_FOUND_PATH, component: NotFound },
  { path: '**', component: NotFound },
];
