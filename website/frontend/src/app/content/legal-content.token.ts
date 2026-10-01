import { InjectionToken, inject } from '@angular/core';
import { ResolveFn } from '@angular/router';
import { TranslocoService } from '@jsverse/transloco';
import { map } from 'rxjs';

import { translated } from '../i18n/translated';
import { LegalContent } from './legal-content.model';

/** Transloco scope of the legal pages' copy: loaded only on those pages. */
export const LEGAL_SCOPE = 'legal';

/** Copy of the legal pages in the page's language (Transloco scope `legal`). */
export const LEGAL_CONTENT = new InjectionToken<LegalContent>('LEGAL_CONTENT', {
  providedIn: 'root',
  factory: () => translated<LegalContent>(inject(TranslocoService), LEGAL_SCOPE),
});

/** Route resolver: loads the legal pages' copy before a legal page is created. */
export const loadLegalContent: ResolveFn<true> = () => {
  const transloco = inject(TranslocoService);
  return transloco.load(`${LEGAL_SCOPE}/${transloco.getActiveLang()}`).pipe(map(() => true));
};
