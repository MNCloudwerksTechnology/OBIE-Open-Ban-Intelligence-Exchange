import { Injectable } from '@angular/core';
import { Translation, TranslocoLoader } from '@jsverse/transloco';
import { Observable, of } from 'rxjs';

import { TRANSLATION_EN, asTranslation } from './site-translation';

/**
 * Where each translation comes from. The translations are TypeScript objects
 * compiled into the application, never fetched as JSON (ADR 0032): English is
 * in the main bundle, every other language and the legal pages' copy are
 * chunks the bundler splits off and loads on demand.
 */
const TRANSLATIONS: Readonly<Record<string, () => Promise<object>>> = {
  de: () => import('./site-translation.de').then((m) => m.TRANSLATION_DE),
  'legal/en': () => import('../content/legal.content').then((m) => m.LEGAL_CONTENT_EN),
  'legal/de': () => import('../content/legal.content.de').then((m) => m.LEGAL_CONTENT_DE),
};

/** Transloco loader of the site's built-in translations. */
@Injectable({ providedIn: 'root' })
export class SiteTranslationLoader implements TranslocoLoader {
  getTranslation(path: string): Observable<Translation> | Promise<Translation> {
    if (path === 'en') {
      // Synchronous, so English pages start without waiting for a chunk.
      return of(asTranslation(TRANSLATION_EN));
    }
    const load = TRANSLATIONS[path];
    if (!load) {
      return Promise.reject(new Error(`No translation "${path}"`));
    }
    return load().then(asTranslation);
  }
}
