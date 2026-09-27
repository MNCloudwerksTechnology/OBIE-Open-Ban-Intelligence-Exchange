import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

import { LandingContent } from './landing-content.model';
import {
  CONTACT_ID,
  FOUNDER_AVATAR_PLACEHOLDER,
  LANDING_CONTENT_EN,
  LINKS,
  REPOSITORY_URL,
} from './landing.content';

/** Every string in the content object, depth first. */
function allStrings(value: unknown): string[] {
  if (typeof value === 'string') {
    return [value];
  }
  if (value && typeof value === 'object') {
    return Object.values(value).flatMap(allStrings);
  }
  return [];
}

const content: LandingContent = LANDING_CONTENT_EN;
const sections = [
  content.problem,
  content.howItWorks,
  content.principles,
  content.status,
  content.getStarted,
  content.founder,
  content.contact,
  content.faq,
];

describe('Landing page content', () => {
  it('points to the public GitHub repository', () => {
    expect(REPOSITORY_URL).toBe(
      'https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange',
    );
    expect(content.hero.primary).toEqual({ label: 'View on GitHub', href: REPOSITORY_URL });
    expect(content.header.github.href).toBe(REPOSITORY_URL);
  });

  it('gives every section a unique anchor id', () => {
    const ids = sections.map((section) => section.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ids) {
      expect(id).toMatch(/^[a-z][a-z-]*$/);
    }
  });

  it('ends every section with a next step to GitHub', () => {
    for (const section of sections) {
      expect(section.nextStep.href.startsWith(REPOSITORY_URL)).toBe(true);
    }
  });

  it('links the hero secondary button to the how-it-works section', () => {
    expect(content.hero.secondary.href).toBe(`#${content.howItWorks.id}`);
  });

  it('tells the v0.1 flow in five or six steps', () => {
    expect(content.howItWorks.steps.length).toBeGreaterThanOrEqual(5);
    expect(content.howItWorks.steps.length).toBeLessThanOrEqual(6);
  });

  it('keeps the diagram within 12 labels', () => {
    const labels = Object.entries(content.howItWorks.diagram).filter(
      ([key]) => key !== 'title' && key !== 'description',
    );
    expect(labels.length).toBeLessThanOrEqual(12);
  });

  it('condenses the manifesto into five or six principles', () => {
    expect(content.principles.cards.length).toBeGreaterThanOrEqual(5);
    expect(content.principles.cards.length).toBeLessThanOrEqual(6);
  });

  it('labels the status honestly as available, in progress and planned', () => {
    expect(content.status.groups.map((group) => group.state)).toEqual([
      'available',
      'in-progress',
      'planned',
    ]);
    const planned = content.status.groups.find((group) => group.state === 'planned');
    expect(planned?.items.map((item) => item.title)).toEqual([
      'Automatic peer discovery',
      'Earned reputation',
      'Diversity checks',
      'Appeals',
      'eBPF blocking',
    ]);
  });

  it('teases getting started in three steps', () => {
    expect(content.getStarted.steps.map((step) => step.title)).toEqual([
      'Install',
      'Observe only',
      'Connect peers',
    ]);
  });

  it('asks five to seven FAQ questions, including the required ones', () => {
    const questions = content.faq.items.map((item) => item.question);
    expect(questions.length).toBeGreaterThanOrEqual(5);
    expect(questions.length).toBeLessThanOrEqual(7);
    for (const required of [
      'Is it free?',
      'What data leaves my server?',
      'Do I need Fail2Ban?',
      'Is it production-ready?',
      'Who is behind it?',
    ]) {
      expect(questions).toContain(required);
    }
  });

  it('links the footer to GitHub, spec, security policy, legal pages and licence', () => {
    expect(content.footer.links.map((link) => link.href)).toEqual([
      LINKS.repository,
      LINKS.spec,
      LINKS.securityPolicy,
      LINKS.impressum,
      LINKS.privacy,
      LINKS.licence,
    ]);
    expect(content.footer.attribution).toEqual({
      text: 'An open protocol initiated by Cloudwerks Technology GmbH.',
      href: 'https://cloudwerks.de',
    });
  });

  it('keeps the company name out of all visible copy but the footer and the founder profile', () => {
    const { footer, founder, ...rest } = content;
    expect(footer.licence).toContain('Cloudwerks');
    // The operator approved the founder's bio and role line as they are.
    const { bio, role, ...founderRest } = founder;
    expect([bio, role].every((text) => text.includes('Cloudwerks'))).toBe(true);
    // URLs are exempt: the GitHub organisation carries the company name.
    const visible = allStrings([rest, founderRest]).filter((text) => !text.startsWith('https://'));
    expect(visible.filter((text) => /cloudwerks/i.test(text))).toEqual([]);
  });

  it('avoids hype words and political vocabulary (operator voice rules)', () => {
    const banned =
      /revolutionary|game-changing|next-generation|world-class|ai-powered|instantly|\bup to\b|police|authoritarian|regime|government/i;
    expect(allStrings(content).filter((text) => banned.test(text))).toEqual([]);
  });

  it('uses only https links or links within this site', () => {
    for (const text of allStrings(content)) {
      if (/^(https?:|\/|#)/.test(text) && !text.includes(' ')) {
        expect(text).toMatch(/^(https:\/\/|\/[a-z]|#[a-z])/);
      }
    }
  });

  it('introduces the founder with the operator-supplied facts', () => {
    const { founder } = content;
    expect(founder.name).toBe('Markus Niewerth');
    expect(founder.role.startsWith('Founder of OBIE')).toBe(true);
    expect(founder.bio.split(/\s+/).length).toBeLessThanOrEqual(80);
    expect(founder.topics.length).toBeGreaterThanOrEqual(3);
    expect(founder.topics.length).toBeLessThanOrEqual(5);
    expect(founder.topicsHeading).toMatch(/proposed/i);
    expect(founder.links.map((link) => link.href)).toEqual([
      'https://www.linkedin.com/in/niewerth/',
      'https://github.com/MNCloudwerksTechnology',
    ]);
  });

  it('keeps the placeholder avatar out of the accessibility tree until a photo exists', () => {
    const { photo } = content.founder;
    expect(existsSync(resolve(process.cwd(), 'public', photo.src.slice(1))), photo.src).toBe(true);
    if (photo.src === FOUNDER_AVATAR_PLACEHOLDER) {
      expect(photo.alt).toBe('');
    } else {
      expect(photo.alt).not.toBe('');
    }
  });

  it('opens the inquiry form from the header and the founder section', () => {
    expect(content.contact.id).toBe(CONTACT_ID);
    expect(content.header.invite.href).toBe(`#${CONTACT_ID}`);
    expect(content.founder.invite.href).toBe(`#${CONTACT_ID}`);
  });

  it('offers exactly the inquiry types the back end accepts', () => {
    expect(content.contact.form.types.map((type) => type.value)).toEqual([
      'talk',
      'workshop',
      'interview',
      'collaboration',
      'other',
    ]);
  });

  it('keeps the booking copy non-commercial (operator rule)', () => {
    const selling = /consult|free call|book a call|pricing|price|termin|quote|offer/i;
    const copy = allStrings([content.founder, content.contact, content.header]);
    expect(copy.filter((text) => selling.test(text))).toEqual([]);
  });

  it('links the consent checkbox to the privacy page', () => {
    expect(content.contact.form.consent.link.href).toBe(LINKS.privacy);
  });

  it('links only to repository files that exist', () => {
    const repoRoot = resolve(process.cwd(), '../..');
    const prefix = `${REPOSITORY_URL}/blob/develop/`;
    const files = Object.values(LINKS)
      .filter((href) => href.startsWith(prefix))
      .map((href) => href.slice(prefix.length).split('#')[0]);
    expect(files).toContain('SECURITY.md');
    for (const file of files) {
      expect(existsSync(resolve(repoRoot, file)), file).toBe(true);
    }
  });
});
