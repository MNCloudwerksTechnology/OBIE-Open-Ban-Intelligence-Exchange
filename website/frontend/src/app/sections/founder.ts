import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LANDING_CONTENT } from '../content/landing.content';

/**
 * The founder: photo, role, bio, proposed talk topics and profile links, and
 * the invitation that opens the inquiry form. Every fact comes from the
 * content file, where the operator fills in what is still a placeholder.
 */
@Component({
  selector: 'app-founder',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="section" [id]="founder.id" aria-labelledby="founder-heading">
      <div class="container">
        <p class="eyebrow"><span>06</span>{{ founder.label }}</p>
        <h2 class="section-heading" id="founder-heading">{{ founder.heading }}</h2>
        <div class="profile">
          <img
            class="photo"
            [src]="founder.photo.src"
            [alt]="founder.photo.alt"
            width="240"
            height="240"
            loading="lazy"
          />
          <div class="about">
            <h3 class="name">{{ founder.name }}</h3>
            <p class="role">{{ founder.role }}</p>
            <p class="bio">{{ founder.bio }}</p>
            @if (founder.links.length) {
              <ul class="links" [attr.aria-label]="founder.linksLabel">
                @for (link of founder.links; track link.href) {
                  <li>
                    <a [href]="link.href" rel="noopener">{{ link.label }}</a>
                  </li>
                }
              </ul>
            }
          </div>
        </div>
        <div class="topics card">
          <h3>{{ founder.topicsHeading }}</h3>
          <p class="note">{{ founder.topicsNote }}</p>
          <ul>
            @for (topic of founder.topics; track topic) {
              <li>{{ topic }}</li>
            }
          </ul>
        </div>
        <p class="invite">
          <a class="button button--primary" [href]="founder.invite.href">{{
            founder.invite.label
          }}</a>
        </p>
        <a class="next-step" [href]="founder.nextStep.href" rel="noopener">{{
          founder.nextStep.label
        }}</a>
      </div>
    </section>
  `,
  styles: `
    .profile {
      display: grid;
      gap: var(--space-5);
      align-items: start;
      max-width: 52rem;
      margin-top: var(--space-6);
    }

    .photo {
      width: 8rem;
      height: 8rem;
      border: 1px solid var(--color-border);
      border-radius: var(--radius-lg);
      object-fit: cover;
    }

    .name {
      font-size: var(--text-xl);
    }

    .role {
      margin-top: var(--space-1);
      color: var(--color-accent-text);
      font-size: var(--text-sm);
      font-weight: 600;
    }

    .bio {
      max-width: 62ch;
      margin-top: var(--space-4);
      color: var(--color-text-muted);
    }

    .links {
      display: flex;
      flex-wrap: wrap;
      gap: var(--space-2) var(--space-5);
      margin: var(--space-4) 0 0;
      padding: 0;
      list-style: none;
      font-weight: 600;
    }

    .links a {
      display: inline-block;
      padding-block: var(--space-2);
    }

    .topics {
      max-width: 52rem;
      margin-top: var(--space-6);
    }

    .topics .note {
      font-size: var(--text-sm);
    }

    .topics ul {
      display: grid;
      gap: var(--space-2);
      margin: var(--space-4) 0 0;
      padding-left: var(--space-5);
    }

    .invite {
      margin-top: var(--space-6);
    }

    .invite + .next-step {
      margin-top: var(--space-5);
    }

    @media (min-width: 40rem) {
      .profile {
        grid-template-columns: auto 1fr;
        gap: var(--space-6);
      }

      .photo {
        width: 10rem;
        height: 10rem;
      }
    }
  `,
})
export class Founder {
  protected readonly founder = inject(LANDING_CONTENT).founder;
}
