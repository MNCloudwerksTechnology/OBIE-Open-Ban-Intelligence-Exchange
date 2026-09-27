import { LandingContent } from './landing-content.model';
import { LANDING_CONTENT_EN, LINKS, REPOSITORY_URL } from './landing.content';

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

  it('keeps the company name out of all visible copy but the footer', () => {
    const { footer, ...rest } = content;
    expect(footer.licence).toContain('Cloudwerks');
    // URLs are exempt: the GitHub organisation carries the company name.
    const visible = allStrings(rest).filter((text) => !text.startsWith('https://'));
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
});
