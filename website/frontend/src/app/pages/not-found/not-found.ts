import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { RouterLink } from '@angular/router';

import { SEO_CONTENT } from '../../content/seo.content';
import { SeoService } from '../../core/seo';

/** Route data of the not-found page, so visitor statistics can tell it apart. */
export const NOT_FOUND_DATA = { notFound: true } as const;

/** Page shown for unknown URLs, both by the client router and the back end; never indexed. */
@Component({
  selector: 'app-not-found',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  templateUrl: './not-found.html',
})
export class NotFound {
  protected readonly copy = inject(SEO_CONTENT).notFound;

  constructor() {
    inject(SeoService).apply({
      title: this.copy.title,
      description: this.copy.description,
      path: null,
    });
  }
}
