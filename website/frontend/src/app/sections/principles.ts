import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';

/** The manifesto condensed to six cards. */
@Component({
  selector: 'app-principles',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="section section--alt" [id]="principles.id" aria-labelledby="principles-heading">
      <div class="container">
        <p class="eyebrow"><span>03</span>{{ principles.label }}</p>
        <h2 class="section-heading" id="principles-heading">{{ principles.heading }}</h2>
        <p class="lead">{{ principles.hook }}</p>
        <ul class="card-grid card-grid--3">
          @for (card of principles.cards; track card.title; let i = $index) {
            <li class="card">
              <span class="index" aria-hidden="true">{{ '0' + (i + 1) }}</span>
              <h3>{{ card.title }}</h3>
              <p>{{ card.text }}</p>
            </li>
          }
        </ul>
        <a class="next-step" [href]="principles.nextStep.href" rel="noopener">{{
          principles.nextStep.label
        }}</a>
      </div>
    </section>
  `,
  styles: `
    .card {
      position: relative;
      border-left: 4px solid var(--color-accent);
    }

    .index {
      display: block;
      margin-bottom: var(--space-2);
    }
  `,
})
export class Principles {
  protected readonly principles = inject(LANDING_CONTENT).principles;
}
