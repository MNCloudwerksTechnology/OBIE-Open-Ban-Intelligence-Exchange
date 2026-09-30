import { ANALYTICS_HOST, CONSENT_STORAGE_KEY } from '../core/analytics/analytics.config';
import { allStrings, leaves } from '../../testing/translation';
import { ANALYTICS_SECTION_ID, CONSENT_CONTENT_EN } from './consent.content';
import { CONSENT_CONTENT_DE } from './consent.content.de';
import { LegalBlock, LegalPageContent } from './legal-content.model';
import {
  LEGAL_CONTENT_DE,
  INQUIRY_RETENTION_DE,
  SERVER_LOG_RETENTION_DE,
} from './legal.content.de';
import { LEGAL_CONTENT_EN } from './legal.content';
import {
  ANALYTICS_RAW_DATA_RETENTION_MONTHS,
  INQUIRY_MAILBOX_RETENTION_YEARS,
  INQUIRY_RETENTION_MONTHS,
  SERVER_LOG_RETENTION_DAYS,
} from './operator';

/** The shape of a block: its kind, list and fact lengths, and whether it has a link. */
function blockShape(block: LegalBlock): string {
  switch (block.kind) {
    case 'paragraph':
      return `paragraph${block.link ? '+link' : ''}`;
    case 'list':
      return `list(${block.items.length})`;
    case 'facts':
      return `facts(${block.items.map((fact) => fact.lines.length).join(',')})`;
  }
}

/** Per section: id, block shapes and the number of TODO(operator) items. */
function outline(page: LegalPageContent): string[] {
  return page.sections.map(
    (section) =>
      `${section.id}: ${section.blocks.map(blockShape).join(' ')}; ` +
      `${allStrings(section).filter((text) => text.includes('TODO(operator)')).length} TODO`,
  );
}

const pages = ['impressum', 'privacy'] as const;

describe('German legal content', () => {
  it('has the sections, blocks and TODO(operator) items of the English pages', () => {
    for (const page of pages) {
      expect(outline(LEGAL_CONTENT_DE[page]), page).toEqual(outline(LEGAL_CONTENT_EN[page]));
    }
    expect(LEGAL_CONTENT_DE.reviewPending).toBe(LEGAL_CONTENT_EN.reviewPending);
  });

  it('uses the German legal terms as headings, so it shows no terms alongside', () => {
    expect(LEGAL_CONTENT_DE.impressum.heading).toBe('Impressum');
    expect(LEGAL_CONTENT_DE.privacy.heading).toBe('Datenschutzerklärung');
    expect(LEGAL_CONTENT_DE.impressum.meta.title).toBe('Impressum · OBIE');
    expect(LEGAL_CONTENT_DE.privacy.meta.title).toBe('Datenschutzerklärung · OBIE');
    for (const page of pages) {
      expect(LEGAL_CONTENT_DE[page].legalTerm).toBeUndefined();
      expect(LEGAL_CONTENT_DE[page].sections.filter((section) => section.legalTerm)).toEqual([]);
      expect(LEGAL_CONTENT_DE[page].meta.description.length).toBeLessThanOrEqual(160);
    }
  });

  it('links exactly where the English links', () => {
    const english = new Map(leaves(LEGAL_CONTENT_EN));
    for (const [path, value] of leaves(LEGAL_CONTENT_DE)) {
      if (path.endsWith('.href')) {
        expect(value, path).toBe(english.get(path));
      }
    }
  });

  it('states the company facts as supplied', () => {
    const text = allStrings(LEGAL_CONTENT_DE.impressum).join('\n');
    for (const fact of [
      'Cloudwerks Technology GmbH',
      'Pottenort 15',
      '45891 Gelsenkirchen',
      'Amtsgericht Gelsenkirchen',
      'HRB 17839',
      'DE363640900',
      '§ 18 Abs. 2 MStV',
    ]) {
      expect(text).toContain(fact);
    }
  });

  describe('Datenschutzerklärung', () => {
    const text = allStrings(LEGAL_CONTENT_DE.privacy).join('\n');
    const section = (id: string) =>
      allStrings(LEGAL_CONTENT_DE.privacy.sections.find((candidate) => candidate.id === id)).join(
        '\n',
      );

    it('cites the DSGVO the German way', () => {
      expect(text).not.toMatch(/GDPR|Art\. 6\(1\)/);
      expect(text).toContain('Art. 6 Abs. 1 lit. f DSGVO');
      expect(text).toContain('Art. 77 DSGVO');
    });

    it('states the retention periods of the deployment', () => {
      expect(INQUIRY_RETENTION_DE).toBe(`${INQUIRY_RETENTION_MONTHS} Monate`);
      expect(SERVER_LOG_RETENTION_DE).toBe(`${SERVER_LOG_RETENTION_DAYS} Tage`);
      expect(text).toContain(`${INQUIRY_RETENTION_DE} nach Eingang automatisch gelöscht`);
      expect(text).toContain(`höchstens ${SERVER_LOG_RETENTION_DE}`);
      expect(text).toContain(
        `längstens ${INQUIRY_MAILBOX_RETENTION_YEARS} Jahre nach dem letzten Kontakt`,
      );
      expect(section(ANALYTICS_SECTION_ID)).toContain(
        `nach ${ANALYTICS_RAW_DATA_RETENTION_MONTHS} Monaten`,
      );
    });

    it('names the stored answer and the statistics server', () => {
      expect(section('cookies')).toContain(CONSENT_STORAGE_KEY);
      expect(section('cookies')).toContain('§ 25 Abs. 2 Nr. 2 TDDDG');
      expect(section(ANALYTICS_SECTION_ID)).toContain(ANALYTICS_HOST);
    });

    it('describes consent and withdrawal with the labels of the German dialog', () => {
      const analytics = section(ANALYTICS_SECTION_ID);
      expect(analytics).toContain('Art. 6 Abs. 1 lit. a DSGVO');
      expect(analytics).toContain('§ 25 Abs. 1 TDDDG');
      expect(analytics).toContain('Art. 7 Abs. 3 DSGVO');
      for (const label of [
        CONSENT_CONTENT_DE.accept,
        CONSENT_CONTENT_DE.decline,
        CONSENT_CONTENT_DE.settings,
      ]) {
        expect(analytics).toContain(`„${label}“`);
      }
    });
  });
});

describe('Consent copy', () => {
  for (const [lang, copy, legal] of [
    ['en', CONSENT_CONTENT_EN, LEGAL_CONTENT_EN],
    ['de', CONSENT_CONTENT_DE, LEGAL_CONTENT_DE],
  ] as const) {
    it(`says in ${lang} who measures, and how to change the answer later`, () => {
      const text = copy.paragraphs.join(' ');
      expect(text).toContain('Matomo');
      expect(text).toContain(ANALYTICS_HOST);
      expect(text).toContain(copy.settings);
      expect(copy.accept).not.toBe('');
      expect(copy.decline).not.toBe('');
    });

    it(`links in ${lang} to the privacy policy's section on visitor statistics`, () => {
      const [path, fragment] = copy.privacyLink.href.split('#');
      expect(fragment).toBe(ANALYTICS_SECTION_ID);
      expect(path).toBe(lang === 'en' ? '/privacy' : '/de/datenschutz');
      expect(legal.privacy.sections.map((section) => section.id)).toContain(ANALYTICS_SECTION_ID);
    });
  }
});
