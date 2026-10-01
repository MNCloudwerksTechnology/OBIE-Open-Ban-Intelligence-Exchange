import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';

import { LANDING_CONTENT } from '../../content/landing.content';
import { fill } from './format';
import { AttackerId, Frame, PublisherId, SERVER_IDS, ServerId } from './mesh-demo.model';

interface Point {
  readonly x: number;
  readonly y: number;
}

/** A straight line between two actors, from the edge of one to the edge of the other. */
interface Segment {
  readonly from: Point;
  readonly to: Point;
}

interface Arrow extends Segment {
  readonly key: string;
  readonly kind: 'report' | 'revocation' | 'untrusted';
}

interface AttackMark extends Segment {
  readonly key: string;
  readonly at: Point;
  readonly label: string;
  readonly turnedAway: boolean;
}

// Positions on the map. The rogue sits beyond C, so that its lines to A, B
// and C cross no other line: no point on the map looks like a centre. Labels
// sit above the servers, clear of every line.
const NODE_RADIUS = 42;
const ACTOR_RADIUS = 20;
const NODES: Readonly<Record<ServerId, Point>> = {
  a: { x: 150, y: 120 },
  b: { x: 530, y: 120 },
  c: { x: 340, y: 255 },
};
const LABEL_OFFSET = -56;
const ROGUE: Point = { x: 340, y: 405 };
const ATTACKER_POSITIONS: Readonly<Record<ServerId, Point>> = {
  a: { x: 45, y: 120 },
  b: { x: 635, y: 120 },
  c: { x: 180, y: 330 },
};
const SCANNER_POSITION: Point = { x: 500, y: 330 };
const LINKS: readonly [ServerId, ServerId][] = [
  ['a', 'b'],
  ['a', 'c'],
  ['b', 'c'],
];

/**
 * The demo's servers as a picture: direct links, attackers, and the messages
 * of the current step. Decorative: the server cards and the caption carry
 * every fact as text, so the map is hidden from assistive technology.
 */
