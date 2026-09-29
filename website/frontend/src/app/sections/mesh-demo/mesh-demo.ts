import { NgTemplateOutlet } from '@angular/common';
import {
  ChangeDetectionStrategy,
  ChangeDetectorRef,
  Component,
  DOCUMENT,
  DestroyRef,
  ElementRef,
  afterNextRender,
  computed,
  inject,
  signal,
} from '@angular/core';

import { DemoStepContent } from '../../content/landing-content.model';
import { LANDING_CONTENT } from '../../content/landing.content';
import { fill } from './format';
import { ServerId, ServerSetup } from './mesh-demo.model';
import { MeshMap } from './mesh-map';
import { replay } from './replay';
import { SCENARIO, SHARED_REPORT } from './scenario';
import { ServerCard } from './server-card';
import { SharedReport } from './shared-report';

// Autoplay shows a step long enough to read its text, or to hear it read
// out by a screen reader: 0.4 s per word, at least 8 s, and 6 s more for the
// step that takes the shared report apart.
const AUTOPLAY_MS_PER_WORD = 400;
const AUTOPLAY_MIN_MS = 8_000;
const AUTOPLAY_REPORT_MS = 6_000;

/** How long autoplay shows `step`; `withReport` for the step with the shared report. */
export function autoplayDelay(step: DemoStepContent, withReport: boolean): number {
  const words = [step.caption, step.note, step.planned].join(' ').trim().split(/\s+/).length;
  return (
    Math.max(AUTOPLAY_MIN_MS, words * AUTOPLAY_MS_PER_WORD) + (withReport ? AUTOPLAY_REPORT_MS : 0)
  );
}

const REDUCED_MOTION = '(prefers-reduced-motion: reduce)';

/** What the demo shows after each step; computed once, so no step can show a mixed state. */
const FRAMES = replay(SCENARIO);

/** Each server's settings and trust, by id. */
const SETUPS = Object.fromEntries(SCENARIO.servers.map((server) => [server.id, server])) as Record<
  ServerId,
  ServerSetup
>;

/** Where A shares the report that the demo takes apart: the step and the receivers. */
const SHARE = SCENARIO.steps.flatMap((step, index) =>
  step.events.flatMap((event) =>
    event.kind === 'share' && event.report === SHARED_REPORT ? [{ index, to: event.to }] : [],
  ),
)[0];

/**
 * The interactive three-node demo of "how it works" (ADR 0028). Prerendered
 * and without JavaScript it is an ordered list of the ten steps; once the
 * browser has rendered it, the visitor steps through it. Every step shows the
 * precomputed frame of the scenario, so jumping, going back and rapid clicks
 * always agree with stepping through one by one.
 */
@Component({
  selector: 'app-mesh-demo',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MeshMap, NgTemplateOutlet, ServerCard, SharedReport],
  templateUrl: './mesh-demo.html',
  styleUrl: './mesh-demo.scss',
  host: { '[attr.data-motion]': 'interactive() ? motion() : null' },
})
export class MeshDemo {
  private readonly content = inject(LANDING_CONTENT);
  private readonly document = inject(DOCUMENT);
  private readonly host: HTMLElement = inject(ElementRef).nativeElement;
  private readonly changeDetector = inject(ChangeDetectorRef);

  protected readonly demo = this.content.howItWorks.demo;
  protected readonly total = FRAMES.length;
  protected readonly setups = SETUPS;
  protected readonly sharedReport = SHARED_REPORT;
  protected readonly shareStep = SHARE.index;
  protected readonly receivers = SHARE.to;

  /** False when prerendered and until the browser has rendered the page. */
  protected readonly interactive = signal(false);
  protected readonly step = signal(0);
  protected readonly playing = signal(false);
  protected readonly motion = signal<'full' | 'reduced'>('reduced');
  /** Text of the polite live region. */
  protected readonly announcement = signal('');

  protected readonly frame = computed(() => FRAMES[this.step()]);
  protected readonly current = computed(() => this.demo.steps[this.step()]);
  protected readonly first = computed(() => this.step() === 0);
  protected readonly last = computed(() => this.step() === this.total - 1);
  protected readonly progress = computed(() =>
    fill(this.demo.controls.stepOf, { n: this.step() + 1, total: this.total }),
  );

