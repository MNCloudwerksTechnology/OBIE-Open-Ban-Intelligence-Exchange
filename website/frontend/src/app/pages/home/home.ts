import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { LANDING_CONTENT } from '../../content/landing.content';
import { SEO_CONTENT } from '../../content/seo.content';
import { SeoService } from '../../core/seo';
import { homeStructuredData } from '../../core/structured-data';
import { PAGE_PATHS } from '../../i18n/languages';
import { LANG } from '../../i18n/provide-i18n';
import { Contact } from '../../sections/contact';
import { Faq } from '../../sections/faq';
import { Founder } from '../../sections/founder';
import { GetStarted } from '../../sections/get-started';
import { Hero } from '../../sections/hero';
import { HowItWorks } from '../../sections/how-it-works';
import { Principles } from '../../sections/principles';
import { Problem } from '../../sections/problem';
import { Status } from '../../sections/status';

/** The landing page: one page with anchored sections. */
@Component({
  selector: 'app-home',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Contact, Faq, Founder, GetStarted, Hero, HowItWorks, Principles, Problem, Status],
  templateUrl: './home.html',
})
export class Home {
  constructor() {
    const landing = inject(LANDING_CONTENT);
    const lang = inject(LANG);
    const seo = inject(SeoService);
    seo.apply({
      title: landing.meta.title,
      description: landing.meta.description,
      path: PAGE_PATHS.home[lang],
      structuredData: homeStructuredData(inject(SEO_CONTENT), landing, lang, (path) =>
        seo.absolute(path),
      ),
    });
  }
}
