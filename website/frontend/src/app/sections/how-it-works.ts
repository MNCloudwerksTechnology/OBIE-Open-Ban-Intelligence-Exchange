import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';
import { FlowDiagram } from './flow-diagram';

/** The v0.1 flow as a diagram and six numbered steps. */
@Component({
  selector: 'app-how-it-works',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FlowDiagram],
  template: `
    <section class="section" [id]="how.id" aria-labelledby="how-heading">
      <div class="container">
        <p class="eyebrow"><span>02</span>{{ how.label }}</p>
        <h2 class="section-heading" id="how-heading">{{ how.heading }}</h2>
        <p class="lead">{{ how.intro }}</p>
        <app-flow-diagram />
        <ol class="card-grid card-grid--3">
          @for (step of how.steps; track step.title; let i = $index) {
            <li class="card">
              <span class="index" aria-hidden="true">{{ i + 1 }}</span>
              <h3>{{ step.title }}</h3>
              <p>{{ step.text }}</p>
            </li>
          }
        </ol>
        <p class="note">{{ how.note }}</p>
        <a class="next-step" [href]="how.nextStep.href">{{ how.nextStep.label }}</a>
      </div>
    </section>
  `,
  styles: `
    .index {
      display: grid;
      place-items: center;
      width: 2.25rem;
      height: 2.25rem;
      margin-bottom: var(--space-4);
      border-radius: 50%;
      background: var(--color-accent);
      color: var(--color-on-accent);
    }

    .note {
      margin-top: var(--space-6);
      color: var(--color-text-muted);
      font-size: var(--text-sm);
    }
  `,
})
export class HowItWorks {
  protected readonly how = inject(LANDING_CONTENT).howItWorks;
}
