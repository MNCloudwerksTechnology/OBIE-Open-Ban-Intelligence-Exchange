import { DOCUMENT, Injectable, inject, signal } from '@angular/core';

import { CONSENT_STORAGE_KEY } from './analytics.config';

/** The visitor's answer to the question about visitor statistics. */
export type ConsentDecision = 'granted' | 'denied';

/**
 * The visitor's consent to visitor statistics (ADR 0034). Nothing is
 * measured without it. The answer is kept in local storage, so the question
 * is asked once and not on every page; storing it is strictly necessary to
 * respect it (§ 25(2) no. 2 TDDDG). Where storage is blocked, the answer
 * lasts for the current page.
 *
 * The dialog opens only in the browser and only after the first render
 * (`start`), so the prerendered page never contains it and hydration always
 * matches.
 */
@Injectable({ providedIn: 'root' })
export class ConsentService {
  private readonly window = inject(DOCUMENT).defaultView;
  private readonly decisionState = signal<ConsentDecision | null>(null);
  private readonly dialogState = signal(false);
  private started = false;
  /** Where focus returns after a dialog the visitor opened from the privacy settings. */
  private returnFocus: HTMLElement | null = null;

  /** The answer in effect; `null` until the visitor answers. */
  readonly decision = this.decisionState.asReadonly();

  /** Whether the dialog is open. */
  readonly dialogOpen = this.dialogState.asReadonly();

  /** Whether the visitor opened the dialog (then it takes focus) rather than the site. */
  readonly openedByVisitor = signal(false);

  /** Reads the stored answer and asks for one if there is none. Once, in the browser. */
  start(): void {
    if (this.started) {
      return;
    }
    this.started = true;
    const stored = this.read();
    this.decisionState.set(stored);
    this.dialogState.set(stored === null);
  }

  grant(): void {
    this.decide('granted');
  }

  deny(): void {
    this.decide('denied');
  }

  /** Opens the dialog again, from the privacy settings; focus returns to `opener` afterwards. */
  openSettings(opener: HTMLElement | null = null): void {
    this.returnFocus = opener;
    this.openedByVisitor.set(true);
    this.dialogState.set(true);
  }

  private decide(decision: ConsentDecision): void {
    this.decisionState.set(decision);
    this.write(decision);
    this.dialogState.set(false);
    this.returnFocus?.focus();
    this.returnFocus = null;
    this.openedByVisitor.set(false);
  }

  // Merely touching localStorage throws where the browser blocks site data.

  private read(): ConsentDecision | null {
    try {
      const value = this.window?.localStorage.getItem(CONSENT_STORAGE_KEY);
      return value === 'granted' || value === 'denied' ? value : null;
    } catch {
      return null;
    }
  }

  private write(decision: ConsentDecision): void {
    try {
      this.window?.localStorage.setItem(CONSENT_STORAGE_KEY, decision);
    } catch {
      // Blocked storage: the answer holds until the visitor leaves the page.
    }
  }
}
