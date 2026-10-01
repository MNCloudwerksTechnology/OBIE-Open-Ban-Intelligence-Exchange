import { DOCUMENT } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { stubSystemTheme } from '../../testing/system-theme';
import { ThemeService } from './theme.service';

describe('ThemeService', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    document.documentElement.removeAttribute('data-theme');
  });

  it('follows a light system theme', () => {
    stubSystemTheme(false);
    expect(TestBed.inject(ThemeService).theme()).toBe('light');
  });

  it('follows a dark system theme', () => {
    stubSystemTheme(true);
    expect(TestBed.inject(ThemeService).theme()).toBe('dark');
  });

  it('follows changes of the system theme', () => {
    const query = stubSystemTheme(false);
    const service = TestBed.inject(ThemeService);

    query.change(true);
    expect(service.theme()).toBe('dark');
    query.change(false);
    expect(service.theme()).toBe('light');
  });

  it('does not touch the page while following the system theme', () => {
    stubSystemTheme(true);
    TestBed.inject(ThemeService);
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('toggles to the other theme and marks it on the root element', () => {
    stubSystemTheme(false);
    const service = TestBed.inject(ThemeService);

    service.toggle();
    expect(service.theme()).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');

    service.toggle();
    expect(service.theme()).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });

  it('keeps the chosen theme when the system theme changes', () => {
    const query = stubSystemTheme(true);
    const service = TestBed.inject(ThemeService);

    service.toggle();
    query.change(false);
    query.change(true);
    expect(service.theme()).toBe('light');
  });

  it('stores nothing in the browser', () => {
    stubSystemTheme(false);
    TestBed.inject(ThemeService).toggle();
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    expect(document.cookie).toBe('');
  });

  it('falls back to light where matchMedia is missing, as when prerendering', () => {
    const windowWithoutMatchMedia = { matchMedia: undefined };
    TestBed.configureTestingModule({
      providers: [
        {
          provide: DOCUMENT,
          useValue: {
            defaultView: windowWithoutMatchMedia,
            documentElement: document.createElement('html'),
          },
        },
      ],
    });
    expect(TestBed.inject(ThemeService).theme()).toBe('light');
  });
});
