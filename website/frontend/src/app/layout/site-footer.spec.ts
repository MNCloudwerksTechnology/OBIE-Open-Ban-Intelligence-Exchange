import { TestBed } from '@angular/core/testing';

import { CONTACT_ID, LANDING_CONTENT_EN, REPOSITORY_URL } from '../content/landing.content';
import { SiteFooter } from './site-footer';

describe('SiteFooter', () => {
  let footer: HTMLElement;

  beforeEach(() => {
    const fixture = TestBed.createComponent(SiteFooter);
    fixture.detectChanges();
    footer = fixture.nativeElement as HTMLElement;
  });

  it('is the content-info landmark with labelled project links', () => {
    expect(footer.querySelector('footer')).not.toBeNull();
    const links = Array.from(footer.querySelectorAll('nav[aria-label="Project links"] a'));
    expect(links.map((link) => [link.textContent?.trim(), link.getAttribute('href')])).toEqual(
      LANDING_CONTENT_EN.footer.links.map((link) => [link.label, link.href]),
    );
  });

  it('shows the "View on GitHub" button', () => {
    const github = footer.querySelector('footer a.button.github');
    expect(github?.getAttribute('href')).toBe(REPOSITORY_URL);
    expect(github?.textContent?.trim()).toBe('View on GitHub');
    expect(github?.getAttribute('rel')).toBe('noopener');
  });

  it('offers the invitation to speak as a plain link to the inquiry form', () => {
    const invite = footer.querySelector(`nav a[href="#${CONTACT_ID}"]`);
    expect(invite?.textContent?.trim()).toBe('Invite Markus to speak');
    expect(invite?.classList).not.toContain('button');
  });

  it('shows the attribution and the licence', () => {
    const attribution = footer.querySelector('a[href="https://cloudwerks.de"]');
    expect(attribution?.textContent?.trim()).toBe(
      'An open protocol initiated by Cloudwerks Technology GmbH.',
    );
    expect(footer.textContent).toContain('MIT licence · © 2026 Cloudwerks Technology GmbH');
  });
});
