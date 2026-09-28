import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';

import { routes } from '../../app.routes';
import { LegalContent } from '../../content/legal-content.model';
import { LEGAL_CONTENT, LEGAL_CONTENT_EN, OPERATOR_EMAIL } from '../../content/legal.content';

async function render(url: string, content: LegalContent = LEGAL_CONTENT_EN): Promise<HTMLElement> {
  TestBed.configureTestingModule({
    providers: [provideRouter(routes), { provide: LEGAL_CONTENT, useValue: content }],
  });
  const harness = await RouterTestingHarness.create(url);
  return harness.routeNativeElement as HTMLElement;
}

/** Text with runs of whitespace collapsed, as a reader sees it. */
function visibleText(element: Element | null | undefined): string {
  return (element?.textContent ?? '').replace(/\s+/g, ' ').trim();
}

describe('Legal pages', () => {
  describe('/impressum', () => {
    let page: HTMLElement;

    beforeEach(async () => {
      page = await render('/impressum');
    });

    it('shows the German term "Impressum" alongside the English heading', () => {
      const h1 = page.querySelector('h1');
      expect(visibleText(h1)).toBe('Impressum Legal notice');
      expect(h1?.querySelector('[lang="de"]')?.textContent).toBe('Impressum');
    });

    it('labels each section with its heading and the German legal term', () => {
      const sections = Array.from(page.querySelectorAll('section[id]'));
      expect(sections.map((section) => section.id)).toEqual(
        LEGAL_CONTENT_EN.impressum.sections.map((section) => section.id),
      );
      const provider = page.querySelector('#provider-heading');
      expect(visibleText(provider)).toBe('Information under § 5 DDG Angaben gemäß § 5 DDG');
      for (const section of sections) {
        const heading = page.querySelector(`#${section.getAttribute('aria-labelledby')}`);
        expect(heading?.tagName).toBe('H2');
      }
    });

    it('lists the provider data as terms and values, contact details as links', () => {
      const facts = Array.from(page.querySelectorAll('#provider .fact')).map((fact) => [
        visibleText(fact.querySelector('dt')),
        Array.from(fact.querySelectorAll('dd .line')).map((line) => line.textContent),
      ]);
      expect(facts).toEqual([
        ['Provider', ['Cloudwerks Technology GmbH']],
        ['Address', ['Pottenort 15', '45891 Gelsenkirchen', 'Germany']],
        ['Represented by', ['Markus Niewerth, managing director']],
      ]);
      const email = page.querySelector<HTMLAnchorElement>('#contact dd a[href^="mailto:"]');
      expect(email?.getAttribute('href')).toBe(`mailto:${OPERATOR_EMAIL}`);
      expect(email?.textContent).toBe(OPERATOR_EMAIL);
    });

    it('renders a paragraph link without stray spaces around it', () => {
      const paragraph = page.querySelector('#copyright p');
      expect(paragraph?.textContent).toContain(
        'published under the MIT licence, which allows their use',
      );
      expect(paragraph?.querySelector('a')?.textContent).toBe('MIT licence');
    });

    it('sets the document title and description', () => {
      expect(document.title).toBe(LEGAL_CONTENT_EN.impressum.meta.title);
      expect(document.querySelector('meta[name="description"]')?.getAttribute('content')).toBe(
        LEGAL_CONTENT_EN.impressum.meta.description,
      );
    });
  });

  describe('/privacy', () => {
    let page: HTMLElement;

    beforeEach(async () => {
      page = await render('/privacy');
    });

    it('shows the German term "Datenschutzerklärung" alongside the English heading', () => {
      expect(visibleText(page.querySelector('h1'))).toBe('Datenschutzerklärung Privacy policy');
      expect(page.querySelector('h1 [lang="de"]')?.textContent).toBe('Datenschutzerklärung');
      expect(page.querySelector('.updated')?.textContent).toBe(LEGAL_CONTENT_EN.privacy.updated);
    });

    it('states that there is no cookie banner because there are no cookies', () => {
      const summary = visibleText(page.querySelector('#summary'));
      expect(summary).toContain('No cookies.');
      expect(summary).toContain('this website shows no cookie banner');
    });

    it('offers the privacy contact as an e-mail link', () => {
      const link = page.querySelector('#privacy-contact a');
      expect(link?.getAttribute('href')).toBe(`mailto:${OPERATOR_EMAIL}`);
    });

    it('has one h1 and never skips a heading level', () => {
      const levels = Array.from(page.querySelectorAll('h1, h2, h3')).map((h) =>
        Number(h.tagName[1]),
      );
      expect(levels[0]).toBe(1);
      expect(levels.filter((level) => level === 1).length).toBe(1);
      levels.slice(1).forEach((level, i) => expect(level - levels[i]).toBeLessThanOrEqual(1));
    });
  });

  describe('review notice', () => {
    for (const url of ['/impressum', '/privacy']) {
      it(`opens ${url} with the notice while the review is pending`, async () => {
        const page = await render(url, { ...LEGAL_CONTENT_EN, reviewPending: true });
        const notice = page.querySelector('[role="note"]');
        expect(notice).not.toBeNull();
        expect(page.querySelector('article')?.firstElementChild).toBe(notice);
        expect(visibleText(notice)).toContain('must be reviewed by the operator');
      });

      it(`removes the notice from ${url} once the flag is cleared`, async () => {
        const page = await render(url, { ...LEGAL_CONTENT_EN, reviewPending: false });
        expect(page.querySelector('[role="note"]')).toBeNull();
        expect(page.textContent).not.toContain(LEGAL_CONTENT_EN.reviewNotice.heading);
      });
    }
  });
});
