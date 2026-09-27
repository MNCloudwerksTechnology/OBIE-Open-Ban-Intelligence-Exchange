import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';

/**
 * The v0.1 flow as a picture: an attacker hits two peers, both send a
 * signed report, your server decides. Colours come from the theme tokens, so
 * it works in light and dark. Four nodes, eight labels (operator limit: 5/12).
 */
@Component({
  selector: 'app-flow-diagram',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <figure>
      <svg viewBox="0 0 480 300" role="img" aria-labelledby="flow-title flow-desc">
        <title id="flow-title">{{ diagram.title }}</title>
        <desc id="flow-desc">{{ diagram.description }}</desc>
        <defs>
          <marker
            id="flow-arrow"
            viewBox="0 0 10 10"
            refX="8"
            refY="5"
            markerWidth="7"
            markerHeight="7"
            orient="auto"
          >
            <path class="arrow-head" d="M0 0L10 5L0 10z" />
          </marker>
        </defs>

        <path class="attack" d="M80 138L148 78M80 162L148 222" />
        <path class="report" d="M252 66C300 66 312 118 336 128" marker-end="url(#flow-arrow)" />
        <path class="report" d="M252 234C300 234 312 182 336 172" marker-end="url(#flow-arrow)" />
        <circle class="seal" cx="298" cy="84" r="8" />
        <circle class="seal" cx="298" cy="216" r="8" />
        <text class="caption" x="330" y="26">{{ diagram.report }}</text>

        <circle class="attacker" cx="48" cy="150" r="30" />
        <path class="attacker-mark" d="M38 140L58 160M58 140L38 160" />
        <text x="48" y="214">{{ diagram.attacker }}</text>

        <rect class="peer" x="150" y="36" width="100" height="60" rx="10" />
        <text x="200" y="74">{{ diagram.peerA }}</text>
        <rect class="peer" x="150" y="204" width="100" height="60" rx="10" />
        <text x="200" y="242">{{ diagram.peerB }}</text>

        <rect class="you" x="338" y="100" width="138" height="100" rx="14" />
        <text class="you-label" x="407" y="157">{{ diagram.you }}</text>
      </svg>
      <ul class="checks">
        @for (check of checks; track check) {
          <li>{{ check }}</li>
        }
      </ul>
    </figure>
  `,
  styleUrl: './flow-diagram.scss',
})
export class FlowDiagram {
  protected readonly diagram = inject(LANDING_CONTENT).howItWorks.diagram;
  protected readonly checks = [this.diagram.decision, this.diagram.safetyList, this.diagram.block];
}
