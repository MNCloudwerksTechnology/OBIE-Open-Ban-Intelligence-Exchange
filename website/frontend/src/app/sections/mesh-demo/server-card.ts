import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';

import { LANDING_CONTENT } from '../../content/landing.content';
import { decimal, fill } from './format';
import {
  DecisionView,
  PublisherId,
  ServerSetup,
  ServerView,
  SubjectState,
} from './mesh-demo.model';
import { SCENARIO } from './scenario';

/** One subject as the card shows it; every value is text, the bar only repeats the score. */
interface Row {
  readonly subject: string;
  readonly name: string;
  readonly address: string;
  readonly state: SubjectState;
  readonly stateLabel: string;
  readonly cause: string;
  readonly changed: boolean;
  /** Present once the server holds reports on the subject. */
  readonly tally?: {
    readonly score: string;
    readonly reporters: string;
    readonly sum: string;
    /** Share of the bar's width; the threshold sits at two thirds. */
    readonly fill: number;
  };
}

const PUBLISHERS: readonly PublisherId[] = ['a', 'b', 'c', 'rogue'];

/** What one server of the demo trusts, never blocks and decides about each subject. */
@Component({
  selector: 'app-server-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h4 class="name">
      <span class="letter" aria-hidden="true">{{ copy().short }}</span>
      <span
        >{{ copy().name }} <span class="operator">{{ copy().operator }}</span></span
      >
    </h4>
    <dl class="setup">
      <div>
        <dt>{{ demo.labels.trusts }}</dt>
        <dd>{{ trust() }}</dd>
      </div>
      <div>
        <dt>{{ demo.labels.safetyList }}</dt>
        <dd>{{ copy().safetyList }}</dd>
      </div>
    </dl>
    <ul class="rows" [class.animated]="animate()">
      @for (row of rows(); track row.subject) {
        <li class="row" [attr.data-state]="row.state" [class.row--changed]="row.changed">
          <p class="subject">
            {{ row.name }} <code>{{ row.address }}</code>
          </p>
          <p class="verdict">
            <span class="state">{{ row.stateLabel }}</span>
            @if (row.cause) {
              &ngsp;<span class="cause">{{ row.cause }}</span>
            }
            @if (row.changed) {
              &ngsp;<span class="changed">{{ demo.labels.changed }}</span>
            }
          </p>
          @if (row.tally; as tally) {
            <p class="tally">
              <span>{{ tally.score }}</span
              >&ngsp;<span>{{ tally.reporters }}</span>
            </p>
            <p class="sum">{{ tally.sum }}</p>
            <span class="bar" aria-hidden="true"
              ><span class="fill" [style.width.%]="tally.fill"></span
            ></span>
          }
        </li>
      }
    </ul>
  `,
  styleUrl: './server-card.scss',
})
export class ServerCard {
  readonly view = input.required<ServerView>();
  readonly setup = input.required<ServerSetup>();
  /** Whether score bars move; false when the visitor asked for reduced motion. */
  readonly animate = input(false);

  private readonly content = inject(LANDING_CONTENT);
  protected readonly demo = this.content.howItWorks.demo;

  protected readonly copy = computed(() => this.demo.servers[this.view().id]);

  /** "B 0.8 · C 0.5 · anyone else 0.0": the trust list, then everyone else. */
  protected readonly trust = computed(() => {
    const { trust, settings } = this.setup();
    const listed = PUBLISHERS.flatMap((publisher) => {
      const weight = trust[publisher];
      return weight === undefined ? [] : [`${this.short(publisher)} ${this.number(weight, 1)}`];
    });
    const others = fill(this.demo.labels.anyoneElse, {
      weight: this.number(settings.defaultWeight, 1),
    });
    return [...listed, others].join(' · ');
  });

  protected readonly rows = computed(() => this.view().decisions.map((d) => this.row(d)));

  private row(decision: DecisionView): Row {
    const { subject, state, cause, changed, contributions } = decision;
    const row: Row = {
      subject,
      name: this.demo.subjects[subject].name,
      address: SCENARIO.addresses[subject],
      state,
      stateLabel: this.demo.states[state],
      cause: state === 'unknown' ? '' : this.demo.causes[cause],
      changed,
    };
    if (contributions.length === 0) {
      return row;
    }
    const { threshold, quorum } = this.setup().settings;
    const sum = contributions
      .map(
        (c) =>
          `${this.short(c.publisher)} ${this.number(c.weight, 1)} × ${this.number(c.confidence, 1)}`,
      )
      .join(' + ');
    return {
      ...row,
      tally: {
        score: fill(this.demo.labels.score, {
          score: this.number(decision.score, 2),
          threshold: this.number(threshold, 2),
        }),
        reporters: fill(this.demo.labels.reporters, { count: decision.reporters, quorum }),
        sum,
        fill: Math.min(decision.score / (threshold * 1.5), 1) * 100,
      },
    };
  }

  private short(publisher: PublisherId): string {
    return publisher === 'rogue' ? this.demo.rogue.short : this.demo.servers[publisher].short;
  }

  private number(value: number, digits: number): string {
    return decimal(value, digits, this.content.meta.locale);
  }
}
