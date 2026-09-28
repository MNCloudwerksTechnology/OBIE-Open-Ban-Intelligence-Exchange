import { Link } from './landing-content.model';

/**
 * Copy of the legal pages (Impressum and privacy policy). Like the landing
 * page copy (ADR 0012), templates hold no text: a German version is a second
 * object of this type.
 */
export interface LegalContent {
  /**
   * While true, both legal pages open with `reviewNotice`. The operator sets
   * it to false once a legal review has approved the pages.
   */
  readonly reviewPending: boolean;
  readonly reviewNotice: { readonly heading: string; readonly text: string };
  readonly impressum: LegalPageContent;
  readonly privacy: LegalPageContent;
}

export interface LegalPageContent {
  readonly meta: { readonly title: string; readonly description: string };
  readonly heading: string;
  /**
   * The German legal term shown alongside the heading ("Impressum",
   * "Datenschutzerklärung"), so German visitors and authorities recognise
   * the page. A German version leaves it out.
   */
  readonly legalTerm?: string;
  /** Date of the current version, e.g. "Last updated: 28 September 2026". */
  readonly updated?: string;
  readonly intro?: string;
  readonly sections: readonly LegalSection[];
}

export interface LegalSection {
  /** Anchor id, unique on its page. */
  readonly id: string;
  readonly heading: string;
  /** German legal term of the section, shown alongside the heading. */
  readonly legalTerm?: string;
  readonly blocks: readonly LegalBlock[];
}

export type LegalBlock = LegalParagraph | LegalList | LegalFacts;

/** A paragraph, optionally followed by a link and more text. */
export interface LegalParagraph {
  readonly kind: 'paragraph';
  readonly text: string;
  readonly link?: Link;
  readonly after?: string;
}

export interface LegalList {
  readonly kind: 'list';
  readonly items: readonly string[];
}

/** Term and value pairs, e.g. company name, address and register number. */
export interface LegalFacts {
  readonly kind: 'facts';
  readonly items: readonly LegalFact[];
}

export interface LegalFact {
  readonly term: string;
  /** Lines of the value; an address has several. */
  readonly lines: readonly string[];
  /** Makes the value a link, e.g. `mailto:` or `tel:`. */
  readonly href?: string;
}
