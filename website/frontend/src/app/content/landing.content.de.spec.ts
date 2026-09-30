import { PAGE_PATHS, sectionHref } from '../i18n/languages';
import { DEMO_SETTINGS, NODE_DEFAULTS } from '../sections/mesh-demo/scenario';
import {
  allStrings,
  leaves,
  placeholders,
  shapeDifferences,
  words,
} from '../../testing/translation';
import { LANDING_CONTENT_DE } from './landing.content.de';
import { CONTACT_ID, LANDING_CONTENT_EN, LINKS } from './landing.content';

const en = LANDING_CONTENT_EN;
const de = LANDING_CONTENT_DE;

/** The German counterpart of an English link: sections and legal pages move under /de. */
function germanHref(href: string): string {
  if (href.startsWith('#')) {
    return sectionHref('de', href.slice(1));
  }
  if (href === LINKS.impressum) {
    return PAGE_PATHS.impressum.de;
  }
  if (href === LINKS.privacy) {
    return PAGE_PATHS.privacy.de;
  }
  return href;
}

/** Leaves whose values are data, not copy: they stay as they are in every language. */
const KEPT =
  /\.(id|state|code|key)$|\.types\.\d+\.value$|\.(servers\.\w+|rogue)\.short$|\.photo\.src$/;

/** Leaves not checked for placeholders: the kept ones and short labels. */
const UNTRANSLATED = new RegExp(`${KEPT.source}|\\.short$|\\.founder\\.name$`);

describe('German landing page content', () => {
  const englishLeaves = new Map(leaves(en));

  it('has exactly the shape of the English copy', () => {
    expect(shapeDifferences(en, de)).toEqual([]);
  });

  it('translates the copy but keeps ids, values, code and the server letters', () => {
    for (const [path, value] of leaves(de)) {
      if (KEPT.test(path)) {
        expect(value, path).toBe(englishLeaves.get(path));
      }
    }
    expect(de.founder.name).toBe(en.founder.name);
    expect(de.founder.photo).toEqual(en.founder.photo);
    expect(de.founder.links).toEqual(en.founder.links);
  });

  it('links to the German pages and sections, and to the same external pages', () => {
    for (const [path, value] of leaves(de)) {
      if (path.endsWith('.href')) {
        expect(value, path).toBe(germanHref(englishLeaves.get(path) as string));
      }
    }
    expect(de.founder.invite.href).toBe(sectionHref('de', CONTACT_ID));
    expect(de.footer.links).toContainEqual(de.founder.invite);
    expect(de.contact.form.consent.link.href).toBe(PAGE_PATHS.privacy.de);
  });

  it('keeps every placeholder of every string', () => {
    for (const [path, value] of leaves(de)) {
      if (typeof value === 'string' && !UNTRANSLATED.test(path)) {
        expect(placeholders(value), path).toEqual(placeholders(englishLeaves.get(path) as string));
      }
    }
  });

  it('formats numbers and dates the German way and switches back to English in English', () => {
    expect(de.meta.locale).toBe('de-DE');
    expect(de.header.language).toEqual({ label: 'EN', name: 'English' });
    expect(en.header.language).toEqual({ label: 'DE', name: 'Deutsch' });
  });

  describe('three-node demo', () => {
    const demo = de.howItWorks.demo;
    const german = (value: number) =>
      value.toLocaleString('de-DE', { minimumFractionDigits: 1, maximumFractionDigits: 1 });

    it('keeps every caption to at most 40 words', () => {
      for (const step of demo.steps) {
        expect(words(step.caption), step.title).toBeLessThanOrEqual(40);
      }
    });

    it('says it is an illustration, not live data', () => {
      expect(demo.illustration).toMatch(/^Illustration\b/);
      expect(demo.illustration).toMatch(/keine Live-Daten|keine echten Daten|nicht live/i);
    });

    it('names the settings it uses, in German number format', () => {
      expect(demo.settings).toContain(german(DEMO_SETTINGS.threshold));
      expect(demo.settings).toContain(german(NODE_DEFAULTS.threshold));
      expect(demo.settings).toContain(german(NODE_DEFAULTS.fail2banConfidence));
      expect(demo.settings).toMatch(/mindestens zwei/);
      expect(demo.settings).toMatch(/Beobachtungsmodus/);
    });

    it('marks every later feature it mentions as planned', () => {
      for (const step of demo.steps) {
        if (step.planned) {
          expect(step.planned, step.title).toMatch(/geplant/);
        }
      }
      expect(demo.labels.planned).toBe('Geplant');
    });

    it('names the payment service on the safety list of server A', () => {
      expect(demo.servers.a.safetyList.toLowerCase()).toContain(
        demo.subjects.payment.name.toLowerCase(),
      );
    });
  });

  it('introduces the founder with the operator-supplied facts', () => {
    expect(de.founder.role.startsWith('Gründer von OBIE')).toBe(true);
    expect(words(de.founder.bio)).toBeLessThanOrEqual(80);
    expect(de.founder.topicsHeading).toMatch(/vorgeschlagen/i);
  });

  it('keeps the company name out of all visible copy but the footer and the founder profile', () => {
    const { footer, founder, ...rest } = de;
    const { bio, role, ...founderRest } = founder;
    expect([bio, role, footer.licence].every((text) => text.includes('Cloudwerks'))).toBe(true);
    const visible = allStrings([rest, founderRest]).filter((text) => !text.startsWith('https://'));
    expect(visible.filter((text) => /cloudwerks/i.test(text))).toEqual([]);
  });

  it('avoids hype words and political vocabulary (operator voice rules)', () => {
    const banned =
      /revolution|bahnbrechend|weltklasse|ki-gestützt|\bsofort\b|\bbis zu\b|polizei|autoritär|regime|regierung/i;
    expect(allStrings(de).filter((text) => banned.test(text))).toEqual([]);
  });

  it('keeps the booking copy non-commercial (operator rule)', () => {
    const selling = /beratung|kostenlos|termin|preis|angebot|honorar/i;
    const copy = allStrings([de.founder, de.contact, de.header]);
    expect(copy.filter((text) => selling.test(text))).toEqual([]);
  });
});
