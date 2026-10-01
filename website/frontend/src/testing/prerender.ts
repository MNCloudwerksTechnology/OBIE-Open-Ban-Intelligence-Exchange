import { Type } from '@angular/core';
import { TestBed } from '@angular/core/testing';

type ServerModeGlobal = typeof globalThis & { ngServerMode?: boolean };

/**
 * Renders `component` the way the build prerenders it (`ngServerMode`):
 * `afterNextRender` callbacks never run, so the result is the HTML a visitor
 * without JavaScript, or a search engine, receives.
 */
export async function renderPrerendered<T>(component: Type<T>): Promise<HTMLElement> {
  const scope = globalThis as ServerModeGlobal;
  scope.ngServerMode = true;
  try {
    TestBed.configureTestingModule({ imports: [component] });
    const fixture = TestBed.createComponent(component);
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  } finally {
    delete scope.ngServerMode;
  }
}
