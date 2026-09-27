import { TestBed } from '@angular/core/testing';

import { LANDING_CONTENT_EN } from '../content/landing.content';
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

  it('links to every section by its anchor, in page order', () => {
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

  it('links the brand back to the top of the page', () => {
    const brand = header.querySelector('.brand');
    expect(brand?.getAttribute('href')).toBe('#top');
    expect(header.querySelector('#top')).not.toBeNull();
    expect(brand?.getAttribute('aria-label')).toBe('OBIE, back to the top of the page');
  });

  it('shows the GitHub button', () => {
    const github = header.querySelector('a.github');
    expect(github?.getAttribute('href')).toBe(LANDING_CONTENT_EN.header.github.href);
    expect(github?.textContent?.trim()).toBe('GitHub');
  });

  it('contains the theme toggle', () => {
    expect(header.querySelector('app-theme-toggle button')).not.toBeNull();
  });
});
