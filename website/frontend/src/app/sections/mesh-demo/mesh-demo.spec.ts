import { ComponentFixture, TestBed } from '@angular/core/testing';
import axe from 'axe-core';

import { renderPrerendered } from '../../../testing/prerender';
import { stubMediaQueries } from '../../../testing/system-theme';
import { LANDING_CONTENT_EN, REPOSITORY_URL } from '../../content/landing.content';
import { MeshDemo, autoplayDelay } from './mesh-demo';
import { replay } from './replay';
import { SCENARIO } from './scenario';

const demo = LANDING_CONTENT_EN.howItWorks.demo;
const FRAMES = replay(SCENARIO);
const LAST = FRAMES.length - 1;
/** How long autoplay shows step `index` (the shared report is on step 3). */
const delay = (index: number) => autoplayDelay(demo.steps[index], index === 2);
/** Longer than any step. */
const AGES = 10 * 60_000;

/** An IntersectionObserver the test drives (jsdom has none). */
class FakeObserver {
  static latest: FakeObserver | undefined;
  readonly observe = vi.fn();
  readonly disconnect = vi.fn();

  constructor(private readonly callback: (entries: { isIntersecting: boolean }[]) => void) {
    FakeObserver.latest = this;
  }

  /** The demo scrolls out of view. */
  leave(): void {
    this.callback([{ isIntersecting: false }]);
  }
}

