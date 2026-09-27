import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';

/**
 * Founder slot. A clearly marked placeholder until the operator supplies the
 * profile (work package #1675); nothing about the founder is invented.
 */
@Component({
  selector: 'app-founder',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="section" [id]="founder.id" aria-labelledby="founder-heading">
      <div class="container">
        <p class="eyebrow"><span>06</span>{{ founder.label }}</p>
        <h2 class="section-heading" id="founder-heading">{{ founder.heading }}</h2>
        <div class="placeholder">
          <span class="badge">{{ founder.placeholderLabel }}</span>
          <p>{{ founder.placeholder }}</p>
        </div>
        <a class="next-step" [href]="founder.nextStep.href">{{ founder.nextStep.label }}</a>
      </div>
    </section>
  `,
  styles: `
    .placeholder {
      display: grid;
      gap: var(--space-3);
      justify-items: start;
      max-width: 40rem;
      margin-top: var(--space-6);
      padding: var(--space-6);
      border: 2px dashed var(--color-mesh);
      border-radius: var(--radius-lg);
      color: var(--color-text-muted);
    }

    .badge {
      padding: var(--space-1) var(--space-3);
      border: 2px dashed var(--color-mesh);
      border-radius: 999px;
      color: var(--color-text);
      font-family: var(--font-mono);
      font-size: var(--text-xs);
      font-weight: 700;
      letter-spacing: 0.06em;
      text-transform: uppercase;
    }
  `,
})
export class Founder {
  protected readonly founder = inject(LANDING_CONTENT).founder;
}
