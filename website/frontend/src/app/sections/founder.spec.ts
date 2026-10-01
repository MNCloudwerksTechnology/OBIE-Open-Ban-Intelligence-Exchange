import { TestBed } from '@angular/core/testing';

import { FOUNDER_AVATAR_PLACEHOLDER, LANDING_CONTENT_EN } from '../content/landing.content';
import { Founder } from './founder';

describe('Founder', () => {
  const founder = LANDING_CONTENT_EN.founder;
  let section: HTMLElement;

  beforeEach(() => {
    const fixture = TestBed.createComponent(Founder);
    fixture.detectChanges();
    section = fixture.nativeElement as HTMLElement;
  });

  it('shows name, role and bio from the content file', () => {
    expect(section.querySelector('h3.name')?.textContent).toBe('Markus Niewerth');
    expect(section.querySelector('.role')?.textContent).toBe(founder.role);
    expect(section.querySelector('.bio')?.textContent).toBe(founder.bio);
  });

  it('shows the placeholder avatar as decoration with fixed dimensions', () => {
    const photo = section.querySelector('img');
    expect(photo?.getAttribute('src')).toBe(FOUNDER_AVATAR_PLACEHOLDER);
    expect(photo?.getAttribute('alt')).toBe('');
    expect(photo?.getAttribute('width')).toBe('240');
    expect(photo?.getAttribute('height')).toBe('240');
  });

  it('lists the talk topics, labelled as proposals', () => {
    const topics = section.querySelector('.topics');
    expect(topics?.querySelector('h3')?.textContent).toBe('Proposed talk topics');
    expect(Array.from(topics?.querySelectorAll('li') ?? []).map((li) => li.textContent)).toEqual(
      founder.topics,
    );
  });

  it('links the profiles in a labelled list', () => {
    const list = section.querySelector('ul.links');
    expect(list?.getAttribute('aria-label')).toBe('Profiles');
    expect(
      Array.from(list?.querySelectorAll('a') ?? []).map((a) => [
        a.textContent,
        a.getAttribute('href'),
      ]),
    ).toEqual(founder.links.map((link) => [link.label, link.href]));
  });

  it('opens the inquiry form with a prominent button', () => {
    const invite = section.querySelector('.invite a');
    expect(invite?.classList).toContain('button--primary');
    expect(invite?.getAttribute('href')).toBe('#contact');
    expect(invite?.textContent?.trim()).toBe('Invite Markus to speak');
  });
});
