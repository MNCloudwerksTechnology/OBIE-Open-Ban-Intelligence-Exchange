import { TestBed } from '@angular/core/testing';

import { CONTACT_ID, LANDING_CONTENT_EN, REPOSITORY_URL } from '../content/landing.content';
import { stubSystemTheme } from '../../testing/system-theme';
import { SiteHeader } from './site-header';

describe('SiteHeader', () => {
  let header: HTMLElement;

  beforeEach(() => {
    stubSystemTheme(false);
    const fixture = TestBed.createComponent(SiteHeader);
    fixture.detectChanges();
    header = fixture.nativeElement as HTMLElement;
  });

  afterEach(() => vi.unstubAllGlobals());

  it('is the banner landmark with a labelled navigation', () => {
    expect(header.querySelector('header')).not.toBeNull();
    expect(header.querySelector('nav')?.getAttribute('aria-label')).toBe('Sections of this page');
  });

  it('links to every section by its anchor, in page order, and to nothing else', () => {
    const links = Array.from(header.querySelectorAll('nav a'));
    const c = LANDING_CONTENT_EN;
    expect(links.map((link) => link.getAttribute('href'))).toEqual(
      [c.problem, c.howItWorks, c.principles, c.status, c.getStarted, c.faq].map(
        (section) => `#${section.id}`,
      ),
    );
    expect(links.map((link) => link.textContent?.trim())).toEqual([
      'The problem',
      'How it works',
      'Principles',
      'Status',
      'Get started',
      'FAQ',
    ]);
  });

  it('does not offer the invitation to speak', () => {
    expect(header.textContent).not.toContain(LANDING_CONTENT_EN.founder.invite.label);
    expect(header.querySelector(`a[href="#${CONTACT_ID}"]`)).toBeNull();
  });

  it('links the brand back to the top of the page', () => {
    const brand = header.querySelector('.brand');
    expect(brand?.getAttribute('href')).toBe('#top');
    expect(header.querySelector('#top')).not.toBeNull();
    expect(brand?.getAttribute('aria-label')).toBe('OBIE, back to the top of the page');
  });

  it('shows the "View on GitHub" button', () => {
    const github = header.querySelector('a.github');
    expect(github?.getAttribute('href')).toBe(REPOSITORY_URL);
    expect(github?.textContent?.trim()).toBe('View on GitHub');
    expect(github?.getAttribute('rel')).toBe('noopener');
  });

  it('keeps "View on GitHub" its only button, in the primary style', () => {
    const buttons = Array.from(header.querySelectorAll('a.button'));
    expect(buttons).toEqual([header.querySelector('a.github')]);
    expect(buttons[0].classList).toContain('button--primary');
  });

  it('reaches its controls in reading order, each with an accessible name', () => {
    const controls = Array.from(header.querySelectorAll<HTMLElement>('a[href], button'));
    expect(controls.every((control) => control.tabIndex === 0)).toBe(true);
    expect(
      controls.map((control) => control.getAttribute('aria-label') ?? control.textContent?.trim()),
    ).toEqual([
      'OBIE, back to the top of the page',
      'The problem',
      'How it works',
      'Principles',
      'Status',
      'Get started',
      'FAQ',
      'Deutsch',
      'Switch to dark theme',
      'View on GitHub',
    ]);
  });

  it('links to this page in German, labelled in German', () => {
    const link = header.querySelector<HTMLAnchorElement>('app-language-switch a');
    expect(link?.getAttribute('href')).toBe('/de');
    expect(link?.getAttribute('hreflang')).toBe('de');
    expect(link?.getAttribute('lang')).toBe('de');
    expect(link?.textContent?.trim()).toBe('DE');
    expect(link?.getAttribute('aria-label')).toBe('Deutsch');
  });

  it('contains the theme toggle', () => {
    expect(header.querySelector('app-theme-toggle button')).not.toBeNull();
  });
});