@Component({
  selector: 'app-mesh-map',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <svg viewBox="0 0 680 440" aria-hidden="true" focusable="false" [class.animated]="animate()">
      <defs>
        <marker
          id="mesh-arrow"
          viewBox="0 0 10 10"
          refX="8"
          refY="5"
          markerWidth="5"
          markerHeight="5"
          orient="auto"
        >
          <path class="arrow-head" d="M0 0L10 5L0 10z" />
        </marker>
        <marker
          id="mesh-arrow-muted"
          viewBox="0 0 10 10"
          refX="8"
          refY="5"
          markerWidth="5"
          markerHeight="5"
          orient="auto"
        >
          <path class="arrow-head arrow-head--muted" d="M0 0L10 5L0 10z" />
        </marker>
      </defs>

      @for (link of links; track $index) {
        <line
          class="link"
          [attr.x1]="link.from.x"
          [attr.y1]="link.from.y"
          [attr.x2]="link.to.x"
          [attr.y2]="link.to.y"
        />
      }
      @if (rogueConnected()) {
        @for (link of rogueLinks; track $index) {
          <line
            class="link link--untrusted"
            [attr.x1]="link.from.x"
            [attr.y1]="link.from.y"
            [attr.x2]="link.to.x"
            [attr.y2]="link.to.y"
          />
        }
        <g class="rogue">
          <circle [attr.cx]="rogue.x" [attr.cy]="rogue.y" [attr.r]="actorRadius" />
          <text [attr.x]="rogue.x" [attr.y]="rogue.y + 7">{{ demo.rogue.short }}</text>
        </g>
      }

      @for (arrow of arrows(); track arrow.key) {
        <g class="message message--{{ arrow.kind }}">
          <line
            [attr.x1]="arrow.from.x"
            [attr.y1]="arrow.from.y"
            [attr.x2]="arrow.to.x"
            [attr.y2]="arrow.to.y"
            [attr.marker-end]="
              arrow.kind === 'untrusted' ? 'url(#mesh-arrow-muted)' : 'url(#mesh-arrow)'
            "
          />
          <g
            class="packet"
            [style.--from-x.px]="arrow.from.x"
            [style.--from-y.px]="arrow.from.y"
            [style.--to-x.px]="arrow.to.x"
            [style.--to-y.px]="arrow.to.y"
          >
            <rect x="-13" y="-9" width="26" height="18" rx="3" />
            <circle cx="0" cy="3" r="4" />
          </g>
        </g>
      }
      @if (copies(); as label) {
        <text class="copies" [attr.x]="rogue.x + 34" [attr.y]="rogue.y + 7">{{ label }}</text>
      }

      @for (node of nodes(); track node.id) {
        <g class="node" [class.node--blocking]="node.blocking">
          <circle [attr.cx]="node.at.x" [attr.cy]="node.at.y" [attr.r]="nodeRadius" />
          <text class="letter" [attr.x]="node.at.x" [attr.y]="node.at.y + 11">
            {{ node.letter }}
          </text>
          <text class="operator" [attr.x]="node.at.x" [attr.y]="node.labelY">
            {{ node.operator }}
          </text>
        </g>
      }

      @for (attack of attacks(); track attack.key) {
        <g class="attack" [class.attack--turned-away]="attack.turnedAway">
          <line
            class="attack-line"
            [attr.x1]="attack.from.x"
            [attr.y1]="attack.from.y"
            [attr.x2]="attack.to.x"
            [attr.y2]="attack.to.y"
          />
          @if (attack.turnedAway) {
            <circle class="stop" [attr.cx]="attack.to.x" [attr.cy]="attack.to.y" r="11" />
          }
          <g class="attacker">
            <circle [attr.cx]="attack.at.x" [attr.cy]="attack.at.y" [attr.r]="actorRadius" />
            <path class="cross" [attr.d]="cross(attack.at)" />
            <text [attr.x]="attack.at.x" [attr.y]="attack.at.y + 42">{{ attack.label }}</text>
          </g>
        </g>
      }
    </svg>
  `,
  styleUrl: './mesh-map.scss',
})
export class MeshMap {
  readonly frame = input.required<Frame>();
  /** Whether messages and attackers move; false when the visitor asked for reduced motion. */
  readonly animate = input(false);

  protected readonly demo = inject(LANDING_CONTENT).howItWorks.demo;
  protected readonly nodeRadius = NODE_RADIUS;
  protected readonly actorRadius = ACTOR_RADIUS;
  protected readonly rogue = ROGUE;
  protected readonly links = LINKS.map(([x, y]) =>
    between(NODES[x], NODE_RADIUS, NODES[y], NODE_RADIUS),
  );
  protected readonly rogueLinks = SERVER_IDS.map((id) =>
    between(ROGUE, ACTOR_RADIUS, NODES[id], NODE_RADIUS),
  );

  protected readonly nodes = computed(() =>
    this.frame().servers.map((server) => ({
      id: server.id,
      at: NODES[server.id],
      letter: this.demo.servers[server.id].short,
      operator: this.demo.servers[server.id].operator,
      labelY: NODES[server.id].y + LABEL_OFFSET,
      blocking: server.decisions.some((d) => d.state === 'blocked' && d.before !== 'blocked'),
    })),
  );

  /** The rogue appears once any server holds one of its reports. */
  protected readonly rogueConnected = computed(() =>
    this.frame().servers.some((server) =>
      server.decisions.some((d) => d.contributions.some((c) => c.publisher === 'rogue')),
    ),
  );

  protected readonly arrows = computed(() =>
    this.frame().messages.flatMap((message, m) =>
      message.to.map((to): Arrow => {
        const [from, radius] = origin(message.from);
        return {
          key: `${this.frame().index}-${m}-${to}`,
          kind: message.from === 'rogue' ? 'untrusted' : message.kind,
          ...between(from, radius + 4, NODES[to], NODE_RADIUS + 6),
        };
      }),
    ),
  );

  protected readonly copies = computed(() => {
    const flood = this.frame().messages.find((message) => message.copies > 1);
    return flood ? fill(this.demo.labels.copies, { n: flood.copies }) : '';
  });

  protected readonly attacks = computed(() =>
    this.frame().attacks.map((attack, i): AttackMark => {
      const at = attackerPosition(attack.attacker, attack.target);
      const label = this.demo.subjects[attack.attacker].short;
      return {
        key: `${this.frame().index}-${i}`,
        at,
        label: attack.turnedAway ? `${label} · ${this.demo.labels.turnedAway}` : label,
        turnedAway: attack.turnedAway,
        ...between(at, ACTOR_RADIUS + 4, NODES[attack.target], NODE_RADIUS + 8),
      };
    }),
  );

  /** The attacker's mark, the same cross as in the flow diagram. */
  protected cross(at: Point): string {
    const d = 9;
    return `M${at.x - d} ${at.y - d}L${at.x + d} ${at.y + d}M${at.x + d} ${at.y - d}L${at.x - d} ${at.y + d}`;
  }
}

function origin(publisher: PublisherId): [Point, number] {
  return publisher === 'rogue' ? [ROGUE, ACTOR_RADIUS] : [NODES[publisher], NODE_RADIUS];
}

function attackerPosition(attacker: AttackerId, target: ServerId): Point {
  return attacker === 'scanner' ? SCANNER_POSITION : ATTACKER_POSITIONS[target];
}

/** The part of the line from `a` to `b` outside both circles around them. */
function between(a: Point, gapA: number, b: Point, gapB: number): Segment {
  const length = Math.hypot(b.x - a.x, b.y - a.y);
  const ux = (b.x - a.x) / length;
  const uy = (b.y - a.y) / length;
  return {
    from: { x: round(a.x + ux * gapA), y: round(a.y + uy * gapA) },
    to: { x: round(b.x - ux * gapB), y: round(b.y - uy * gapB) },
  };
}

function round(value: number): number {
  return Math.round(value * 10) / 10;
}
