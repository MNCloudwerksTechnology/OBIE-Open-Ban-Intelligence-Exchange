import { PlatformLocation } from '@angular/common';
import {
  DOCUMENT,
  EnvironmentProviders,
  InjectionToken,
  inject,
  isDevMode,
  makeEnvironmentProviders,
  provideAppInitializer,
} from '@angular/core';
import { TranslocoService, provideTransloco } from '@jsverse/transloco';
import { firstValueFrom } from 'rxjs';

import { DEFAULT_LANG, LANGS, Lang, langFromPath } from './languages';
import { SiteTranslationLoader } from './site-translation-loader';

/**
 * The language of this page, from its URL. It is fixed for the life of the
 * application: the language switch is a plain link that loads the other
 * language's prerendered page, and no in-app link crosses languages.
 */
export const LANG = new InjectionToken<Lang>('LANG', {
  providedIn: 'root',
  factory: () => langFromPath(inject(PlatformLocation).pathname),
});

/**
 * Transloco with the site's built-in translations (ADR 0032). Before the
 * first component is created, both while prerendering and in the browser,
 * the page's language becomes the active one, its translation is loaded
 * and `<html lang>` says it. Prerendered page and hydrating application
 * therefore always render the same language.
 */
export function provideI18n(): EnvironmentProviders {
  return makeEnvironmentProviders([
    provideTransloco({
      config: {
        availableLangs: [...LANGS],
        defaultLang: DEFAULT_LANG,
        // The language never changes while the application runs.
        reRenderOnLangChange: false,
        prodMode: !isDevMode(),
      },
      loader: SiteTranslationLoader,
    }),
    provideAppInitializer(() => {
      const lang = inject(LANG);
      const transloco = inject(TranslocoService);
      transloco.setActiveLang(lang);
      inject(DOCUMENT).documentElement.lang = lang;
      return firstValueFrom(transloco.load(lang));
    }),
  ]);
}
