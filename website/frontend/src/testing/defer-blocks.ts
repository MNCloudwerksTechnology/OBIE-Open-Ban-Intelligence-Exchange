import { DeferBlockState } from '@angular/core/testing';
import type { ComponentFixture } from '@angular/core/testing';

/**
 * Renders every `@defer` block of the fixture, as the browser does once its
 * trigger fires (the landing page defers the inquiry form).
 */
export async function renderDeferBlocks(fixture: ComponentFixture<unknown>): Promise<void> {
  for (const block of await fixture.getDeferBlocks()) {
    await block.render(DeferBlockState.Complete);
  }
  fixture.detectChanges();
}
