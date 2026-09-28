import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';

/** Honest status: available, in progress and planned, each with a text label. */
@Component({
  selector: 'app-status',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="section" [id]="status.id" aria-labelledby="status-heading">
      <div class="container">
        <p class="eyebrow"><span>04</span>{{ status.label }}</p>
        <h2 class="section-heading" id="status-heading">{{ status.heading }}</h2>
        <p class="lead">{{ status.intro }}</p>
        <div class="groups">
          @for (group of status.groups; track group.state) {
            <div class="group">
              <h3>
                <span class="badge badge--{{ group.state }}">{{ group.label }}</span>
                <span class="summary">{{ group.summary }}</span>
              </h3>
              <ul>
                @for (item of group.items; track item.title) {
                  <li>
                    <strong>{{ item.title }}</strong>
                    <span>{{ item.text }}</span>
                  </li>
                }
              </ul>
            </div>
          }
        </div>
        <p class="details">
          <a [href]="status.details.href" rel="noopener">{{ status.details.label }}</a>
        </p>
        <a class="next-step" [href]="status.nextStep.href" rel="noopener">{{
          status.nextStep.label
        }}</a>
      </div>
    </section>
  `,
  styles: `
    .groups {
      display: grid;
      gap: var(--space-5);
      margin-top: var(--space-7);
    }

    .group {
      padding: var(--space-5);
      border: 1px solid var(--color-border);
      border-radius: var(--radius-lg);
      background: var(--color-surface);
    }

    h3 {
      display: flex;
      flex-wrap: wrap;
      gap: var(--space-3);
      align-items: center;
      font-size: var(--text-base);
    }

    .badge {
      padding: var(--space-1) var(--space-3);
      border: 2px solid var(--color-accent);
      border-radius: 999px;
      font-family: var(--font-mono);
      font-size: var(--text-xs);
      letter-spacing: 0.06em;
      text-transform: uppercase;
    }

    .badge--available {
      background: var(--color-accent);
      color: var(--color-on-accent);
    }

    .badge--planned {
      border-color: var(--color-mesh);
      border-style: dashed;
    }

    .summary {
      color: var(--color-text-muted);
      font-weight: 600;
    }

    ul {
      display: grid;
      gap: var(--space-4);
      margin: var(--space-5) 0 0;
      padding: 0;
      list-style: none;
    }

    li {
      display: grid;
      gap: var(--space-1);
    }

    li span {
      color: var(--color-text-muted);
      font-size: var(--text-sm);
    }

    .details {
      margin-top: var(--space-6);
    }

    @media (min-width: 64rem) {
      .groups {
        grid-template-columns: repeat(3, minmax(0, 1fr));
      }
    }
  `,
})
export class Status {
  protected readonly status = inject(LANDING_CONTENT).status;
}
