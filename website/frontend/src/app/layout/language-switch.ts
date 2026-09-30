import { PlatformLocation } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  booleanAttribute,
  computed,
  inject,
  input,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router } from '@angular/router';
import { filter, map } from 'rxjs';

import { LANDING_CONTENT } from '../content/landing.content';
import { LANGS, counterpartPath } from '../i18n/languages';
import { LANG } from '../i18n/provide-i18n';

/**
 * Link to the current page in the other language (ADR 0032), labelled in
 * that language: a compact button ("DE") by default, the language's name as
 * a text link ("Deutsch") with `plain`. It is a plain link: the other
 * language's page is prerendered, so the browser loads it like any other
 * page and the running application never changes its language.
 */
@Component({
  selector: 'app-language-switch',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { '[class.plain]': 'plain()' },
  template: `
    <a
      [href]="href()"
      [attr.hreflang]="target"
      [attr.lang]="target"
      [attr.aria-label]="plain() ? null : copy.name"
      [attr.title]="plain() ? null : copy.name"
      >{{ plain() ? copy.name : copy.label }}</a
    >
  `,
  styles: `
    :host(:not(.plain)) a {
      display: inline-grid;
      place-items: center;
      min-width: 2.75rem;
      height: 2.75rem;
      padding-inline: var(--space-2);
      border: 1px solid var(--color-border);
      border-radius: var(--radius-md);
      color: var(--color-text);
      font-size: var(--text-sm);
      font-weight: 700;
      letter-spacing: 0.04em;
      text-decoration: none;
    }

    :host(:not(.plain)) a:hover {
      border-color: var(--color-border-strong);
    }
  `,
})
export class LanguageSwitch {
  /** A text link with the language's name instead of the compact button. */
  readonly plain = input(false, { transform: booleanAttribute });

  protected readonly copy = inject(LANDING_CONTENT).header.language;
  private readonly lang = inject(LANG);

  /** The language this link switches to. */
  protected readonly target = LANGS.find((lang) => lang !== this.lang) ?? this.lang;

  /** The current path; the prerendered page and the hydrating one start from the same URL. */
  private readonly path = toSignal(
    inject(Router).events.pipe(
      filter((event) => event instanceof NavigationEnd),
      map((event) => event.urlAfterRedirects),
    ),
    { initialValue: inject(PlatformLocation).pathname },
  );

  protected readonly href = computed(() => counterpartPath(this.path(), this.target));
}
