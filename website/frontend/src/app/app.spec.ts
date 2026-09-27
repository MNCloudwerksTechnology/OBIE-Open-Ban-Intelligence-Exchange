import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';

import { App } from './app';
import { routes } from './app.routes';

describe('App routing', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [App], providers: [provideRouter(routes)] });
  });

  it('renders the home page at /', async () => {
    const harness = await RouterTestingHarness.create('/');
    expect(harness.routeNativeElement?.querySelector('h1')?.textContent).toBe('OBIE');
  });

  it('renders the not-found page for unknown URLs', async () => {
    const harness = await RouterTestingHarness.create('/does/not/exist');
    expect(harness.routeNativeElement?.querySelector('h1')?.textContent).toBe('Page not found');
  });

  it('renders the not-found page at its own prerendered path', async () => {
    const harness = await RouterTestingHarness.create('/404');
    expect(harness.routeNativeElement?.querySelector('h1')?.textContent).toBe('Page not found');
  });

  it('wraps pages in banner, main and content-info landmarks', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();
    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('header')).not.toBeNull();
    expect(root.querySelector('main')).not.toBeNull();
    expect(root.querySelector('footer')).not.toBeNull();
  });

  it('starts with a skip link that moves focus to the main landmark', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();
    const root = fixture.nativeElement as HTMLElement;
    document.body.appendChild(root);

    const skipLink = root.querySelector('a') as HTMLAnchorElement;
    expect(skipLink.classList).toContain('skip-link');
    expect(skipLink.getAttribute('href')).toBe('#main');

    skipLink.click();
    expect(document.activeElement).toBe(root.querySelector('main#main'));
    root.remove();
  });
});
