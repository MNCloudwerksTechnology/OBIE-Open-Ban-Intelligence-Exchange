import {
  ChangeDetectionStrategy,
  Component,
  afterNextRender,
  computed,
  inject,
  signal,
} from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';
import { ProjectApi, ProjectStats } from '../core/project-api';

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * How long ago `iso` was, e.g. "today", "8 days ago" or "2 months ago";
 * whole days, weeks are not used.
 */
export function timeAgo(iso: string, now: number, locale: string): string {
  const days = Math.max(0, Math.floor((now - Date.parse(iso)) / DAY_MS));
  const format = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  if (days < 30) {
    return format.format(-days, 'day');
  }
  if (days < 365) {
    return format.format(-Math.floor(days / 30), 'month');
  }
  return format.format(-Math.floor(days / 365), 'year');
}

/**
 * Contributor links (repository, quick start, spec, good first issues) and a
 * small strip of live GitHub stats. The stats are fetched from the site's own
 * back end after the first render in the browser, never while prerendering;
 * while they are unavailable the strip is simply not there.
 */
@Component({
  selector: 'app-github-strip',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <ul class="links" [attr.aria-label]="project.linksLabel">
      @for (link of project.links; track link.href) {
        <li>
          <a [href]="link.href" rel="noopener">{{ link.label }}</a>
        </li>
      }
    </ul>
    @if (view(); as view) {
      <div class="stats">
        <p class="caption">{{ project.statsCaption }}</p>
        <dl>
          <div>
            <dt>{{ project.stars }}</dt>
            <dd>{{ view.stars }}</dd>
          </div>
          <div>
            <dt>{{ project.latestRelease }}</dt>
            <dd>
              @if (view.release; as release) {
                <a [href]="release.url" rel="noopener">{{ release.tag }}</a>
                <time [attr.datetime]="release.publishedAt">{{ release.date }}</time>
              } @else {
                {{ project.noRelease }}
              }
            </dd>
          </div>
          <div>
            <dt>{{ project.lastActivity }}</dt>
            <dd>
              <time [attr.datetime]="view.lastCommit.iso" [attr.title]="view.lastCommit.date">{{
                view.lastCommit.ago
              }}</time>
            </dd>
          </div>
        </dl>
      </div>
    }
  `,
  styles: `
    :host {
      display: block;
      margin-top: var(--space-6);
    }

    .links {
      display: flex;
      flex-wrap: wrap;
      gap: var(--space-2) var(--space-5);
      margin: 0;
      padding: 0;
      list-style: none;
    }

    .links a {
      display: inline-block;
      padding-block: var(--space-2);
      font-weight: 600;
    }

    .stats {
      display: inline-block;
      margin-top: var(--space-4);
      padding: var(--space-3) var(--space-5);
      border: 1px solid var(--color-border);
      border-radius: var(--radius-md);
      background: var(--color-surface);
    }

    .caption {
      margin: 0 0 var(--space-2);
      color: var(--color-text-muted);
      font-size: var(--text-sm);
    }

    dl {
      display: flex;
      flex-wrap: wrap;
      gap: var(--space-2) var(--space-6);
      margin: 0;
    }

    dt {
      color: var(--color-text-muted);
      font-size: var(--text-sm);
    }

    dd {
      margin: 0;
      font-weight: 600;
    }

    time {
      font-weight: 400;
    }

    dd a + time::before {
      content: ' · ';
    }
  `,
})
export class GithubStrip {
  private readonly content = inject(LANDING_CONTENT);
  protected readonly project = this.content.getStarted.project;

  private readonly stats = signal<ProjectStats | undefined>(undefined);

  /** The stats, formatted for display; `undefined` while unavailable. */
  protected readonly view = computed(() => {
    const stats = this.stats();
    if (!stats) {
      return undefined;
    }
    const locale = this.content.meta.locale;
    const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium' });
    const release = stats.latestRelease;
    return {
      stars: new Intl.NumberFormat(locale).format(stats.stars),
      release: release && { ...release, date: date.format(new Date(release.publishedAt)) },
      lastCommit: {
        iso: stats.lastCommitAt,
        date: date.format(new Date(stats.lastCommitAt)),
        ago: timeAgo(stats.lastCommitAt, Date.now(), locale),
      },
    };
  });

  constructor() {
    const api = inject(ProjectApi);
    afterNextRender(() => {
      void api.stats().then((stats) => this.stats.set(stats));
    });
  }
}
