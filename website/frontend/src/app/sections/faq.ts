import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';

/** Frequently asked questions as native disclosure widgets (keyboard ready). */
@Component({
  selector: 'app-faq',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="section section--alt" [id]="faq.id" aria-labelledby="faq-heading">
      <div class="container">
        <p class="eyebrow"><span>07</span>{{ faq.label }}</p>
        <h2 class="section-heading" id="faq-heading">{{ faq.heading }}</h2>
        <div class="items">
          @for (item of faq.items; track item.question) {
            <details>
              <summary>
                <h3>{{ item.question }}</h3>
              </summary>
              <p>{{ item.answer }}</p>
            </details>
          }
        </div>
        <a class="next-step" [href]="faq.nextStep.href">{{ faq.nextStep.label }}</a>
      </div>
    </section>
  `,
  styles: `
    .items {
      max-width: 48rem;
      margin-top: var(--space-6);
      border-top: 1px solid var(--color-border);
    }

    details {
      border-bottom: 1px solid var(--color-border);
    }

    summary {
      display: flex;
      gap: var(--space-4);
      align-items: center;
      justify-content: space-between;
      padding-block: var(--space-4);
      cursor: pointer;
      list-style: none;
    }

    summary::-webkit-details-marker {
      display: none;
    }

    summary::after {
      content: '';
      flex: none;
      width: 0.6rem;
      height: 0.6rem;
      margin-right: var(--space-2);
      border-right: 2px solid var(--color-accent-text);
      border-bottom: 2px solid var(--color-accent-text);
      transform: rotate(45deg);
      transition: transform var(--duration);
    }

    details[open] summary::after {
      transform: rotate(-135deg);
    }

    h3 {
      font-size: var(--text-lg);
      font-weight: 600;
    }

    details p {
      max-width: 62ch;
      padding-bottom: var(--space-5);
      color: var(--color-text-muted);
    }
  `,
})
export class Faq {
  protected readonly faq = inject(LANDING_CONTENT).faq;
}
