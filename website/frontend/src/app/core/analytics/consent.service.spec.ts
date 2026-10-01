import { TestBed } from '@angular/core/testing';

import { CONSENT_STORAGE_KEY } from './analytics.config';
import { ConsentService } from './consent.service';

describe('ConsentService', () => {
  beforeEach(() => localStorage.removeItem(CONSENT_STORAGE_KEY));
  afterEach(() => {
    localStorage.removeItem(CONSENT_STORAGE_KEY);
    vi.restoreAllMocks();
  });

  it('asks nothing until started, so the prerendered page never shows the question', () => {
    const consent = TestBed.inject(ConsentService);
    expect(consent.dialogOpen()).toBe(false);
    expect(consent.decision()).toBeNull();
  });

  it('asks on the first visit and keeps the answer, so it asks only once', () => {
    const consent = TestBed.inject(ConsentService);
    consent.start();
    expect(consent.dialogOpen()).toBe(true);

    consent.grant();

    expect(consent.dialogOpen()).toBe(false);
    expect(consent.decision()).toBe('granted');
    expect(localStorage.getItem(CONSENT_STORAGE_KEY)).toBe('granted');
  });

  it.each(['granted', 'denied'] as const)('does not ask again after "%s"', (answer) => {
    localStorage.setItem(CONSENT_STORAGE_KEY, answer);
    const consent = TestBed.inject(ConsentService);
    consent.start();
    expect(consent.dialogOpen()).toBe(false);
    expect(consent.decision()).toBe(answer);
  });

  it('ignores a stored value it does not know and asks again', () => {
    localStorage.setItem(CONSENT_STORAGE_KEY, 'yes please');
    const consent = TestBed.inject(ConsentService);
    consent.start();
    expect(consent.dialogOpen()).toBe(true);
    expect(consent.decision()).toBeNull();
  });

  it('lets the visitor change the answer from the privacy settings, then returns focus', () => {
    localStorage.setItem(CONSENT_STORAGE_KEY, 'granted');
    const consent = TestBed.inject(ConsentService);
    consent.start();
    const opener = document.createElement('button');
    document.body.appendChild(opener);

    consent.openSettings(opener);
    expect(consent.dialogOpen()).toBe(true);
    expect(consent.openedByVisitor()).toBe(true);
    consent.deny();

    expect(consent.decision()).toBe('denied');
    expect(localStorage.getItem(CONSENT_STORAGE_KEY)).toBe('denied');
    expect(document.activeElement).toBe(opener);
    expect(consent.openedByVisitor()).toBe(false);
    opener.remove();
  });

  it('still works for the current page where the browser blocks storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    const consent = TestBed.inject(ConsentService);
    consent.start();
    expect(consent.dialogOpen()).toBe(true);

    consent.deny();

    expect(consent.decision()).toBe('denied');
    expect(consent.dialogOpen()).toBe(false);
  });
});
