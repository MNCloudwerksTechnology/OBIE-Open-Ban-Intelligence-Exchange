import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { IMPRESSUM_PATH, PRIVACY_PATH } from '../app.routes';
import { ANALYTICS_HOST, CONSENT_STORAGE_KEY } from '../core/analytics/analytics.config';
import { ANALYTICS_SECTION_ID, CONSENT_CONTENT_EN } from './consent.content';
import { ANALYTICS_RAW_DATA_RETENTION_MONTHS, INQUIRY_MAILBOX_RETENTION_YEARS } from './operator';
import { LegalPageContent } from './legal-content.model';
import { LINKS } from './landing.content';
import {
  INQUIRY_RETENTION,
  LEGAL_CONTENT_EN,
  OPERATOR_EMAIL,
  SERVER_LOG_RETENTION,
} from './legal.content';

/** Every string in the value, depth first. */
function allStrings(value: unknown): string[] {
  if (typeof value === 'string') {
    return [value];
  }
  if (value && typeof value === 'object') {
    return Object.values(value).flatMap(allStrings);
  }
  return [];
}

/** All text of a page as one string, for "does it say X" checks. */
function textOf(page: LegalPageContent): string {
  return allStrings(page).join('\n');
}

/** Default of an `${ENV:default}` placeholder in the back end's application.properties. */
function backendDefault(variable: string): string | undefined {
  const properties = readFileSync(
    resolve(process.cwd(), '../backend/src/main/resources/application.properties'),
    'utf8',
  );
  return new RegExp(`\\$\\{${variable}:([^}]*)\\}`).exec(properties)?.[1];
}

const { impressum, privacy } = LEGAL_CONTENT_EN;

