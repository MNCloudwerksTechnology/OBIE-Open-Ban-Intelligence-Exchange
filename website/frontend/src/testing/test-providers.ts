import { provideI18n } from '../app/i18n/provide-i18n';

/**
 * Providers of every test's TestBed (angular.json, `providersFile`): the
 * site's translations, as the application has them. The language follows
 * the test document's URL, which is English; a test of the German pages
 * provides `LANG` and waits for `whenI18nReady()`.
 */
export default [provideI18n()];
