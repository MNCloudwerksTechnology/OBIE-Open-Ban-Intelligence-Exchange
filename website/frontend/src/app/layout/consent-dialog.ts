import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  afterRenderEffect,
  inject,
  viewChild,
} from '@angular/core';

import { CONSENT_CONTENT } from '../content/consent.content';
import { ConsentService } from '../core/analytics/consent.service';
import { LanguageSwitch } from './language-switch';

/**
 * The question about visitor statistics (ADR 0034): a non-modal dialog at the
 * bottom of the window that leaves the page usable. Declining is offered
 * exactly like accepting. It asks on the first visit and whenever the
 * visitor opens the privacy settings; only then does it take focus. The
 * link to the other language lets a visitor read the question, and the
 * site, in their language before answering.
 */
@Component({
  selector: 'app-consent-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [LanguageSwitch],
  template: `
    @if (consent.dialogOpen()) {
      <section
        #dialog
        class="consent"
        role="dialog"
        aria-labelledby="consent-heading"
        aria-describedby="consent-text"
        tabindex="-1"
      >
        <div class="head">
          <!-- Not a heading: the dialog sits outside the page's outline, which starts with its h1. -->
          <p class="title" id="consent-heading">{{ copy.heading }}</p>
          <app-language-switch plain />
        </div>
        <div id="consent-text">
          @for (paragraph of copy.paragraphs; track $index) {
            <p>{{ paragraph }}</p>
          }
          @if (consent.decision(); as decision) {
            <p class="current">{{ copy.current[decision] }}</p>
          }
        </div>
        <p>
          <a [href]="copy.privacyLink.href">{{ copy.privacyLink.label }}</a>
        </p>
        <div class="actions">
          <button type="button" class="button button--secondary" (click)="consent.deny()">
            {{ copy.decline }}
          </button>
          <button type="button" class="button button--secondary" (click)="consent.grant()">
            {{ copy.accept }}
          </button>
        </div>
      </section>
    }
  `,
  styles: `
    .consent {
      position: fixed;
      right: var(--space-4);
      bottom: var(--space-4);
      left: var(--space-4);
      z-index: 30;
      display: grid;
      gap: var(--space-3);
      max-width: 34rem;
      line-height: 1.5;
      max-height: calc(100dvh - 2 * var(--space-4));
      overflow-y: auto;
      padding: var(--space-4) var(--space-5) var(--space-5);
      border: 1px solid var(--color-border);
      border-radius: var(--radius-lg);
      background: var(--color-surface);
      box-shadow:
        var(--shadow-card),
        0 12px 32px rgb(0 0 0 / 18%);
      font-size: var(--text-sm);
    }

    .head {
      display: flex;
      gap: var(--space-4);
      align-items: baseline;
      justify-content: space-between;
    }

    .title {
      font-size: var(--text-lg);
      font-weight: 700;
      line-height: 1.3;
    }

    #consent-text {
      display: grid;
      gap: var(--space-2);
    }

    .current {
      font-weight: 600;
    }

    .actions {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: var(--space-3);
    }

    // Buttons get the browser's grey without a background of their own.
    .actions .button {
      min-height: 2.75rem;
      padding-block: var(--space-2);
      background: transparent;
    }

    .actions .button:hover {
      background: var(--color-surface-alt);
    }
  `,
})
export class ConsentDialog {
  protected readonly consent = inject(ConsentService);
  protected readonly copy = inject(CONSENT_CONTENT);
  private readonly dialog = viewChild<ElementRef<HTMLElement>>('dialog');

  constructor() {
    // Asked by the site, the dialog waits to be found; opened by the visitor, it takes focus.
    afterRenderEffect(() => {
      const dialog = this.dialog();
      if (dialog && this.consent.openedByVisitor()) {
        dialog.nativeElement.focus();
      }
    });
  }
}
