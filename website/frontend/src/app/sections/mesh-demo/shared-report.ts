import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';

import { LANDING_CONTENT } from '../../content/landing.content';
import { decimal, duration, fill } from './format';
import { Report, ServerId } from './mesh-demo.model';
import { SCENARIO } from './scenario';

/** What a signed report carries (obie/0.1), and what stays on the server that sends it. */
@Component({
  selector: 'app-shared-report',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="shared">
      <p class="title">{{ heading() }}</p>
      <dl>
        @for (row of rows(); track row.key) {
          <div>
            <dt>{{ row.key }}</dt>
            <dd>
              @if (row.code) {
                <code>{{ row.code }}</code>
              }
              @if (row.value) {
                &ngsp;<span [class.hint]="row.code">{{ row.value }}</span>
              }
            </dd>
          </div>
        }
      </dl>
    </div>
    <div class="kept">
      <p class="title">{{ keptHeading() }}</p>
      <ul>
        @for (item of copy.kept; track item) {
          <li>{{ item }}</li>
        }
      </ul>
    </div>
  `,
  styles: `
    :host {
      display: grid;
      gap: var(--space-3);
      margin-top: var(--space-5);
      font-size: var(--text-sm);
    }

    .shared,
    .kept {
      padding: var(--space-3) var(--space-4);
      border-radius: var(--radius-md);
    }

    .shared {
      border: 2px solid var(--color-accent);
      background: var(--color-surface);
    }

    .kept {
      border: 2px dashed var(--color-border-strong);
    }

    .title {
      font-family: var(--font-mono);
      font-weight: 700;
    }

    dl {
      display: grid;
      gap: var(--space-1);
      margin: var(--space-2) 0 0;
    }

    dl div {
      display: grid;
      grid-template-columns: 7.5rem minmax(0, 1fr);
      gap: var(--space-2);
    }

    dt {
      color: var(--color-text-muted);
    }

    dd {
      margin: 0;
      overflow-wrap: anywhere;
    }

    .hint {
      display: block;
      color: var(--color-text-muted);
      font-size: var(--text-xs);
    }

    ul {
      margin: var(--space-2) 0 0;
      padding-left: var(--space-5);
    }
  `,
})
export class SharedReport {
  readonly report = input.required<Report>();
  readonly receivers = input.required<readonly ServerId[]>();

  private readonly content = inject(LANDING_CONTENT);
  protected readonly copy = this.content.howItWorks.demo.report;
  private readonly demo = this.content.howItWorks.demo;

  private readonly sender = computed(() => {
    const { publisher } = this.report();
    return publisher === 'rogue' ? this.demo.rogue.short : this.demo.servers[publisher].short;
  });

  protected readonly heading = computed(() => fill(this.copy.heading, { server: this.sender() }));
  protected readonly keptHeading = computed(() =>
    fill(this.copy.keptHeading, { server: this.sender() }),
  );

  protected readonly rows = computed(() => {
    const report = this.report();
    const { fields } = this.copy;
    const locale = this.content.meta.locale;
    const receivers = new Intl.ListFormat(locale, { type: 'conjunction' }).format(
      this.receivers().map((id) => this.demo.servers[id].short),
    );
    return [
      { key: fields.address, value: '', code: SCENARIO.addresses[report.subject] },
      { key: fields.reason, value: this.copy.reasons[report.reason] ?? '', code: report.reason },
      { key: fields.events, value: fill(this.copy.eventCount, { n: report.events }), code: '' },
      { key: fields.fingerprint, value: this.copy.fingerprintNote, code: report.logHash },
      {
        key: fields.suggestion,
        value: fill(this.copy.suggestionValue, { duration: duration(report.ttlMinutes, locale) }),
        code: '',
      },
      { key: fields.confidence, value: decimal(report.confidence, 1, locale), code: '' },
      { key: fields.signature, value: fill(this.copy.signatureValue, { receivers }), code: '' },
    ];
  });
}
