import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { stubSystemTheme } from '../../testing/system-theme';
import { ThemeToggle } from './theme-toggle';

describe('ThemeToggle', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    document.documentElement.removeAttribute('data-theme');
  });

  function render(): HTMLButtonElement {
    const fixture = TestBed.createComponent(ThemeToggle);
    fixture.autoDetectChanges();
    return (fixture.nativeElement as HTMLElement).querySelector('button') as HTMLButtonElement;
  }

  it('offers the dark theme when the system theme is light', () => {
    stubSystemTheme(false);
    expect(render().getAttribute('aria-label')).toBe('Switch to dark theme');
  });

  it('offers the light theme when the system theme is dark', () => {
    stubSystemTheme(true);
    expect(render().getAttribute('aria-label')).toBe('Switch to light theme');
  });

  it('switches the theme on click and updates its label', async () => {
    stubSystemTheme(false);
    const button = render();

    button.click();
    await TestBed.inject(ApplicationRef).whenStable();
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(button.getAttribute('aria-label')).toBe('Switch to light theme');
  });

  it('is a real button, so it works with the keyboard', () => {
    stubSystemTheme(false);
    const button = render();
    expect(button.type).toBe('button');
    expect(button.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
  });
});
