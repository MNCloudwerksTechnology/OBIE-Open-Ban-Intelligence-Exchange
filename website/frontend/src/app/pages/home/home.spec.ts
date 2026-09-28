import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';

import { App } from '../../app';
import { routes } from '../../app.routes';
import { LANDING_CONTENT_EN, REPOSITORY_URL } from '../../content/landing.content';

describe('Home page', () => {
  let page: HTMLElement;

  beforeEach(async () => {
    TestBed.configureTestingModule({ imports: [App], providers: [provideRouter(routes)] });
    const fixture = TestBed.createComponent(App);
    await TestBed.inject(Router).navigateByUrl('/');
    fixture.detectChanges();
    page = fixture.nativeElement as HTMLElement;
  });

  it('renders the sections in order, each labelled by its heading', () => {
    const sections = Array.from(page.querySelectorAll('main section[id]'));
    const c = LANDING_CONTENT_EN;
    expect(sections.map((section) => section.id)).toEqual(
      [
        c.problem,
        c.howItWorks,
        c.principles,
        c.status,
        c.getStarted,
        c.founder,
        c.contact,
        c.faq,
      ].map((section) => section.id),
    );
    for (const section of sections) {
      const heading = page.querySelector(`#${section.getAttribute('aria-labelledby')}`);
      expect(heading?.tagName).toBe('H2');
    }
  });

  it('points every navigation link at a section on the page', () => {
    const links = Array.from(
      page.querySelectorAll<HTMLAnchorElement>('nav[aria-label="Sections of this page"] li a'),
    );
    expect(links.length).toBe(6);
    for (const link of links) {
      const target = page.querySelector(link.getAttribute('href') as string);
      expect(target?.tagName, link.getAttribute('href') as string).toBe('SECTION');
      expect(target?.querySelector('h2')).not.toBeNull();
    }
  });

  it('resolves every in-page link to an element on the page', () => {
    const anchors = Array.from(page.querySelectorAll('a[href^="#"]'));
    expect(anchors.length).toBeGreaterThan(6);
    for (const anchor of anchors) {
      const id = (anchor.getAttribute('href') as string).slice(1);
      expect(page.querySelector(`[id="${id}"]`), `#${id}`).not.toBeNull();
    }
  });

  it('links the hero buttons to GitHub and to "How it works"', () => {
    const buttons = Array.from(page.querySelectorAll('main .hero .button'));
    expect(
      buttons.map((button) => [button.textContent?.trim(), button.getAttribute('href')]),
    ).toEqual([
      ['View on GitHub', REPOSITORY_URL],
      ['How it works', '#how-it-works'],
    ]);
    expect(buttons[0].classList).toContain('button--primary');
  });

  it('ends every section with a next step to GitHub', () => {
    for (const section of Array.from(page.querySelectorAll('main section[id]'))) {
      const links = section.querySelectorAll(':scope > .container > a.next-step');
      expect(links.length, section.id).toBe(1);
      expect(links[0].getAttribute('href')?.startsWith(REPOSITORY_URL), section.id).toBe(true);
    }
  });

  it('has one h1 and never skips a heading level', () => {
    const headings = Array.from(page.querySelectorAll('h1, h2, h3')).map((h) =>
      Number(h.tagName[1]),
    );
    expect(headings.filter((level) => level === 1).length).toBe(1);
    expect(headings[0]).toBe(1);
    headings.slice(1).forEach((level, i) => expect(level - headings[i]).toBeLessThanOrEqual(1));
  });

  it('describes the diagram for screen readers', () => {
    const svg = page.querySelector('svg[role="img"]');
    expect(svg?.getAttribute('aria-labelledby')).toBe('flow-title flow-desc');
    expect(page.querySelector('#flow-title')?.textContent).toBe(
      LANDING_CONTENT_EN.howItWorks.diagram.title,
    );
  });

  it('introduces the founder and links the invitation to the inquiry form', () => {
    const founder = page.querySelector('#founder');
    expect(founder?.querySelector('h3')?.textContent).toBe('Markus Niewerth');
    expect(founder?.querySelector('a[href="#contact"]')?.textContent?.trim()).toBe(
      'Invite Markus to speak',
    );
    expect(page.querySelector('section#contact form')).not.toBeNull();
  });

  it('asks the FAQ as keyboard-operable disclosure widgets', () => {
    const questions = page.querySelectorAll('#faq details > summary');
    expect(questions.length).toBe(LANDING_CONTENT_EN.faq.items.length);
  });

  it('sets the document title and description from the content file', () => {
    expect(document.title).toBe(LANDING_CONTENT_EN.meta.title);
    expect(document.querySelector('meta[name="description"]')?.getAttribute('content')).toBe(
      LANDING_CONTENT_EN.meta.description,
    );
  });

  it('keeps all copy in the content file', () => {
    const text = page.textContent ?? '';
    for (const card of LANDING_CONTENT_EN.principles.cards) {
      expect(text).toContain(card.title);
    }
  });
});
