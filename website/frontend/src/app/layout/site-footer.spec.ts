import { TestBed } from '@angular/core/testing';

import { CONSENT_CONTENT_EN } from '../content/consent.content';
import { CONTACT_ID, LANDING_CONTENT_EN, REPOSITORY_URL } from '../content/landing.content';
import { ConsentService } from '../core/analytics/consent.service';
import { renderPrerendered } from '../../testing/prerender';
import { SiteFooter } from './site-footer';

describe('SiteFooter', () => {
  let footer: HTMLElement;

  beforeEach(async () => {
    const fixture = TestBed.createComponent(SiteFooter);
    fixture.detectChanges();
    await fixture.whenStable();
    footer = fixture.nativeElement as HTMLElement;
  });

  it('reopens the question about visitor statistics from the privacy settings', () => {
    const settings = footer.querySelector<HTMLButtonElement>('nav button');
    expect(settings?.textContent?.trim()).toBe(CONSENT_CONTENT_EN.settings);
    const consent = TestBed.inject(ConsentService);
    const open = vi.spyOn(consent, 'openSettings');

    settings?.click();

    expect(open).toHaveBeenCalledWith(settings);
    expect(consent.dialogOpen()).toBe(true);
  });

  it('leaves the privacy settings out of the prerendered page, where they could not work', async () => {
    TestBed.resetTestingModule();
    const page = await renderPrerendered(SiteFooter);
    expect(page.querySelector('button')).toBeNull();
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
