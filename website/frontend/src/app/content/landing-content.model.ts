/**
 * Shape of the landing page copy. Templates hold no user-visible text: every
 * string comes from an object of this type, so a German version is a second
 * object of the same type (ADR 0012).
 */
export interface LandingContent {
  readonly meta: { readonly title: string; readonly description: string };
  readonly a11y: A11yContent;
  readonly header: HeaderContent;
  readonly hero: HeroContent;
  readonly problem: ProblemContent;
  readonly howItWorks: HowItWorksContent;
  readonly principles: PrinciplesContent;
  readonly status: StatusContent;
  readonly getStarted: GetStartedContent;
  readonly founder: FounderContent;
  readonly faq: FaqContent;
  readonly footer: FooterContent;
}

/** A link rendered as an anchor; `href` is absolute or an in-page `#id`. */
export interface Link {
  readonly label: string;
  readonly href: string;
}

/** Fields every page section shares: its anchor id and its heading. */
export interface SectionBase {
  /** Anchor id of the section; the header navigation links to it. */
  readonly id: string;
  /** Short label shown above the heading and in the navigation. */
  readonly label: string;
  readonly heading: string;
  /** Closing call to action of the section, always pointing to GitHub. */
  readonly nextStep: Link;
}

export interface A11yContent {
  readonly skipLink: string;
  readonly primaryNav: string;
  readonly footerNav: string;
  readonly homeLink: string;
  readonly themeToDark: string;
  readonly themeToLight: string;
}

export interface HeaderContent {
  readonly github: Link;
}

export interface HeroContent {
  readonly eyebrow: string;
  readonly heading: string;
  readonly lead: string;
  readonly primary: Link;
  readonly secondary: Link;
  readonly challenge: Link;
  readonly noTokens: string;
  readonly report: SignedReportContent;
}

/** A simplified signed report shown as an illustration in the hero. */
export interface SignedReportContent {
  readonly caption: string;
  readonly title: string;
  readonly rows: readonly { readonly key: string; readonly value: string }[];
  readonly signature: string;
}

export interface ProblemContent extends SectionBase {
  readonly hook: string;
  readonly paragraphs: readonly string[];
  readonly points: readonly Card[];
  readonly answer: string;
}

export interface Card {
  readonly title: string;
  readonly text: string;
}

export interface HowItWorksContent extends SectionBase {
  readonly intro: string;
  readonly steps: readonly Card[];
  readonly note: string;
  readonly diagram: DiagramContent;
}

/** Labels of the "how it works" diagram (at most 5 nodes and 12 labels). */
export interface DiagramContent {
  readonly title: string;
  readonly description: string;
  readonly attacker: string;
  readonly peerA: string;
  readonly peerB: string;
  readonly you: string;
  readonly report: string;
  readonly decision: string;
  readonly safetyList: string;
  readonly block: string;
}

export interface PrinciplesContent extends SectionBase {
  readonly hook: string;
  readonly cards: readonly Card[];
}

/** Honest delivery state of a feature, shown as a label. */
export type FeatureState = 'available' | 'in-progress' | 'planned';

export interface StatusGroup {
  readonly state: FeatureState;
  readonly label: string;
  readonly summary: string;
  readonly items: readonly Card[];
}

export interface StatusContent extends SectionBase {
  readonly intro: string;
  readonly groups: readonly StatusGroup[];
  readonly details: Link;
}

export interface GetStartedStep extends Card {
  readonly code: string;
}

export interface GetStartedContent extends SectionBase {
  readonly intro: string;
  readonly steps: readonly GetStartedStep[];
  readonly note: string;
  readonly quickStart: Link;
}

export interface FounderContent extends SectionBase {
  readonly placeholderLabel: string;
  readonly placeholder: string;
}

export interface FaqContent extends SectionBase {
  readonly items: readonly { readonly question: string; readonly answer: string }[];
}

export interface FooterContent {
  readonly tagline: string;
  readonly links: readonly Link[];
  readonly attribution: { readonly text: string; readonly href: string };
  readonly licence: string;
}
