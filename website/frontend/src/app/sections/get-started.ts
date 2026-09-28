import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';
import { GithubStrip } from './github-strip';

/** Three-step teaser (install, observe only, connect peers), contributor links and live stats. */
@Component({
  selector: 'app-get-started',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [GithubStrip],
  template: `
    <section class="section section--alt" [id]="start.id" aria-labelledby="start-heading">
      <div class="container">
        <p class="eyebrow"><span>05</span>{{ start.label }}</p>
        <h2 class="section-heading" id="start-heading">{{ start.heading }}</h2>
        <p class="lead">{{ start.intro }}</p>
        <ol class="card-grid card-grid--3">
          @for (step of start.steps; track step.title; let i = $index) {
            <li class="card">
              <span class="index" aria-hidden="true">{{ '0' + (i + 1) }}</span>
              <h3>{{ step.title }}</h3>
              <p>{{ step.text }}</p>
              <pre><code>{{ step.code }}</code></pre>
            </li>
          }
        </ol>
        <p class="note">{{ start.note }}</p>
        <p class="note">
          <a [href]="start.quickStart.href" rel="noopener">{{ start.quickStart.label }}</a>
        </p>
        <app-github-strip />
        <a class="next-step" [href]="start.nextStep.href" rel="noopener">{{
          start.nextStep.label
        }}</a>
      </div>
    </section>
  `,
  styles: `
    .card {
      display: flex;
      flex-direction: column;
    }

    .index {
      display: block;
      margin-bottom: var(--space-2);
    }

    pre {
      margin: auto 0 0;
      padding: var(--space-3) var(--space-4);
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      border-radius: var(--radius-md);
      background: var(--color-code-bg);
      color: var(--color-code-text);
      font-size: var(--text-sm);
    }

    .card p {
      margin-bottom: var(--space-4);
    }

    .note {
      max-width: 62ch;
      margin-top: var(--space-6);
      color: var(--color-text-muted);
      font-size: var(--text-sm);
    }

    .note + .note {
      margin-top: var(--space-2);
    }
  `,
})
export class GetStarted {
  protected readonly start = inject(LANDING_CONTENT).getStarted;
}
