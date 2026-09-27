import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';
import { GithubIcon, ObieMark } from './icons';
import { ThemeToggle } from './theme-toggle';

/** Site header: the mark, anchored navigation, theme toggle and GitHub link. */
@Component({
  selector: 'app-site-header',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [GithubIcon, ObieMark, ThemeToggle],
  template: `
    <header id="top">
      <div class="container bar">
        <a class="brand" href="#top" [attr.aria-label]="content.a11y.homeLink">
          <app-obie-mark />
          <span aria-hidden="true">OBIE</span>
        </a>
        <nav [attr.aria-label]="content.a11y.primaryNav">
          <ul>
            @for (section of sections; track section.id) {
              <li>
                <a [href]="'#' + section.id">{{ section.label }}</a>
              </li>
            }
          </ul>
        </nav>
        <div class="actions">
          <app-theme-toggle />
          <a class="button button--primary github" [href]="content.header.github.href">
            <app-github-icon />
            {{ content.header.github.label }}
          </a>
        </div>
      </div>
    </header>
  `,
  styleUrl: './site-header.scss',
})
export class SiteHeader {
  protected readonly content = inject(LANDING_CONTENT);

  /** The sections listed in the navigation, in page order. */
  protected readonly sections = [
    this.content.problem,
    this.content.howItWorks,
    this.content.principles,
    this.content.status,
    this.content.getStarted,
    this.content.faq,
  ];
}
