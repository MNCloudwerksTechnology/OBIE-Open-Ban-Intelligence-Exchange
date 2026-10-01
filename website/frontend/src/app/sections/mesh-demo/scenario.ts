import {
  DecisionSettings,
  Report,
  Scenario,
  ScenarioStep,
  ServerId,
  SubjectId,
} from './mesh-demo.model';

// The story of the demo as data: who trusts whom, who reports what and when.
// Its captions are in the content file (howItWorks.demo); the numbers here
// are checked against the node's documented defaults (scenario.spec.ts).

/**
 * Defaults of an OBIE node (documentation/examples/obie.yaml) and the
 * confidence of a Fail2Ban report (documentation/guides/fail2ban.md).
 */
export const NODE_DEFAULTS = {
  /** `node.mode`: a new node decides but blocks nothing; the demo's servers enforce. */
  mode: 'observe',
  threshold: 1.8,
  quorum: 2,
  localWeight: 1,
  defaultWeight: 0,
  localAutoblock: true,
  fail2banConfidence: 0.8,
} as const;

/**
 * The settings of all three servers: the defaults, except for the threshold,
 * which the federation guide suggests for three to five nodes. With the
 * default of 1.8, two peers with a weight of 0.8 (0.64 + 0.64) would not be
 * enough. The demo says so (howItWorks.demo.settings).
 */
export const DEMO_SETTINGS: DecisionSettings = {
  threshold: 1.2,
  quorum: NODE_DEFAULTS.quorum,
  localWeight: NODE_DEFAULTS.localWeight,
  defaultWeight: NODE_DEFAULTS.defaultWeight,
  localAutoblock: NODE_DEFAULTS.localAutoblock,
};

/** Weight the federation guide suggests for a peer you trust. */
const TRUSTED = 0.8;

const MINUTES_PER_DAY = 24 * 60;

/** A Fail2Ban ban on this server, reported with the action's default confidence. */
function fail2ban(
  publisher: ServerId,
  subject: SubjectId,
  details: Pick<Report, 'reason' | 'events' | 'logHash' | 'issuedAt' | 'ttlMinutes'>,
): Report {
  return {
    publisher,
    subject,
    action: 'ban',
    confidence: NODE_DEFAULTS.fail2banConfidence,
    ...details,
  };
}

const A_BOT = fail2ban('a', 'bot', {
  reason: 'password_bruteforce',
  events: 47,
  logHash: 'sha256:9f2c…41e7',
  issuedAt: 5,
  ttlMinutes: 7 * MINUTES_PER_DAY,
});

/** The mistake: a colleague mistyped a password a few times behind a shared office connection. */
const A_OFFICE = fail2ban('a', 'office', {
  reason: 'password_bruteforce',
  events: 6,
  logHash: 'sha256:3b8d…0c52',
  issuedAt: 5,
  ttlMinutes: 7 * MINUTES_PER_DAY,
});

const B_BOT = fail2ban('b', 'bot', {
  reason: 'password_bruteforce',
  events: 52,
  logHash: 'sha256:c41a…9d03',
  issuedAt: 40,
  ttlMinutes: 7 * MINUTES_PER_DAY,
});

/** A web jail with a one-hour ban: its report expires after an hour. */
const C_SCANNER = fail2ban('c', 'scanner', {
  reason: 'web_scan',
  events: 120,
  logHash: 'sha256:07e5…b6f1',
  issuedAt: 75,
  ttlMinutes: 60,
});

/** The rogue claims full confidence and the longest lifetime the protocol allows. */
const ROGUE_PAYMENT: Report = {
  publisher: 'rogue',
  subject: 'payment',
  action: 'ban',
  confidence: 1,
  reason: 'password_bruteforce',
  events: 9999,
  logHash: 'sha256:0000…0000',
  issuedAt: 90,
  ttlMinutes: 30 * MINUTES_PER_DAY,
};

/** How often the rogue repeats its report. */
export const FLOOD_COPIES = 50;

const steps: readonly ScenarioStep[] = [
  { id: 'neighbourhood', at: 0, events: [] },
  {
    id: 'bot-hits-a',
    at: 5,
    events: [
      { kind: 'attack', attacker: 'bot', target: 'a' },
      { kind: 'detect', report: A_BOT },
      { kind: 'detect', report: A_OFFICE },
    ],
  },
  {
    id: 'a-shares',
    at: 6,
    events: [
      { kind: 'share', report: A_BOT, to: ['b', 'c'], copies: 1 },
      { kind: 'share', report: A_OFFICE, to: ['b', 'c'], copies: 1 },
    ],
  },
  { id: 'one-voice', at: 6, events: [] },
  {
    id: 'bot-hits-b',
    at: 40,
    events: [
      { kind: 'attack', attacker: 'bot', target: 'b' },
      { kind: 'detect', report: B_BOT },
    ],
  },
  {
    id: 'c-protected',
    at: 52,
    events: [
      { kind: 'share', report: B_BOT, to: ['a', 'c'], copies: 1 },
      { kind: 'attack', attacker: 'bot', target: 'c' },
    ],
  },
  {
    id: 'scanner-hits-c',
    at: 75,
    events: [
      { kind: 'attack', attacker: 'scanner', target: 'c' },
      { kind: 'detect', report: C_SCANNER },
      { kind: 'share', report: C_SCANNER, to: ['a', 'b'], copies: 1 },
    ],
  },
  {
    id: 'rogue-floods',
    at: 90,
    events: [{ kind: 'share', report: ROGUE_PAYMENT, to: ['a', 'b', 'c'], copies: FLOOD_COPIES }],
  },
  {
    id: 'undo',
    at: 140,
    events: [{ kind: 'revoke', report: A_OFFICE, to: ['b', 'c'] }],
  },
  { id: 'recap', at: 140, events: [] },
];

export const SCENARIO: Scenario = {
  servers: [
    { id: 'a', settings: DEMO_SETTINGS, trust: { b: TRUSTED, c: 0.5 }, safetyList: ['payment'] },
    { id: 'b', settings: DEMO_SETTINGS, trust: { a: TRUSTED, c: TRUSTED }, safetyList: [] },
    { id: 'c', settings: DEMO_SETTINGS, trust: { a: TRUSTED, b: TRUSTED }, safetyList: [] },
  ],
  // Documentation ranges (RFC 5737): example addresses that belong to nobody.
  // A real node keeps these ranges on its built-in allow-list and refuses
  // reports about them; the demo is an illustration and uses them on purpose,
  // so that it names no real address. Do not "fix" the rule for them.
  addresses: {
    bot: '203.0.113.7',
    office: '198.51.100.23',
    scanner: '198.51.100.44',
    payment: '192.0.2.80',
  },
  steps,
};

/** A's report on the bot, shown when A shares it (what is shared and what is not). */
export const SHARED_REPORT = A_BOT;
