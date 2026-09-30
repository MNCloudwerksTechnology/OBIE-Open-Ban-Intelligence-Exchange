/**
 * Shape of the landing page copy. Templates hold no user-visible text: every
 * string comes from an object of this type, so a German version is a second
 * object of the same type (ADR 0012).
 */
export interface LandingContent {
  readonly meta: {
    readonly title: string;
    readonly description: string;
    /** BCP 47 tag used to format numbers and dates, e.g. `en-GB`. */
    readonly locale: string;
  };
  readonly a11y: A11yContent;
  readonly header: HeaderContent;
  readonly hero: HeroContent;
  readonly problem: ProblemContent;
  readonly howItWorks: HowItWorksContent;
  readonly principles: PrinciplesContent;
  readonly status: StatusContent;
  readonly getStarted: GetStartedContent;
  readonly founder: FounderContent;
  readonly contact: ContactContent;
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
  readonly demo: MeshDemoContent;
}

/**
 * Copy of the step-by-step demo with three servers (ADR 0028). The story's
 * numbers and events live in `sections/mesh-demo/scenario.ts`; placeholders
 * in braces, e.g. `{n}`, are filled in by the demo.
 */
export interface MeshDemoContent {
  readonly heading: string;
  readonly intro: string;
  /** Says that the demo is an illustration, not live data from the network. */
  readonly illustration: string;
  /** The decision settings the demo uses and how they differ from the defaults. */
  readonly settings: string;
  /** One per step of the scenario, in the same order. */
  readonly steps: readonly DemoStepContent[];
  readonly servers: Readonly<Record<'a' | 'b' | 'c', DemoServerContent>>;
  readonly rogue: DemoActorContent;
  readonly subjects: Readonly<Record<'bot' | 'office' | 'scanner' | 'payment', DemoActorContent>>;
  /** Text of each state a subject can have on a server. */
  readonly states: Readonly<Record<'unknown' | 'watching' | 'blocked' | 'safe', string>>;
  /** Why a subject has its state. */
  readonly causes: Readonly<
    Record<'none' | 'below-bar' | 'agreement' | 'own-detection' | 'safety-list', string>
  >;
  readonly labels: DemoLabels;
  readonly controls: DemoControls;
  /** What a report carries and what stays on the server, shown when A shares. */
  readonly report: DemoReportContent;
  /** The last step's takeaways and calls to action. */
  readonly recap: { readonly takeaways: readonly string[]; readonly actions: readonly Link[] };
}

export interface DemoStepContent {
  readonly title: string;
  /** At most 40 words; terms are explained where they first appear. */
  readonly caption: string;
  /** Why the step matters, shown below the caption. */
  readonly note?: string;
  /** A related feature of a later release, shown with the "planned" label. */
  readonly planned?: string;
}

export interface DemoActorContent {
  readonly name: string;
  /** Short label on the map and in score sums. */
  readonly short: string;
}

export interface DemoServerContent extends DemoActorContent {
  readonly operator: string;
  /** What the server's safety list holds. */
  readonly safetyList: string;
}

export interface DemoLabels {
  readonly trusts: string;
  /** Weight of anyone not on the trust list; `{weight}` is the number. */
  readonly anyoneElse: string;
  readonly safetyList: string;
  /** `{score}` and `{threshold}`. */
  readonly score: string;
  /** `{count}` and `{quorum}`. */
  readonly reporters: string;
  /** Marks a state the current step changed. */
  readonly changed: string;
  readonly turnedAway: string;
  /** `{n}` copies of the same report. */
  readonly copies: string;
  readonly planned: string;
  readonly note: string;
}

export interface DemoControls {
  readonly label: string;
  readonly restart: string;
  readonly previous: string;
  readonly next: string;
  readonly play: string;
  readonly pause: string;
  readonly steps: string;
  /** `{n}` and `{total}`. */
  readonly stepOf: string;
  /** Accessible name of a step button: `{n}` and `{title}`. */
  readonly goTo: string;
  /** Announced on every step change: `{n}`, `{total}`, `{title}` and `{caption}`. */
  readonly announcement: string;
  /** Summary of the disclosure with all steps as text. */
  readonly transcript: string;
  /** Name of the list of servers. */
  readonly servers: string;
}

