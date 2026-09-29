// Types of the "how it works" demo (ADR 0028): three servers, the people and
// programs that act on them, the reports they exchange and what each server
// decides at every step. The copy lives in the content file; these types
// carry no user-visible text.

/** The three servers of the demo, each run by a different operator. */
export type ServerId = 'a' | 'b' | 'c';

/** Whoever signs a report: one of the servers or the rogue participant. */
export type PublisherId = ServerId | 'rogue';

/** The attackers and addresses the story is about, in order of appearance. */
export type SubjectId = 'bot' | 'office' | 'scanner' | 'payment';

/** Subjects that attack servers. */
export type AttackerId = Extract<SubjectId, 'bot' | 'scanner'>;

export const SERVER_IDS: readonly ServerId[] = ['a', 'b', 'c'];
export const SUBJECT_IDS: readonly SubjectId[] = ['bot', 'office', 'scanner', 'payment'];

/** A node's decision settings, named after its configuration keys. */
export interface DecisionSettings {
  /** `decision.threshold`: the score at which an address is blocked. */
  readonly threshold: number;
  /** `decision.quorum`: the least number of reporters with a weight above 0. */
  readonly quorum: number;
  /** `trust.local_weight`: the weight of the node's own reports. */
  readonly localWeight: number;
  /** `trust.default_weight`: the weight of publishers the node does not list. */
  readonly defaultWeight: number;
  /** `decision.local_autoblock`: the node's own ban reports block on their own. */
  readonly localAutoblock: boolean;
}

/** One server: its settings, whom it trusts and how much, and its safety list. */
export interface ServerSetup {
  readonly id: ServerId;
  readonly settings: DecisionSettings;
  /** `trust.publishers`: the weight of each listed peer, from 0 to 1. */
  readonly trust: Readonly<Partial<Record<PublisherId, number>>>;
  /** Subjects on the server's allow-list, which are never blocked. */
  readonly safetyList: readonly SubjectId[];
}

/** The parts of a signed verdict (obie/0.1) that the demo uses. */
export interface Report {
  readonly publisher: PublisherId;
  readonly subject: SubjectId;
  /** `verdict.suggested_action`; only `ban` counts towards a block. */
  readonly action: 'ban' | 'watch';
  /** `verdict.confidence`, from 0 to 1. */
  readonly confidence: number;
  /** `evidence.reason`, e.g. `password_bruteforce`. */
  readonly reason: string;
  /** `evidence.events`: how many malicious events the publisher saw. */
  readonly events: number;
  /** `evidence.log_hash`, shortened for display. */
  readonly logHash: string;
  /** Minutes after the start of the story. */
  readonly issuedAt: number;
  /** `verdict.ttl_seconds`, in minutes. */
  readonly ttlMinutes: number;
}

/** Something that happens during a step, in the order it happens. */
export type DemoEvent =
  /** An attacker tries to reach a server; it is turned away if the server blocks it. */
  | { readonly kind: 'attack'; readonly attacker: AttackerId; readonly target: ServerId }
  /** A server's own detection: it keeps its own report. */
  | { readonly kind: 'detect'; readonly report: Report }
  /** A publisher sends its report directly to other servers, `copies` times. */
  | {
      readonly kind: 'share';
      readonly report: Report;
      readonly to: readonly ServerId[];
      readonly copies: number;
    }
  /** A publisher withdraws its report, on its own server and on the others. */
  | { readonly kind: 'revoke'; readonly report: Report; readonly to: readonly ServerId[] };

export interface ScenarioStep {
  /** Stable name of the step, used in tests. */
  readonly id: string;
  /** Minutes after the start of the story; reports expire by this clock. */
  readonly at: number;
  readonly events: readonly DemoEvent[];
}

export interface Scenario {
  readonly servers: readonly ServerSetup[];
  /** Example address (documentation ranges) of every subject. */
  readonly addresses: Readonly<Record<SubjectId, string>>;
  readonly steps: readonly ScenarioStep[];
}

/**
 * What a server makes of a subject: `unknown` without reports, `watching`
 * with reports that do not justify a block, `blocked`, or `safe` when the
 * subject is on its safety list.
 */
export type SubjectState = 'unknown' | 'watching' | 'blocked' | 'safe';

/** Why a subject has its state; `agreement` is the node's "consensus". */
export type DecisionCause = 'none' | 'below-bar' | 'agreement' | 'own-detection' | 'safety-list';

/** One publisher's latest report as the server weighs it. */
export interface Contribution {
  readonly publisher: PublisherId;
  readonly weight: number;
  readonly confidence: number;
  /** weight × confidence if the report counts, else 0. */
  readonly score: number;
  /** Whether it counts: a `ban` report of a publisher with a weight above 0. */
  readonly counts: boolean;
}

export interface Decision {
  readonly subject: SubjectId;
  readonly state: SubjectState;
  readonly cause: DecisionCause;
  /** Σ weight × confidence over the counting reports. */
  readonly score: number;
  /** Number of distinct publishers whose report counts. */
  readonly reporters: number;
  /** Every publisher's latest active report, in publisher order. */
  readonly contributions: readonly Contribution[];
}

/** A decision as shown at one step, marked when the step changed it. */
export interface DecisionView extends Decision {
  readonly changed: boolean;
  /** The state after the previous step; at the first step, the state itself. */
  readonly before: SubjectState;
}

export interface ServerView {
  readonly id: ServerId;
  readonly decisions: readonly DecisionView[];
}

export interface AttackView {
  readonly attacker: AttackerId;
  readonly target: ServerId;
  /** The target already blocked the attacker, so it never got in. */
  readonly turnedAway: boolean;
}

export interface MessageView {
  readonly kind: 'report' | 'revocation';
  readonly from: PublisherId;
  readonly to: readonly ServerId[];
  readonly subject: SubjectId;
  readonly copies: number;
}

/** Everything the demo shows at one step. */
export interface Frame {
  /** Zero-based index of the step. */
  readonly index: number;
  readonly servers: readonly ServerView[];
  /** Attacks during this step. */
  readonly attacks: readonly AttackView[];
  /** Messages sent during this step, always from one node straight to another. */
  readonly messages: readonly MessageView[];
}
