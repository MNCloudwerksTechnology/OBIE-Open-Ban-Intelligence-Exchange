import { ApplicationInitStatus, Provider } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { Lang } from '../app/i18n/languages';
import { LANG } from '../app/i18n/provide-i18n';

/** Renders the application in `lang`, as if the page's URL said so. */
export function provideLang(lang: Lang): Provider {
  return { provide: LANG, useValue: lang };
}

/**
 * Resolves once the page's translation is loaded. English is there at once;
 * other languages are chunks, so a test of them awaits this after
 * configuring the testing module and before creating a component.
 */
export async function whenI18nReady(): Promise<void> {
  await TestBed.inject(ApplicationInitStatus).donePromise;
}
