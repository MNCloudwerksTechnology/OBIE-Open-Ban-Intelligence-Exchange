import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Meta, Title } from '@angular/platform-browser';

import { LANDING_CONTENT } from '../../content/landing.content';
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
  imports: [Faq, Founder, GetStarted, Hero, HowItWorks, Principles, Problem, Status],
  templateUrl: './home.html',
})
export class Home {
  constructor() {
    const { meta } = inject(LANDING_CONTENT);
    inject(Title).setTitle(meta.title);
    inject(Meta).updateTag({ name: 'description', content: meta.description });
  }
}