  private timer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    const destroyRef = inject(DestroyRef);
    destroyRef.onDestroy(() => this.stopTimer());
    afterNextRender(() => {
      this.becomeInteractive();
      this.followMotionPreference(destroyRef);
      this.pauseWhenOutOfSight(destroyRef);
    });
  }

  protected stepLabel(index: number): string {
    return fill(this.demo.controls.goTo, { n: index + 1, title: this.demo.steps[index].title });
  }

  protected next(): void {
    if (!this.last()) {
      this.navigate(this.step() + 1);
    }
  }

  protected previous(): void {
    if (!this.first()) {
      this.navigate(this.step() - 1);
    }
  }

  protected restart(): void {
    this.navigate(0);
  }

  protected jump(index: number): void {
    this.navigate(index);
  }

  protected togglePlay(): void {
    if (this.playing()) {
      this.pause();
    } else {
      this.play();
    }
  }

  /** Arrow keys on the controls move one step, like Previous and Next. */
  protected arrow(event: Event, direction: 1 | -1): void {
    event.preventDefault();
    if (direction === 1) {
      this.next();
    } else {
      this.previous();
    }
  }

  /** A step the visitor chose: autoplay stops, the visitor has taken over. */
  private navigate(index: number): void {
    this.pause();
    this.show(index);
  }

  private show(index: number): void {
    const { title, caption } = this.demo.steps[index];
    this.step.set(index);
    this.announcement.set(
      fill(this.demo.controls.announcement, { n: index + 1, total: this.total, title, caption }),
    );
  }

  /** Autoplay starts only here, when the visitor asks for it; from the last step it starts over. */
  private play(): void {
    if (this.last()) {
      this.show(0);
    }
    this.playing.set(true);
    this.schedule();
  }

  private pause(): void {
    this.playing.set(false);
    this.stopTimer();
  }

  private schedule(): void {
    this.stopTimer();
    const index = this.step();
    const delay = autoplayDelay(this.demo.steps[index], index === this.shareStep);
    this.timer = setTimeout(() => this.advance(), delay);
  }

  private advance(): void {
    this.show(this.step() + 1);
    if (this.last()) {
      this.pause();
    } else {
      this.schedule();
    }
  }

  private stopTimer(): void {
    clearTimeout(this.timer);
    this.timer = undefined;
  }

  /**
   * Swaps the prerendered list for the interactive demo, which has another
   * height. When the demo is above the viewport, e.g. after a link to
   * /#contact, the page scrolls by the difference, so that what the visitor
   * looks at stays in place (browsers without scroll anchoring would move it).
   */
  private becomeInteractive(): void {
    const bottom = this.host.getBoundingClientRect().bottom;
    this.interactive.set(true);
    this.changeDetector.detectChanges();
    const shift = this.host.getBoundingClientRect().bottom - bottom;
    if (bottom <= 0 && shift !== 0) {
      this.document.defaultView?.scrollBy(0, shift);
    }
  }

  private followMotionPreference(destroyRef: DestroyRef): void {
    const apply = (reduce: boolean) => this.motion.set(reduce ? 'reduced' : 'full');
    const query = this.document.defaultView?.matchMedia?.(REDUCED_MOTION);
    if (!query) {
      apply(false);
      return;
    }
    apply(query.matches);
    const onChange = (event: MediaQueryListEvent) => apply(event.matches);
    query.addEventListener('change', onChange);
    destroyRef.onDestroy(() => query.removeEventListener('change', onChange));
  }

  /** Autoplay pauses when the demo leaves the viewport or the tab is hidden. */
  private pauseWhenOutOfSight(destroyRef: DestroyRef): void {
    const onVisibility = () => {
      if (this.document.visibilityState === 'hidden') {
        this.pause();
      }
    };
    this.document.addEventListener('visibilitychange', onVisibility);
    destroyRef.onDestroy(() => this.document.removeEventListener('visibilitychange', onVisibility));

    const Observer = this.document.defaultView?.IntersectionObserver;
    if (Observer) {
      const observer = new Observer((entries) => {
        if (entries.some((entry) => !entry.isIntersecting)) {
          this.pause();
        }
      });
      observer.observe(this.host);
      destroyRef.onDestroy(() => observer.disconnect());
    }
  }
}