describe('MeshDemo', () => {
  let fixture: ComponentFixture<MeshDemo>;
  let host: HTMLElement;

  async function render(): Promise<void> {
    TestBed.configureTestingModule({ imports: [MeshDemo] });
    fixture = TestBed.createComponent(MeshDemo);
    host = fixture.nativeElement as HTMLElement;
    await fixture.whenStable();
  }

  function press(button: HTMLElement): void {
    button.click();
    fixture.detectChanges();
  }

  function control(label: string): HTMLButtonElement {
    const found = Array.from(host.querySelectorAll<HTMLButtonElement>('.buttons button')).find(
      (button) => button.textContent?.trim() === label,
    );
    if (!found) {
      throw new Error(`no control labelled ${label}`);
    }
    return found;
  }

  const dots = () => Array.from(host.querySelectorAll<HTMLButtonElement>('.steps button'));
  const current = () => dots().findIndex((dot) => dot.getAttribute('aria-current') === 'step');
  const title = () => host.querySelector('.narrative .title')?.textContent?.trim();
  const announced = () => host.querySelector('[aria-live="polite"]')?.textContent?.trim();
  const cards = () => Array.from(host.querySelectorAll('app-server-card'));
  const states = () =>
    cards().map((card) =>
      Array.from(card.querySelectorAll('.row')).map((row) => row.getAttribute('data-state')),
    );
  const expectedStates = (index: number) =>
    FRAMES[index].servers.map((server) => server.decisions.map((d) => d.state));
  /** A row's paragraphs, each as it reads, one after another. */
  const row = (server: number, subject: number) =>
    Array.from(cards()[server].querySelectorAll('.row')[subject].querySelectorAll('p'))
      .map((p) => p.textContent?.replace(/\s+/g, ' ').trim())
      .join(' ');

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(document, 'visibilityState');
    FakeObserver.latest = undefined;
  });

  describe('navigation', () => {
    beforeEach(render);

    it('starts at the first step, labelled as an illustration, and announces nothing yet', () => {
      expect(current()).toBe(0);
      expect(title()).toBe(demo.steps[0].title);
      expect(host.querySelector('.progress')?.textContent).toBe('Step 1 of 10');
      expect(host.querySelector('.illustration')?.textContent).toBe(demo.illustration);
      expect(announced()).toBe('');
      expect(control('Previous').getAttribute('aria-disabled')).toBe('true');
      expect(control('Next').getAttribute('aria-disabled')).toBeNull();
    });

    it('offers its controls as a labelled group with one button per step', () => {
      const toolbar = host.querySelector('.toolbar');
      expect(toolbar?.getAttribute('role')).toBe('group');
      expect(toolbar?.getAttribute('aria-label')).toBe('Demo controls');
      expect(dots().map((dot) => dot.querySelector('.visually-hidden')?.textContent)).toEqual(
        demo.steps.map((step, i) => `Step ${i + 1}: ${step.title}`),
      );
      expect(dots().every((dot) => dot.type === 'button')).toBe(true);
      expect(host.querySelector('app-mesh-map svg')?.getAttribute('aria-hidden')).toBe('true');
    });

    it('moves forward and back with Next and Previous and announces every step', () => {
      press(control('Next'));
      expect(current()).toBe(1);
      expect(title()).toBe(demo.steps[1].title);
      expect(announced()).toBe(`Step 2 of 10: ${demo.steps[1].title}. ${demo.steps[1].caption}`);

      press(control('Next'));
      press(control('Previous'));
      expect(current()).toBe(1);
      expect(announced()).toContain('Step 2 of 10');
    });

    it('jumps to any step and restarts at the first', () => {
      press(dots()[6]);
      expect(current()).toBe(6);
      expect(title()).toBe(demo.steps[6].title);

      press(control('Restart'));
      expect(current()).toBe(0);
      expect(announced()).toContain(`Step 1 of 10: ${demo.steps[0].title}.`);
    });

    it('stays put at either end, keeping focus on the pressed button', () => {
      control('Previous').focus();
      press(control('Previous'));
      expect(current()).toBe(0);

      press(dots()[LAST]);
      const next = control('Next');
      next.focus();
      press(next);
      expect(current()).toBe(LAST);
      expect(next.getAttribute('aria-disabled')).toBe('true');
      expect(document.activeElement).toBe(next);
    });

    it('moves with the arrow keys on the controls', () => {
      const next = control('Next');
      next.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
      fixture.detectChanges();
      expect(current()).toBe(1);
      next.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
      fixture.detectChanges();
      expect(current()).toBe(0);
    });
  });

  describe('what each server decides', () => {
    beforeEach(render);

    it('shows, at every step, every subject on every server as the decision rule has it', () => {
      for (let index = 0; index <= LAST; index++) {
        press(dots()[index]);
        expect(states(), `step ${index + 1}`).toEqual(expectedStates(index));
        for (const card of cards()) {
          for (const r of Array.from(card.querySelectorAll('.row'))) {
            const state = r.getAttribute('data-state') as keyof typeof demo.states;
            expect(r.querySelector('.state')?.textContent).toBe(demo.states[state]);
          }
        }
      }
    });

    it('shows the score against the threshold and the reporters where a server decides', () => {
      press(dots()[3]);
      expect(row(1, 0)).toBe(
        'Password bot 203.0.113.7 Watching, not blocked below the bar Score 0.64 of 1.20 needed 1 of 2 reporters A 0.8 × 0.8',
      );

      press(dots()[5]);
      expect(row(2, 0)).toBe(
        'Password bot 203.0.113.7 Blocked enough trusted reporters agree Changed Score 1.28 of 1.20 needed 2 of 2 reporters A 0.8 × 0.8 + B 0.8 × 0.8',
      );

      press(dots()[7]);
      expect(row(0, 3)).toBe(
        'Payment service 192.0.2.80 Never blocked on the safety list Changed Score 0.00 of 1.20 needed 0 of 2 reporters R 0.0 × 1.0',
      );
      expect(row(1, 3)).toContain('Watching, not blocked below the bar Changed Score 0.00');
    });

    it('lists whom each server trusts and what its safety list holds', () => {
      const setup = (i: number) =>
        Array.from(cards()[i].querySelectorAll('.setup dt, .setup dd')).map((item) =>
          item.textContent?.trim(),
        );
      expect(setup(0)).toEqual([
        'Trusts',
        'B 0.8 · C 0.5 · anyone else 0.0',
        'Safety list',
        'own networks, payment service',
      ]);
      expect(setup(2)[1]).toBe('A 0.8 · B 0.8 · anyone else 0.0');
    });

    it('shows the same state after jumping straight to a late step as after stepping through', () => {
      for (let i = 0; i < 8; i++) {
        press(control('Next'));
      }
      const steppedThrough = host.querySelector('.servers')?.textContent;
      press(control('Restart'));
      press(dots()[8]);
      expect(host.querySelector('.servers')?.textContent).toBe(steppedThrough);
    });

    it('brings back the withdrawn report and its block when going back from step 9', () => {
      press(dots()[8]);
      expect(states().map((server) => server[1])).toEqual(['unknown', 'unknown', 'unknown']);

      press(control('Previous'));
      expect(states().map((server) => server[1])).toEqual(['blocked', 'watching', 'watching']);
      expect(row(0, 1)).toContain('Blocked own detection');
    });

    it('never mixes two steps, however fast the visitor clicks', () => {
      for (let i = 0; i < 25; i++) {
        control('Next').click();
      }
      dots()[3].click();
      control('Previous').click();
      fixture.detectChanges();

      expect(current()).toBe(2);
      expect(host.querySelectorAll('[aria-current="step"]').length).toBe(1);
      expect(host.querySelectorAll('.narrative').length).toBe(1);
      expect(title()).toBe(demo.steps[2].title);
      expect(states()).toEqual(expectedStates(2));
    });
  });

  describe('story', () => {
    beforeEach(render);

    it('takes the shared report apart at step 3 only', () => {
      press(dots()[2]);
      const card = host.querySelector('app-shared-report');
      const text = card?.textContent?.replace(/\s+/g, ' ');
      for (const part of [
        'What A shares',
        '203.0.113.7',
        'password_bruteforce',
        'password guessing',
        '47 failed logins',
        'sha256:9f2c…41e7',
        'block for 7 days',
        'Ed25519, checked by B and C',
        'What stays on A',
        ...demo.report.kept,
      ]) {
        expect(text).toContain(part);
      }
      press(dots()[3]);
      expect(host.querySelector('app-shared-report')).toBeNull();
    });

    it('draws every message straight from sender to receiver, and the flood from the rogue', () => {
      for (let index = 0; index <= LAST; index++) {
        press(dots()[index]);
        const receivers = FRAMES[index].messages.reduce((sum, m) => sum + m.to.length, 0);
        expect(host.querySelectorAll('.message').length, `step ${index + 1}`).toBe(receivers);
      }
      press(dots()[7]);
      expect(host.querySelectorAll('.message--untrusted').length).toBe(3);
      expect(host.querySelector('.copies')?.textContent).toBe('×50');
    });

    it('turns the bot away at C at step 6, the only server that newly blocks', () => {
      press(dots()[5]);
      expect(host.querySelectorAll('.attack--turned-away').length).toBe(1);
      expect(host.querySelector('.attack--turned-away')?.textContent).toContain(
        'bot · turned away',
      );
      const blocking = Array.from(host.querySelectorAll('.node--blocking .letter'));
      expect(blocking.map((letter) => letter.textContent?.trim())).toEqual(['C']);
    });

    it('marks later features as planned', () => {
      press(dots()[0]);
      expect(host.querySelector('.narrative .planned')?.textContent).toContain('Planned');
      expect(host.querySelector('.narrative .planned')?.textContent).toContain(
        demo.steps[0].planned,
      );
    });

    it('closes with the three takeaways and links to GitHub and to getting started', () => {
      press(dots()[LAST]);
      const takeaways = Array.from(host.querySelectorAll('.takeaways li'));
      expect(takeaways.map((li) => li.textContent?.trim())).toEqual([...demo.recap.takeaways]);
      const links = Array.from(host.querySelectorAll<HTMLAnchorElement>('.actions a'));
      expect(links.map((a) => [a.textContent?.trim(), a.getAttribute('href')])).toEqual([
        ['View on GitHub', REPOSITORY_URL],
        ['Get started', '#get-started'],
      ]);
      expect(links[0].getAttribute('rel')).toBe('noopener');
    });

    it('passes the automated accessibility check at every step (axe, WCAG 2.1 AA)', async () => {
      for (let index = 0; index <= LAST; index++) {
        press(dots()[index]);
        const results = await axe.run(host, {
          runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] },
          resultTypes: ['violations'],
        });
        const found = results.violations.map((v) => `${v.id}: ${v.nodes.length}`);
        expect(found, `step ${index + 1}`).toEqual([]);
      }
    });

    it('keeps all ten steps readable as text', () => {
      const transcript = host.querySelector('details.transcript');
      expect(transcript?.querySelector('summary')?.textContent?.trim()).toBe(
        demo.controls.transcript,
      );
      expect(transcript?.querySelectorAll('ol.story > li').length).toBe(10);
    });
  });

  describe('autoplay', () => {
    it('never starts on its own', async () => {
      await render();
      vi.useFakeTimers();
      vi.advanceTimersByTime(AGES);
      fixture.detectChanges();
      expect(current()).toBe(0);
      expect(control('Play')).toBeTruthy();
    });

    it('shows one step after another once started and stops at the last step', async () => {
      await render();
      vi.useFakeTimers();
      press(control('Play'));
      expect(control('Pause')).toBeTruthy();

      vi.advanceTimersByTime(delay(0) - 1);
      fixture.detectChanges();
      expect(current()).toBe(0);
      vi.advanceTimersByTime(1);
      fixture.detectChanges();
      expect(current()).toBe(1);
      expect(announced()).toContain('Step 2 of 10');

      vi.advanceTimersByTime(delay(1) + delay(2) - 1);
      fixture.detectChanges();
      expect(current()).toBe(2);
      vi.advanceTimersByTime(1);
      fixture.detectChanges();
      expect(current()).toBe(3);

      vi.advanceTimersByTime(AGES);
      fixture.detectChanges();
      expect(current()).toBe(LAST);
      expect(control('Play')).toBeTruthy();
    });

    it('starts over when played from the last step', async () => {
      await render();
      vi.useFakeTimers();
      press(dots()[LAST]);
      press(control('Play'));
      expect(current()).toBe(0);
      expect(control('Pause')).toBeTruthy();
    });

    it('pauses on Pause and whenever the visitor picks a step', async () => {
      await render();
      vi.useFakeTimers();
      press(control('Play'));
      press(control('Pause'));
      vi.advanceTimersByTime(AGES);
      fixture.detectChanges();
      expect(current()).toBe(0);

      press(control('Play'));
      press(dots()[4]);
      expect(control('Play')).toBeTruthy();
      vi.advanceTimersByTime(AGES);
      fixture.detectChanges();
      expect(current()).toBe(4);
    });

    it('pauses when the tab is hidden', async () => {
      await render();
      vi.useFakeTimers();
      press(control('Play'));
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
      document.dispatchEvent(new Event('visibilitychange'));
      fixture.detectChanges();

      expect(control('Play')).toBeTruthy();
      vi.advanceTimersByTime(AGES);
      fixture.detectChanges();
      expect(current()).toBe(0);
    });

    it('pauses when the demo scrolls out of view', async () => {
      vi.stubGlobal('IntersectionObserver', FakeObserver);
      await render();
      expect(FakeObserver.latest?.observe).toHaveBeenCalledWith(host);
      vi.useFakeTimers();
      press(control('Play'));
      FakeObserver.latest?.leave();
      fixture.detectChanges();

      expect(control('Play')).toBeTruthy();
      vi.advanceTimersByTime(AGES);
      fixture.detectChanges();
      expect(current()).toBe(0);
    });
  });

  describe('autoplay timing', () => {
    it('shows a step long enough to read it: 0.4 s per word, at least 8 s', () => {
      const words = (text = '') => (text ? text.trim().split(/\s+/).length : 0);
      demo.steps.forEach((step, index) => {
        const count = words(step.caption) + words(step.note) + words(step.planned);
        const expected = Math.max(8_000, count * 400) + (index === 2 ? 6_000 : 0);
        expect(delay(index), step.title).toBe(expected);
      });
      expect(delay(9)).toBe(8_000);
      expect(delay(2)).toBeGreaterThan(delay(3) - 1);
    });
  });

  describe('switching from the prerendered list', () => {
    function stubDemoBox(bottoms: number[]): ReturnType<typeof vi.fn> {
      const original = HTMLElement.prototype.getBoundingClientRect;
      let call = 0;
      vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
        this: HTMLElement,
      ) {
        // The demo's host (a <div> under TestBed): it holds the list or the toolbar.
        if (!this.querySelector(':scope > ol.story, :scope > .toolbar')) {
          return original.call(this);
        }
        const bottom = bottoms[Math.min(call++, bottoms.length - 1)];
        return { bottom, top: bottom - 100 } as DOMRect;
      });
      const scrollBy = vi.fn();
      vi.stubGlobal('scrollBy', scrollBy);
      return scrollBy;
    }

    afterEach(() => vi.restoreAllMocks());

    it('keeps what the visitor looks at in place when the demo above it changes height', async () => {
      // A link to /#contact: the demo is above the viewport and gets 466 px shorter.
      const scrollBy = stubDemoBox([-300, -766]);
      await render();
      expect(host.querySelector('.toolbar')).not.toBeNull();
      expect(scrollBy).toHaveBeenCalledExactlyOnceWith(0, -466);
    });

    it('does not scroll when the visitor can see the demo, or nothing moved', async () => {
      const scrollBy = stubDemoBox([400, 120]);
      await render();
      expect(scrollBy).not.toHaveBeenCalled();
    });
  });

  describe('motion', () => {
    it('changes steps without animation when the visitor asks for reduced motion', async () => {
      stubMediaQueries({ reducedMotion: true });
      await render();
      press(dots()[2]);
      expect(host.getAttribute('data-motion')).toBe('reduced');
      expect(host.querySelectorAll('.animated').length).toBe(0);
      // The arrows still show who sends what to whom: A's two reports to B and C.
      expect(host.querySelectorAll('.message line').length).toBe(4);
    });

    it('animates otherwise, and stops as soon as the visitor asks for reduced motion', async () => {
      const { reducedMotion } = stubMediaQueries({ reducedMotion: false });
      await render();
      press(dots()[2]);
      expect(host.getAttribute('data-motion')).toBe('full');
      expect(host.querySelector('app-mesh-map svg.animated')).not.toBeNull();
      expect(host.querySelector('.narrative.animated')).not.toBeNull();

      reducedMotion.change(true);
      fixture.detectChanges();
      expect(host.getAttribute('data-motion')).toBe('reduced');
      expect(host.querySelectorAll('.animated').length).toBe(0);

      expect(reducedMotion.listenerCount).toBe(1);
      fixture.destroy();
      expect(reducedMotion.listenerCount).toBe(0);
    });
  });

  describe('without JavaScript', () => {
    it('tells the same story as an ordered list of the ten steps with their captions', async () => {
      const page = await renderPrerendered(MeshDemo);

      expect(page.querySelector('.toolbar, button, app-mesh-map, app-server-card')).toBeNull();
      expect(page.hasAttribute('data-motion')).toBe(false);
      expect(page.querySelector('.illustration')?.textContent).toBe(demo.illustration);
      const items = Array.from(page.querySelectorAll(':scope > ol.story > li'));
      expect(items.length).toBe(10);
      items.forEach((item, i) => {
        expect(item.querySelector('strong')?.textContent).toBe(demo.steps[i].title);
        expect(item.querySelector('p')?.textContent).toBe(demo.steps[i].caption);
      });
      expect(page.querySelectorAll('ol.story .planned').length).toBe(
        demo.steps.filter((step) => step.planned).length,
      );
    });
  });
});
