import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';
import { sectionHref } from '../i18n/languages';
import { LANG } from '../i18n/provide-i18n';
import { GithubIcon, ObieMark } from './icons';
import { LanguageSwitch } from './language-switch';
import { ThemeToggle } from './theme-toggle';

/**
 * Site header: the mark, anchored navigation to the page sections, language
 * switch, theme toggle and GitHub link.
 */
@Component({
  selector: 'app-site-header',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [GithubIcon, LanguageSwitch, ObieMark, ThemeToggle],
  template: `
    <header id="top">
      <div class="container bar">
        <a class="brand" [href]="href('top')" [attr.aria-label]="content.a11y.homeLink">
          <app-obie-mark />
          <span aria-hidden="true">OBIE</span>
        </a>
        <nav [attr.aria-label]="content.a11y.primaryNav">
          <ul>
            @for (section of sections; track section.id) {
              <li>
                <a [href]="href(section.id)">{{ section.label }}</a>
              </li>
            }
          </ul>
        </nav>
        <div class="actions">
          <app-language-switch />
          <app-theme-toggle />
          <a
            class="button button--primary github"
            [href]="content.header.github.href"
            rel="noopener"
          >
            <app-github-icon />
            <span class="label">{{ content.header.github.label }}</span>
          </a>
        </div>
      </div>
    </header>
  `,
  styleUrl: './site-header.scss',
})
export class SiteHeader {
  protected readonly content = inject(LANDING_CONTENT);
  private readonly lang = inject(LANG);

  /** The sections listed in the navigation, in page order. */
  protected readonly sections = [
    this.content.problem,
    this.content.howItWorks,
    this.content.principles,
    this.content.status,
    this.content.getStarted,
    this.content.faq,
  ];

  /** Link to a section of the home page in this page's language. */
  protected href(id: string): string {
    return sectionHref(this.lang, id);
  }
}
