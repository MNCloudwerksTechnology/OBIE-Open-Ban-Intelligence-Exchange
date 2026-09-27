import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';
import { GithubIcon, ObieMark } from '../layout/icons';

/** Hero: the promise, the two main calls to action and a sample signed report. */
@Component({
  selector: 'app-hero',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [GithubIcon, ObieMark],
  template: `
    <section class="hero" aria-labelledby="hero-heading">
      <div class="container layout">
        <div class="copy">
          <p class="eyebrow">{{ hero.eyebrow }}</p>
          <h1 id="hero-heading">{{ hero.heading }}</h1>
          <p class="lead">{{ hero.lead }}</p>
          <div class="actions">
            <a class="button button--primary" [href]="hero.primary.href">
              <app-github-icon />
              {{ hero.primary.label }}
            </a>
            <a class="button button--secondary" [href]="hero.secondary.href">
              {{ hero.secondary.label }}
            </a>
          </div>
          <p class="challenge">
            <a [href]="hero.challenge.href">{{ hero.challenge.label }}</a>
          </p>
          <p class="no-tokens">{{ hero.noTokens }}</p>
        </div>
        <figure class="report">
          <app-obie-mark class="backdrop" />
          <div class="report-card">
            <p class="report-title">{{ hero.report.title }}</p>
            <dl>
              @for (row of hero.report.rows; track row.key) {
                <div>
                  <dt>{{ row.key }}</dt>
                  <dd>{{ row.value }}</dd>
                </div>
              }
            </dl>
            <p class="signature">
              <span class="seal" aria-hidden="true"></span>{{ hero.report.signature }}
            </p>
          </div>
          <figcaption>{{ hero.report.caption }}</figcaption>
        </figure>
      </div>
    </section>
  `,
  styleUrl: './hero.scss',
})
export class Hero {
  protected readonly hero = inject(LANDING_CONTENT).hero;
}
