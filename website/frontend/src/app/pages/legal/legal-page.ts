import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { ActivatedRoute } from '@angular/router';

import { LegalContent, LegalPageContent } from '../../content/legal-content.model';
import { LEGAL_CONTENT } from '../../content/legal.content';
import { SeoService } from '../../core/seo';

/** The legal pages; the route's `data.legalPage` names which one. */
export type LegalPageKey = 'impressum' | 'privacy';

/**
 * A legal page (Impressum or privacy policy): the review notice while the
 * operator has not approved the pages yet, then the heading with its German
 * legal term and the sections, all from the legal content file.
 */
@Component({
  selector: 'app-legal-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './legal-page.html',
  styleUrl: './legal-page.scss',
})
export class LegalPage {
  protected readonly content: LegalContent = inject(LEGAL_CONTENT);
  protected readonly page: LegalPageContent;

  constructor() {
    const key = inject(ActivatedRoute).snapshot.data['legalPage'] as LegalPageKey;
    this.page = this.content[key];
    inject(SeoService).apply({ ...this.page.meta, path: `/${key}` });
  }
}
