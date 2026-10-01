import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import axe from 'axe-core';

import { renderDeferBlocks } from '../testing/defer-blocks';
import { provideLang, whenI18nReady } from '../testing/i18n';
import { stubSystemTheme } from '../testing/system-theme';
import { App } from './app';
import { routes } from './app.routes';
import { CONSENT_STORAGE_KEY } from './core/analytics/analytics.config';
import { Theme, ThemeService } from './core/theme.service';
import { langFromPath } from './i18n/languages';

// Automated accessibility check (axe-core, WCAG 2.1 A and AA rules plus best
// practices) of every prerendered route in both themes, the way a visitor
// reaches them: the whole application shell around the page. jsdom has no
// layout, so axe cannot measure colour contrast here; styles.spec.ts checks
// the colour tokens of both themes and Lighthouse the rendered page. Every
// page is checked in both languages, with the question about visitor
// statistics open, as on a first visit.

const ROUTES = [
  '/',
  '/impressum',
  '/privacy',
  '/404',
  '/de',
  '/de/impressum',
  '/de/datenschutz',
  '/de/404',
];
const THEMES: Theme[] = ['light', 'dark'];

const OPTIONS: axe.RunOptions = {
  runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice'] },
  resultTypes: ['violations'],
};

/** One line per violation and element, readable in the test output. */
function describeViolations(violations: axe.Result[]): string[] {
  return violations.flatMap((violation) =>
    violation.nodes.map((node) => `${violation.id}: ${violation.help} at ${node.target.join(' ')}`),
  );
}

describe('Accessibility (axe)', () => {
  afterEach(() => {
    document.documentElement.removeAttribute('data-theme');
    document.documentElement.lang = 'en';
    vi.unstubAllGlobals();
  });

  for (const theme of THEMES) {
    for (const url of ROUTES) {
      it(`finds no violations on ${url} in the ${theme} theme`, async () => {
        // The system theme is the opposite one, so the page shows `theme`
        // after a toggle, exactly as for a visitor who switched.
        stubSystemTheme(theme === 'light');
        localStorage.removeItem(CONSENT_STORAGE_KEY);
        const lang = langFromPath(url);
        TestBed.configureTestingModule({
          imports: [App],
          providers: [provideRouter(routes), provideLang(lang)],
        });
        await whenI18nReady();
        const fixture = TestBed.createComponent(App);
        await TestBed.inject(Router).navigateByUrl(url);
        const themes = TestBed.inject(ThemeService);
        themes.toggle();
        fixture.detectChanges();
        await renderDeferBlocks(fixture);
        await fixture.whenStable();
        expect(themes.theme()).toBe(theme);
        if (url === '/' || url === '/de') {
          expect(
            document.querySelector('#contact form'),
            'the deferred inquiry form',
          ).not.toBeNull();
        }
        expect(document.querySelector('[role="dialog"]'), 'the consent dialog').not.toBeNull();
        expect(document.documentElement.lang).toBe(lang);

        const results = await axe.run(document, OPTIONS);

        expect(describeViolations(results.violations)).toEqual([]);
      });
    }
  }
});
