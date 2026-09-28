import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { RouterLink } from '@angular/router';

import { SEO_CONTENT } from '../../content/seo.content';
import { SeoService } from '../../core/seo';

/** Page shown for unknown URLs, both by the client router and the back end; never indexed. */
@Component({
  selector: 'app-not-found',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  templateUrl: './not-found.html',
})
export class NotFound {
  constructor() {
    inject(SeoService).apply({ ...inject(SEO_CONTENT).notFound, path: null });
  }
}
