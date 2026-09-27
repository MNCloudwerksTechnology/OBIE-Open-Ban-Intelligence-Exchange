import { Routes } from '@angular/router';

import { Home } from './pages/home/home';
import { NotFound } from './pages/not-found/not-found';

/** Path of the not-found page; the back end serves it for unknown URLs. */
export const NOT_FOUND_PATH = '404';

export const routes: Routes = [
  { path: '', component: Home },
  { path: NOT_FOUND_PATH, component: NotFound, title: 'Page not found · OBIE' },
  { path: '**', component: NotFound, title: 'Page not found · OBIE' },
];
