import { DOCUMENT, Injectable, computed, inject, signal } from '@angular/core';

export type Theme = 'light' | 'dark';

const DARK_QUERY = '(prefers-color-scheme: dark)';

/**
 * The colour theme. It follows the system setting (`prefers-color-scheme`)
 * until the visitor picks the other theme with the header toggle; the pick
 * lasts for this visit only and is never stored (ADR 0010). The CSS applies
 * the system theme on its own, so the page is themed correctly before this
 * service runs, when prerendering and without JavaScript.
 */
@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly document = inject(DOCUMENT);
  private readonly systemTheme = signal<Theme>('light');
  private readonly chosenTheme = signal<Theme | null>(null);

  /** The theme in effect. */
  readonly theme = computed(() => this.chosenTheme() ?? this.systemTheme());

  constructor() {
    // matchMedia is missing when prerendering; the system theme then stays light.
    const query = this.document.defaultView?.matchMedia?.(DARK_QUERY);
    if (query) {
      this.systemTheme.set(toTheme(query.matches));
      query.addEventListener('change', (event) => this.systemTheme.set(toTheme(event.matches)));
    }
  }

  /** Switches to the other theme for the rest of the visit. */
  toggle(): void {
    const next: Theme = this.theme() === 'dark' ? 'light' : 'dark';
    this.chosenTheme.set(next);
    this.document.documentElement.setAttribute('data-theme', next);
  }
}

function toTheme(dark: boolean): Theme {
  return dark ? 'dark' : 'light';
}
