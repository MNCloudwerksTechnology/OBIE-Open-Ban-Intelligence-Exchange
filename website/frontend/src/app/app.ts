import { ChangeDetectionStrategy, Component, afterNextRender, inject } from '@angular/core';
import { RouterOutlet } from '@angular/router';

import { LANDING_CONTENT } from './content/landing.content';
import { Analytics } from './core/analytics/analytics';
import { ConsentService } from './core/analytics/consent.service';
import { ConsentDialog } from './layout/consent-dialog';
import { SiteFooter } from './layout/site-footer';
import { SiteHeader } from './layout/site-header';

/**
 * Application shell: skip link, the question about visitor statistics (in
 * the browser only), header, the page in the main landmark, footer.
 */
@Component({
  selector: 'app-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ConsentDialog, RouterOutlet, SiteFooter, SiteHeader],
  template: `
    <a class="skip-link" href="#main" (click)="skipToMain($event)">{{ skipLink }}</a>
    <app-consent-dialog />
    <app-site-header />
    <main id="main" tabindex="-1">
      <router-outlet />
    </main>
    <app-site-footer />
  `,
  styles: `
    .skip-link {
      position: absolute;
      top: var(--space-2);
      left: var(--space-2);
      z-index: 20;
      padding: var(--space-3) var(--space-4);
      border-radius: var(--radius-md);
      background: var(--color-accent);
      color: var(--color-on-accent);
      font-weight: 600;
      transform: translateY(-200%);
    }

    .skip-link:focus {
      transform: none;
    }

    main:focus {
      outline: none;
    }
  `,
})
export class App {
  protected readonly skipLink = inject(LANDING_CONTENT).a11y.skipLink;

  constructor() {
    const consent = inject(ConsentService);
    const analytics = inject(Analytics);
    // After hydration, so the prerendered page never shows the dialog.
    afterNextRender(() => {
      consent.start();
      analytics.start();
    });
  }

  /**
   * Moves focus to the main landmark. Handled here because `#main` resolves
   * against `<base href="/">` and would leave pages other than the home page.
   */
  protected skipToMain(event: Event): void {
    const main = (event.target as HTMLElement).ownerDocument.getElementById('main');
    if (main) {
      event.preventDefault();
      main.focus();
    }
  }
}
