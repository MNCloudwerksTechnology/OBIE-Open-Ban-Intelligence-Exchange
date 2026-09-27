import { ChangeDetectionStrategy, Component } from '@angular/core';

/**
 * The OBIE mark (six peers around a shared hub), inlined so it follows the
 * theme. Decorative: the surrounding link or text names it.
 */
@Component({
  selector: 'app-obie-mark',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <svg viewBox="30 30 140 140" aria-hidden="true" focusable="false">
      <path class="ring" d="M100 40 L160 70 L160 130 L100 160 L40 130 L40 70 Z" />
      <path
        class="spokes"
        d="M100 50V80M151 75L117 90M151 125L117 110M100 150V120M49 125L83 110M49 75L83 90"
      />
      <circle class="hub" cx="100" cy="100" r="20" />
      <g class="nodes">
        <circle cx="100" cy="40" r="10" />
        <circle cx="160" cy="70" r="10" />
        <circle cx="160" cy="130" r="10" />
        <circle cx="100" cy="160" r="10" />
        <circle cx="40" cy="130" r="10" />
        <circle cx="40" cy="70" r="10" />
      </g>
    </svg>
  `,
  styles: `
    :host {
      display: inline-block;
      line-height: 0;
    }

    svg {
      width: 100%;
      height: 100%;
      fill: none;
      stroke-width: 4;
    }

    .ring {
      stroke: var(--color-text);
      stroke-width: 3;
    }

    .spokes {
      stroke: var(--color-mesh);
      stroke-width: 3;
    }

    .hub {
      stroke: var(--color-mark);
      stroke-width: 6;
    }

    .nodes {
      fill: var(--color-mark);
    }
  `,
})
export class ObieMark {}

/** The GitHub mark, inlined so no request leaves the site. Decorative. */
@Component({
  selector: 'app-github-icon',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <path
        d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"
      />
    </svg>
  `,
  styles: `
    :host {
      display: inline-flex;
    }

    svg {
      width: 1.25rem;
      height: 1.25rem;
      fill: currentColor;
    }
  `,
})
export class GithubIcon {}
