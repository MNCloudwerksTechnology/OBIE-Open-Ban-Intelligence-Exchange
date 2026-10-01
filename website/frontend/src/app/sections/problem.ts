import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';

/** Why OBIE exists: every server alone, central feeds as a weak shortcut. */
@Component({
  selector: 'app-problem',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="section section--alt" [id]="problem.id" aria-labelledby="problem-heading">
      <div class="container">
        <p class="eyebrow"><span>01</span>{{ problem.label }}</p>
        <h2 class="section-heading" id="problem-heading">{{ problem.heading }}</h2>
        <p class="hook">{{ problem.hook }}</p>
        <div class="text">
          @for (paragraph of problem.paragraphs; track $index) {
            <p>{{ paragraph }}</p>
          }
        </div>
        <ul class="card-grid card-grid--3">
          @for (point of problem.points; track point.title) {
            <li class="card">
              <h3>{{ point.title }}</h3>
              <p>{{ point.text }}</p>
            </li>
          }
        </ul>
        <p class="answer">{{ problem.answer }}</p>
        <a class="next-step" [href]="problem.nextStep.href" rel="noopener">{{
          problem.nextStep.label
        }}</a>
      </div>
    </section>
  `,
  styles: `
    .hook {
      max-width: 36ch;
      margin-top: var(--space-6);
      padding-left: var(--space-4);
      border-left: 4px solid var(--color-accent);
      font-size: var(--text-xl);
      font-weight: 600;
      line-height: 1.35;
    }

    .text {
      display: grid;
      gap: var(--space-4);
      max-width: 62ch;
      margin-top: var(--space-6);
      color: var(--color-text-muted);
    }

    .card h3::before {
      content: '';
      display: block;
      width: var(--space-6);
      height: 3px;
      margin-bottom: var(--space-4);
      background: var(--color-accent);
    }

    .answer {
      max-width: 62ch;
      margin-top: var(--space-7);
      font-size: var(--text-lg);
      font-weight: 600;
    }
  `,
})
export class Problem {
  protected readonly problem = inject(LANDING_CONTENT).problem;
}
