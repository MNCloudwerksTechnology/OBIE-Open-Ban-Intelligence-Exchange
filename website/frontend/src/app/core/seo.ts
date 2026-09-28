import { DOCUMENT, Injectable, InjectionToken, PLATFORM_ID, inject } from '@angular/core';
import { isPlatformBrowser } from '@angular/common';
import { Meta, Title } from '@angular/platform-browser';

import { SEO_CONTENT } from '../content/seo.content';

/**
 * Origin written into the prerendered pages in place of the real one. The
 * site's origin is deployment configuration (`OBIE_SITE_ORIGIN`), unknown at
 * build time; the back end replaces this string when it serves a page
 * (ADR 0015). Keep it in sync with `SiteOrigin.PLACEHOLDER` in the back end.
 */
export const SITE_ORIGIN_PLACEHOLDER = 'https://site-origin.invalid';

/**
 * The site's origin for absolute URLs (canonical link, share image). While
 * prerendering it is the placeholder; in the browser it is taken from the
 * canonical link the back end filled in, so a later client-side navigation
 * keeps the configured origin even on another host name.
 */
export const SITE_ORIGIN = new InjectionToken<string>('SITE_ORIGIN', {
  providedIn: 'root',
  factory: () => {
    if (!isPlatformBrowser(inject(PLATFORM_ID))) {
      return SITE_ORIGIN_PLACEHOLDER;
    }
    const document = inject(DOCUMENT);
    const canonical = document.querySelector<HTMLLinkElement>('link[rel="canonical"]')?.href;
    return canonical ? new URL(canonical).origin : document.location.origin;
  },
});

/** What the head of one page says about it. */
export interface PageMeta {
  /** Unique per page. */
  readonly title: string;
  /** Unique per page, at most 160 characters. */
  readonly description: string;
  /**
   * Absolute path of the page's canonical URL, e.g. `/impressum`, or `null`
   * for a page that must not be indexed (the not-found page).
   */
  readonly path: string | null;
  /** JSON-LD objects of the page; they share one `@graph`. */
  readonly structuredData?: readonly object[];
}

const STRUCTURED_DATA_ID = 'structured-data';

/**
 * Writes the head of the current page: title, description, canonical link,
 * robots, Open Graph and Twitter card tags and the JSON-LD. Every page calls
 * `apply` once; tags of a previous page that the new one lacks are removed.
 */
@Injectable({ providedIn: 'root' })
export class SeoService {
  private readonly document = inject(DOCUMENT);
  private readonly title = inject(Title);
  private readonly meta = inject(Meta);
  private readonly content = inject(SEO_CONTENT);
  private readonly origin = inject(SITE_ORIGIN);

  apply(page: PageMeta): void {
    const url = page.path === null ? null : this.absolute(page.path);
    const image = this.content.shareImage;

    this.title.setTitle(page.title);
    this.setName('description', page.description);
    this.setName('robots', url === null ? 'noindex' : null);
    this.setCanonical(url);

    this.setProperty('og:type', 'website');
    this.setProperty('og:site_name', this.content.siteName);
    this.setProperty('og:locale', this.content.locale);
    this.setProperty('og:title', page.title);
    this.setProperty('og:description', page.description);
    this.setProperty('og:url', url);
    this.setProperty('og:image', this.absolute(image.path));
    this.setProperty('og:image:width', String(image.width));
    this.setProperty('og:image:height', String(image.height));
    this.setProperty('og:image:alt', image.alt);
    this.setName('twitter:card', 'summary_large_image');
    this.setName('twitter:title', page.title);
    this.setName('twitter:description', page.description);
    this.setName('twitter:image', this.absolute(image.path));
    this.setName('twitter:image:alt', image.alt);

    this.setStructuredData(page.structuredData ?? []);
  }

  /** `path` as an absolute URL on the site's origin. */
  absolute(path: string): string {
    return this.origin + path;
  }

  private setName(name: string, content: string | null): void {
    if (content === null) {
      this.meta.removeTag(`name="${name}"`);
    } else {
      this.meta.updateTag({ name, content });
    }
  }

  private setProperty(property: string, content: string | null): void {
    if (content === null) {
      this.meta.removeTag(`property="${property}"`);
    } else {
      this.meta.updateTag({ property, content });
    }
  }

  private setCanonical(url: string | null): void {
    let link = this.document.head.querySelector<HTMLLinkElement>('link[rel="canonical"]');
    if (url === null) {
      link?.remove();
      return;
    }
    if (!link) {
      link = this.document.createElement('link');
      link.setAttribute('rel', 'canonical');
      this.document.head.appendChild(link);
    }
    link.setAttribute('href', url);
  }

  private setStructuredData(objects: readonly object[]): void {
    let script = this.document.getElementById(STRUCTURED_DATA_ID);
    if (objects.length === 0) {
      script?.remove();
      return;
    }
    if (!script) {
      script = this.document.createElement('script');
      script.id = STRUCTURED_DATA_ID;
      script.setAttribute('type', 'application/ld+json');
      this.document.head.appendChild(script);
    }
    script.textContent = jsonForScript({ '@context': 'https://schema.org', '@graph': objects });
  }
}

/**
 * JSON that is safe inside a `<script>` element: `<` is escaped, so no string
 * in the data can close the element.
 */
export function jsonForScript(value: unknown): string {
  return JSON.stringify(value).replace(/</g, LESS_THAN_ESCAPE);
}

/** The JSON escape of `<`. */
const LESS_THAN_ESCAPE = '\\' + 'u003c';
