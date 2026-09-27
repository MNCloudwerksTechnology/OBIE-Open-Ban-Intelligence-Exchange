import { Component } from '@angular/core';
import { RouterOutlet } from '@angular/router';

/** Application shell: every page renders into the main landmark. */
@Component({
  selector: 'app-root',
  imports: [RouterOutlet],
  template: `
    <main>
      <router-outlet />
    </main>
  `,
  styles: `
    main {
      max-width: var(--content-width);
      margin: 0 auto;
      padding: var(--space-4) var(--space-2);
    }
  `,
})
export class App {}
