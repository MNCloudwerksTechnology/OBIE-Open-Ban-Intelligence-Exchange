import { TranslocoService } from '@jsverse/transloco';

/**
 * The copy object stored under `key` in the active language's translation,
 * e.g. the whole landing page for `landing`. The translations are typed
 * objects, not flat strings (ADR 0032), so this is how components read them.
 * It fails loudly when the translation has not been loaded: `provideI18n`
 * loads the page's language before the first component is created.
 */
export function translated<T extends object>(transloco: TranslocoService, key: string): T {
  const value: unknown = transloco.translateObject(key);
  if (value === null || typeof value !== 'object') {
    throw new Error(`No "${key}" translation loaded for "${transloco.getActiveLang()}"`);
  }
  return value as T;
}
