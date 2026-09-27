import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';
import { ThemeService } from '../core/theme.service';

/** Header button that switches between the light and the dark theme. */
@Component({
  selector: 'app-theme-toggle',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button type="button" [attr.aria-label]="label()" [title]="label()" (click)="themes.toggle()">
      <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 3a9 9 0 0 1 0 18z" />
      </svg>
    </button>
  `,
  styles: `
    button {
      display: grid;
      place-items: center;
      width: 2.75rem;
      height: 2.75rem;
      padding: 0;
      border: 1px solid var(--color-border);
      border-radius: var(--radius-md);
      background: transparent;
      color: var(--color-text);
      cursor: pointer;
    }

    button:hover {
      border-color: var(--color-border-strong);
    }

    svg {
      width: 1.25rem;
      height: 1.25rem;
      fill: none;
      stroke: currentColor;
      stroke-width: 2;
    }

    path {
      fill: currentColor;
    }
  `,
})
export class ThemeToggle {
  protected readonly themes = inject(ThemeService);
  private readonly a11y = inject(LANDING_CONTENT).a11y;

  protected readonly label = computed(() =>
    this.themes.theme() === 'dark' ? this.a11y.themeToLight : this.a11y.themeToDark,
  );
}
