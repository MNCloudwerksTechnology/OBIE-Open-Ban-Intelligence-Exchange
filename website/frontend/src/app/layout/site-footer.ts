import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';
import { GithubIcon, ObieMark } from './icons';

/**
 * Site footer: the GitHub button, project links with the invitation to the
 * inquiry form, legal links, attribution and licence.
 */
@Component({
  selector: 'app-site-footer',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [GithubIcon, ObieMark],
  template: `
    <footer>
      <div class="container">
        <p class="tagline">
          <app-obie-mark />
          {{ footer.tagline }}
        </p>
        <a class="button button--primary github" [href]="footer.github.href" rel="noopener">
          <app-github-icon />
          {{ footer.github.label }}
        </a>
        <nav [attr.aria-label]="content.a11y.footerNav">
          <ul>
            @for (link of footer.links; track link.href) {
              <li>
                <a [href]="link.href" rel="noopener">{{ link.label }}</a>
              </li>
            }
          </ul>
        </nav>
        <p class="small">
          <a [href]="footer.attribution.href" rel="noopener">{{ footer.attribution.text }}</a>
        </p>
        <p class="small">{{ footer.licence }}</p>
      </div>
    </footer>
  `,
  styles: `
    footer {
      padding-block: var(--space-8);
      border-top: 1px solid var(--color-border);
      background: var(--color-surface-alt);
    }

    .tagline {
      display: flex;
      gap: var(--space-3);
      align-items: center;
      font-weight: 700;
    }

    app-obie-mark {
      width: 2rem;
      height: 2rem;
    }

    .github {
      margin-top: var(--space-5);
    }

    ul {
      display: flex;
      flex-wrap: wrap;
      gap: var(--space-2) var(--space-5);
      margin: var(--space-6) 0;
      padding: 0;
      list-style: none;
    }

    nav a {
      display: inline-block;
      padding-block: var(--space-2);
      font-weight: 600;
    }

    .small {
      margin-top: var(--space-2);
      color: var(--color-text-muted);
      font-size: var(--text-sm);
    }

    .small a {
      color: inherit;
    }
  `,
})
export class SiteFooter {
  protected readonly content = inject(LANDING_CONTENT);
  protected readonly footer = this.content.footer;
}