describe('Legal content', () => {
  it('is linked from the footer and the consent checkbox under the routes that exist', () => {
    expect(LINKS.impressum).toBe(`/${IMPRESSUM_PATH}`);
    expect(LINKS.privacy).toBe(`/${PRIVACY_PATH}`);
  });

  it('shows the German legal terms alongside the English headings', () => {
    expect(impressum.legalTerm).toBe('Impressum');
    expect(privacy.legalTerm).toBe('Datenschutzerklärung');
    expect(impressum.meta.title).toContain('Impressum');
    expect(privacy.meta.title).toContain('Datenschutzerklärung');
  });

  it('gives every section an id that is unique on its page', () => {
    for (const page of [impressum, privacy]) {
      const ids = page.sections.map((section) => section.id);
      expect(new Set(ids).size).toBe(ids.length);
      for (const id of ids) {
        expect(id).toMatch(/^[a-z][a-z-]*$/);
        // Ids the shell and the page template use themselves.
        expect(['main', 'top', 'legal-heading', 'review-notice-heading']).not.toContain(id);
      }
    }
  });

  it('states the review notice for the operator', () => {
    expect(LEGAL_CONTENT_EN.reviewNotice.text).toMatch(/reviewed by the operator/);
  });

  describe('Impressum', () => {
    const text = textOf(impressum);

    it('names the provider, its representative, address and contact (§ 5 DDG)', () => {
      expect(text).toContain('Cloudwerks Technology GmbH');
      expect(text).toContain('Markus Niewerth, managing director');
      expect(text).toContain('Pottenort 15');
      expect(text).toContain('45891 Gelsenkirchen');
      expect(text).toContain(OPERATOR_EMAIL);
      expect(allStrings(impressum)).toContain(`mailto:${OPERATOR_EMAIL}`);
    });

    it('names the register entry, the VAT ID and the person responsible (§ 18 MStV)', () => {
      expect(text).toContain('Amtsgericht Gelsenkirchen');
      expect(text).toContain('HRB 17839');
      expect(text).toContain('DE363640900');
      expect(text).toContain('§ 18 Abs. 2 MStV');
    });

    it('contains the standard sections the operator asked for', () => {
      expect(impressum.sections.map((section) => section.legalTerm)).toEqual(
        expect.arrayContaining([
          'Verbraucherstreitbeilegung',
          'Haftung für Inhalte',
          'Haftung für Links',
          'Urheberrecht',
        ]),
      );
    });
  });

  describe('Privacy policy', () => {
    const text = textOf(privacy);

    it('says there are no cookies, statistics only with consent and no other third-party requests', () => {
      expect(text).toMatch(/sets no cookies/);
      expect(text).toMatch(/Visitor statistics only with your consent/);
      expect(text).toMatch(/No third-party requests/);
      expect(text).toMatch(/no connection to Google Fonts/);
      expect(text).not.toMatch(/no cookie banner|No tracking/);
    });

    it('names the only thing stored in the browser: the answer, under its real key and values', () => {
      const cookies = allStrings(privacy.sections.find((section) => section.id === 'cookies'));
      const stored = cookies.join('\n');
      expect(stored).toContain(CONSENT_STORAGE_KEY);
      expect(stored).toContain('“granted” or “denied”');
      expect(stored).toContain('§ 25(2) no. 2 TDDDG');
    });

    it('describes the visitor statistics: server, data, consent, withdrawal and no cookies', () => {
      const section = privacy.sections.find((candidate) => candidate.id === ANALYTICS_SECTION_ID);
      const analytics = allStrings(section).join('\n');
      expect(analytics).toContain('Matomo');
      expect(analytics).toContain(ANALYTICS_HOST);
      expect(analytics).toContain('Art. 6(1)(a) GDPR');
      expect(analytics).toContain('§ 25(1) TDDDG');
      expect(analytics).toContain('Art. 7(3) GDPR');
      expect(analytics).toMatch(/Matomo sets no cookies/);
      expect(analytics).toMatch(/never its content/);
      expect(analytics).toMatch(/our own statistics server/);
      expect(analytics).toMatch(
        /No data are passed to the maker of Matomo or to any other third party/,
      );
      expect(analytics).toContain(`after ${ANALYTICS_RAW_DATA_RETENTION_MONTHS} months`);
      // No promise the deployment might not keep: the IP address is not said to be shortened.
      expect(analytics).not.toMatch(/shorten|anonymi/i);
      // The labels the visitor sees in the dialog and the footer.
      for (const label of [
        CONSENT_CONTENT_EN.accept,
        CONSENT_CONTENT_EN.decline,
        CONSENT_CONTENT_EN.settings,
      ]) {
        expect(analytics).toContain(`“${label}”`);
      }
    });

    it('describes the server logs and how long they are kept', () => {
      expect(text).toMatch(/IP address of your device/);
      expect(text).toContain(`at most ${SERVER_LOG_RETENTION}`);
      expect(text).toContain('Art. 6(1)(f) GDPR');
    });

    it('describes the inquiry form: fields, legal basis, storage, processor and IP hash', () => {
      for (const field of ['name', 'e-mail address', 'organisation', 'message']) {
        expect(text).toContain(field);
      }
      expect(text).toContain('Art. 6(1)(b) GDPR');
      expect(text).toContain('PostgreSQL');
      expect(text).toMatch(/e-mail \(SMTP\) provider/);
      expect(text).toContain('processor, Art. 28 GDPR');
      expect(text).toMatch(/only as a salted hash/);
    });

    it('states how long the copy of an inquiry stays in the mailbox (cloudwerks.de/datenschutz)', () => {
      expect(text).toContain(
        `at most ${INQUIRY_MAILBOX_RETENTION_YEARS} years after our last contact`,
      );
    });

    it('states the retention the back end deletes inquiries after by default', () => {
      const period = backendDefault('OBIE_INQUIRY_RETENTION');
      const months = /^P(\d+)M$/.exec(period ?? '')?.[1];
      expect(months, `OBIE_INQUIRY_RETENTION default ${period}`).toBeDefined();
      expect(INQUIRY_RETENTION).toBe(`${months} months`);
      expect(text).toContain(`deleted automatically ${INQUIRY_RETENTION} after it was received`);
    });

    it('lists the data subject rights, the authority and the contact for requests', () => {
      for (const article of ['Art. 15', 'Art. 16', 'Art. 17', 'Art. 18', 'Art. 20', 'Art. 21']) {
        expect(text).toContain(article);
      }
      expect(text).toContain('Art. 77 GDPR');
      expect(text).toContain(
        'Landesbeauftragte für Datenschutz und Informationsfreiheit Nordrhein-Westfalen',
      );
      const contact = privacy.sections.find((section) => section.id === 'privacy-contact');
      expect(allStrings(contact)).toContain(`mailto:${OPERATOR_EMAIL}`);
    });
  });

  it('lists every open TODO(operator) item in website/README.md', () => {
    const readme = readFileSync(resolve(process.cwd(), '../README.md'), 'utf8');
    for (const [key, page] of Object.entries({ impressum, privacy })) {
      for (const section of page.sections) {
        if (allStrings(section).some((text) => text.includes('TODO(operator)'))) {
          expect(readme, section.id).toContain(`\`${key}.${section.id}\``);
        }
      }
    }
  });

  it('links only to this site, GitHub, e-mail and phone', () => {
    const hrefs = allStrings(LEGAL_CONTENT_EN).filter((text) =>
      /^(https?:|mailto:|tel:)/.test(text),
    );
    for (const href of hrefs) {
      expect(href).toMatch(/^(https:\/\/github\.com\/|mailto:|tel:\+\d+$)/);
    }
  });
});
