import { ComponentFixture, TestBed } from '@angular/core/testing';

import { STATS } from '../../testing/project-stats';
import { LANDING_CONTENT_EN, LINKS } from '../content/landing.content';
import { ProjectApi, ProjectStats } from '../core/project-api';
import { GithubStrip, timeAgo } from './github-strip';

const project = LANDING_CONTENT_EN.getStarted.project;
const NOW = Date.parse('2026-09-28T12:00:00Z');

describe('GithubStrip', () => {
  let fixture: ComponentFixture<GithubStrip>;
  let host: HTMLElement;
  let api: { stats: ReturnType<typeof vi.fn> };

  async function render(stats: ProjectStats | undefined): Promise<void> {
    api = { stats: vi.fn(async () => stats) };
    TestBed.configureTestingModule({ providers: [{ provide: ProjectApi, useValue: api }] });
    fixture = TestBed.createComponent(GithubStrip);
    host = fixture.nativeElement as HTMLElement;
    await fixture.whenStable();
    await fixture.whenStable();
  }

  const value = (term: string) =>
    Array.from(host.querySelectorAll('.stats dl > div'))
      .find((item) => item.querySelector('dt')?.textContent?.trim() === term)
      ?.querySelector('dd');

  beforeEach(() => vi.useFakeTimers({ now: NOW, toFake: ['Date'] }));
  afterEach(() => vi.useRealTimers());

  it('links to the repository, the quick start, the spec and good first issues', async () => {
    await render(undefined);

    const list = host.querySelector('ul.links');
    expect(list?.getAttribute('aria-label')).toBe('OBIE on GitHub');
    const links = Array.from(list?.querySelectorAll('a') ?? []);
    expect(links.map((a) => [a.textContent?.trim(), a.getAttribute('href')])).toEqual([
      ['Repository', LINKS.repository],
      ['Quick start', LINKS.quickStart],
      ['Protocol specification', LINKS.spec],
      ['Good first issues', LINKS.goodFirstIssues],
    ]);
    expect(links.every((a) => a.getAttribute('rel') === 'noopener')).toBe(true);
    expect(LINKS.goodFirstIssues).toContain('label%3A%22good%20first%20issue%22');
  });

  it('shows stars, the latest release and the last activity', async () => {
    await render(STATS);

    expect(api.stats).toHaveBeenCalledTimes(1);
    expect(host.querySelector('.stats .caption')?.textContent).toBe(project.statsCaption);
    expect(value('Stars')?.textContent?.trim()).toBe('1,234');

    const release = value('Latest release');
    const tag = release?.querySelector('a');
    expect(tag?.textContent).toBe('v0.1.0');
    expect(tag?.getAttribute('href')).toBe(STATS.latestRelease?.url);
    expect(tag?.getAttribute('rel')).toBe('noopener');
    expect(release?.querySelector('time')?.getAttribute('datetime')).toBe('2026-09-01T12:00:00Z');
    expect(release?.querySelector('time')?.textContent).toBe(
      new Intl.DateTimeFormat('en-GB', { dateStyle: 'medium' }).format(
        new Date('2026-09-01T12:00:00Z'),
      ),
    );

    const lastCommit = value('Last commit')?.querySelector('time');
    expect(lastCommit?.getAttribute('datetime')).toBe(STATS.lastCommitAt);
    expect(lastCommit?.textContent?.trim()).toBe('8 days ago');
  });

  it('says so when there is no release yet', async () => {
    await render({ ...STATS, latestRelease: null });

    expect(value('Latest release')?.textContent?.trim()).toBe('None yet');
    expect(value('Latest release')?.querySelector('a')).toBeNull();
  });

  it('leaves the stats out while they are unavailable, but keeps the links', async () => {
    await render(undefined);

    expect(api.stats).toHaveBeenCalledTimes(1);
    expect(host.querySelector('.stats')).toBeNull();
    expect(host.textContent).not.toContain(project.stars);
    expect(host.querySelectorAll('ul.links a').length).toBe(4);
  });
});

describe('timeAgo', () => {
  const ago = (iso: string) => timeAgo(iso, NOW, 'en-GB');

  it('counts whole days within a month', () => {
    expect(ago('2026-09-28T01:00:00Z')).toBe('today');
    expect(ago('2026-09-27T11:00:00Z')).toBe('yesterday');
    expect(ago('2026-09-20T10:30:00Z')).toBe('8 days ago');
  });

  it('counts months, then years', () => {
    expect(ago('2026-07-01T12:00:00Z')).toBe('2 months ago');
    expect(ago('2024-09-01T12:00:00Z')).toBe('2 years ago');
  });

  it('never talks about the future', () => {
    expect(ago('2026-09-29T12:00:00Z')).toBe('today');
  });
});