export interface DemoReportContent {
  /** `{server}` is the sender. */
  readonly heading: string;
  readonly fields: {
    readonly address: string;
    readonly reason: string;
    readonly events: string;
    readonly fingerprint: string;
    readonly suggestion: string;
    readonly confidence: string;
    readonly signature: string;
  };
  /** Plain words for `evidence.reason` codes. */
  readonly reasons: Readonly<Record<string, string>>;
  /** `{n}`. */
  readonly eventCount: string;
  readonly fingerprintNote: string;
  /** `{duration}`. */
  readonly suggestionValue: string;
  /** `{receivers}` check the signature. */
  readonly signatureValue: string;
  /** `{server}` keeps these. */
  readonly keptHeading: string;
  readonly kept: readonly string[];
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
  readonly project: ProjectContent;
}

/**
 * Links for contributors and the live stats strip in "Get started". The stats
 * come from the back end (`GET /api/project`); the strip is left out while
 * they are unavailable, the links are always shown.
 */
export interface ProjectContent {
  readonly linksLabel: string;
  /** Repository, quick start, protocol specification and good first issues. */
  readonly links: readonly Link[];
  /** Caption above the stats. */
  readonly statsCaption: string;
  readonly stars: string;
  readonly latestRelease: string;
  /** Shown instead of a release while there is none. */
  readonly noRelease: string;
  readonly lastActivity: string;
}

/** The founder profile. Every fact comes from the operator; nothing is invented. */
export interface FounderContent extends SectionBase {
  readonly name: string;
  readonly role: string;
  /** At most 80 words. */
  readonly bio: string;
  /** Headshot; `alt` is empty while `src` is the neutral placeholder avatar. */
  readonly photo: { readonly src: string; readonly alt: string };
  readonly topicsHeading: string;
  readonly topicsNote: string;
  /** Three to five talk topics. */
  readonly topics: readonly string[];
  /** Optional profile links (LinkedIn, GitHub); may be empty. */
  readonly links: readonly Link[];
  readonly linksLabel: string;
  /** Opens the inquiry form. */
  readonly invite: Link;
}

/** What a visitor can ask for; the values of the back end's `type` field. */
export type InquiryType = 'talk' | 'workshop' | 'interview' | 'collaboration' | 'other';

/** Label and optional hint of one form field. */
export interface FieldCopy {
  readonly label: string;
  readonly hint?: string;
}

export interface ContactContent extends SectionBase {
  readonly intro: string;
  readonly form: InquiryFormContent;
}

/** Copy of the inquiry form, including the messages that mirror the back end's rules. */
export interface InquiryFormContent {
  readonly typeLegend: string;
  readonly types: readonly { readonly value: InquiryType; readonly label: string }[];
  readonly eventLegend: string;
  readonly fields: {
    readonly name: FieldCopy;
    readonly email: FieldCopy;
    readonly organisation: FieldCopy;
    readonly eventDate: FieldCopy;
    readonly eventLocation: FieldCopy;
    readonly audienceSize: FieldCopy;
    readonly message: FieldCopy;
  };
  readonly optional: string;
  /** Consent sentence: `before`, then the privacy link, then `after`. */
  readonly consent: { readonly before: string; readonly link: Link; readonly after: string };
  readonly honeypot: string;
  readonly submit: string;
  readonly sending: string;
  /** Announced when a submission is stopped by invalid fields. */
  readonly invalid: string;
  readonly success: { readonly heading: string; readonly text: string };
  readonly error: {
    readonly heading: string;
    readonly text: string;
    readonly expired: string;
    readonly rateLimited: string;
    readonly retry: string;
  };
  readonly messages: ValidationMessages;
}

/** Validation messages, worded exactly like the back end's (InquiryRequest.java). */
export interface ValidationMessages {
  readonly typeRequired: string;
  readonly nameRequired: string;
  readonly emailRequired: string;
  readonly emailInvalid: string;
  readonly messageRequired: string;
  readonly messageLength: string;
  /** `{max}` is replaced with the limit. */
  readonly maxLength: string;
  readonly singleLine: string;
  readonly controlCharacters: string;
  readonly dateInFuture: string;
  readonly dateFormat: string;
  readonly positiveNumber: string;
  readonly wholeNumber: string;
  readonly maxAudience: string;
  readonly consentRequired: string;
}

export interface FaqContent extends SectionBase {
  readonly items: readonly { readonly question: string; readonly answer: string }[];
}

export interface FooterContent {
  readonly tagline: string;
  /** "View on GitHub" button. */
  readonly github: Link;
  readonly links: readonly Link[];
  readonly attribution: { readonly text: string; readonly href: string };
  readonly licence: string;
}
