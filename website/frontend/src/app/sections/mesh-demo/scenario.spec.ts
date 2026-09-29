import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { Frame, SERVER_IDS, SUBJECT_IDS, ServerId, SubjectId } from './mesh-demo.model';
import { replay } from './replay';
import { DEMO_SETTINGS, FLOOD_COPIES, NODE_DEFAULTS, SCENARIO } from './scenario';

const frames = replay(SCENARIO);
const STEP_IDS = SCENARIO.steps.map((step) => step.id);

function frame(step: string): Frame {
  return frames[STEP_IDS.indexOf(step)];
}

function decision(step: string, server: ServerId, subject: SubjectId) {
  const found = frame(step)
    .servers.find((s) => s.id === server)
    ?.decisions.find((d) => d.subject === subject);
  if (!found) {
    throw new Error(`no decision of ${server} on ${subject} at ${step}`);
  }
  return found;
}

function minuteOf(step: string): number {
  return SCENARIO.steps[STEP_IDS.indexOf(step)].at;
}

const repoFile = (path: string) => readFileSync(resolve(process.cwd(), '../..', path), 'utf8');

// What every server makes of bot, office, scanner and payment after each
// step: U unknown, W watching, B blocked, S safety list.
const OUTCOMES: Record<string, Record<ServerId, string>> = {
  neighbourhood: { a: 'U U U S', b: 'U U U U', c: 'U U U U' },
  'bot-hits-a': { a: 'B B U S', b: 'U U U U', c: 'U U U U' },
  'a-shares': { a: 'B B U S', b: 'W W U U', c: 'W W U U' },
  'one-voice': { a: 'B B U S', b: 'W W U U', c: 'W W U U' },
  'bot-hits-b': { a: 'B B U S', b: 'B W U U', c: 'W W U U' },
  'c-protected': { a: 'B B U S', b: 'B W U U', c: 'B W U U' },
  'scanner-hits-c': { a: 'B B W S', b: 'B W W U', c: 'B W B U' },
  'rogue-floods': { a: 'B B W S', b: 'B W W W', c: 'B W B W' },
  undo: { a: 'B U U S', b: 'B U U W', c: 'B U U W' },
  recap: { a: 'B U U S', b: 'B U U W', c: 'B U U W' },
};

const LETTER = { unknown: 'U', watching: 'W', blocked: 'B', safe: 'S' } as const;

describe('Demo scenario', () => {
  it('sets up each of the three servers once', () => {
    expect(SCENARIO.servers.map((server) => server.id)).toEqual([...SERVER_IDS]);
  });

  it('tells ten steps in the order of the story', () => {
    expect(STEP_IDS).toEqual(Object.keys(OUTCOMES));
    expect(frames.length).toBe(10);
    const times = SCENARIO.steps.map((step) => step.at);
    expect(times).toEqual([...times].sort((x, y) => x - y));
  });

  for (const [step, expected] of Object.entries(OUTCOMES)) {
    it(`decides who blocks and who only watches after step "${step}"`, () => {
      for (const server of SERVER_IDS) {
        const states = SUBJECT_IDS.map((subject) => LETTER[decision(step, server, subject).state]);
        expect(states.join(' '), `server ${server}`).toBe(expected[server]);
      }
    });
  }

  it('lets A block the bot on its own detection, with one reporter below the bar', () => {
    const a = decision('bot-hits-a', 'a', 'bot');
    expect([a.cause, a.reporters]).toEqual(['own-detection', 1]);
    expect(a.score).toBeCloseTo(0.8, 10);
  });

  it('keeps one trusted reporter below every bar: 0.8 × 0.8 = 0.64 of 1.2, 1 of 2', () => {
    for (const server of ['b', 'c'] as const) {
      for (const subject of ['bot', 'office'] as const) {
        const d = decision('one-voice', server, subject);
        expect(d.score).toBeCloseTo(0.64, 10);
        expect(d.score).toBeLessThan(DEMO_SETTINGS.threshold);
        expect(d.reporters).toBeLessThan(DEMO_SETTINGS.quorum);
      }
    }
  });

  it('counts B’s own detection as one of two agreeing voices: 0.80 + 0.64 = 1.44', () => {
    const b = decision('bot-hits-b', 'b', 'bot');
    expect([b.cause, b.reporters]).toEqual(['agreement', 2]);
    expect(b.score).toBeCloseTo(1.44, 10);
  });

  it('protects C before the bot arrives: 0.64 + 0.64 = 1.28 reaches 1.2 with 2 reporters', () => {
    const c = decision('c-protected', 'c', 'bot');
    expect([c.state, c.cause, c.reporters, c.changed]).toEqual(['blocked', 'agreement', 2, true]);
    expect(c.score).toBeCloseTo(1.28, 10);
    expect(c.contributions.map((x) => [x.publisher, x.weight, x.confidence])).toEqual([
      ['a', 0.8, 0.8],
      ['b', 0.8, 0.8],
    ]);
    expect(frame('c-protected').attacks).toEqual([
      { attacker: 'bot', target: 'c', turnedAway: true },
    ]);
  });

  it('turns an attacker away only where it is already blocked', () => {
    const turnedAway = frames.flatMap((f) =>
      f.attacks.filter((a) => a.turnedAway).map((a) => `${STEP_IDS[f.index]}:${a.target}`),
    );
    expect(turnedAway).toEqual(['c-protected:c']);
  });

  it('weighs C’s scanner report differently on A and B, and neither blocks', () => {
    expect(decision('scanner-hits-c', 'a', 'scanner').score).toBeCloseTo(0.4, 10);
    expect(decision('scanner-hits-c', 'b', 'scanner').score).toBeCloseTo(0.64, 10);
    const c = decision('scanner-hits-c', 'c', 'scanner');
    expect([c.state, c.cause]).toEqual(['blocked', 'own-detection']);
  });

  it('gives the rogue’s flood no weight, one vote at most, and never beats the safety list', () => {
    const [flood] = frame('rogue-floods').messages;
    expect(flood).toEqual({
      kind: 'report',
      from: 'rogue',
      to: ['a', 'b', 'c'],
      subject: 'payment',
      copies: FLOOD_COPIES,
    });
    for (const server of SERVER_IDS) {
      const d = decision('rogue-floods', server, 'payment');
      expect([d.score, d.reporters, d.contributions.length], server).toEqual([0, 0, 1]);
    }
    expect(decision('rogue-floods', 'a', 'payment').cause).toBe('safety-list');
  });

  it('undoes the office block when A withdraws its report, and lets the scanner block expire', () => {
    expect(frame('undo').messages).toEqual([
      { kind: 'revocation', from: 'a', to: ['b', 'c'], subject: 'office', copies: 1 },
    ]);
    for (const server of SERVER_IDS) {
      expect(decision('undo', server, 'office').state, server).toBe('unknown');
      expect(decision('undo', server, 'scanner').state, server).toBe('unknown');
    }
    // Nothing withdrew the scanner's report: it lasts an hour, and more than an hour has passed.
    expect(decision('rogue-floods', 'c', 'scanner').state).toBe('blocked');
    expect(minuteOf('undo') - minuteOf('scanner-hits-c')).toBeGreaterThan(60);
  });

  it('keeps the earlier blocks when going back: A still blocks the office before the undo', () => {
    expect(decision('rogue-floods', 'a', 'office').state).toBe('blocked');
    expect(decision('rogue-floods', 'b', 'office').state).toBe('watching');
  });

  it('sends every message straight from its publisher to other servers', () => {
    for (const f of frames) {
      for (const message of f.messages) {
        expect(message.to).not.toContain(message.from);
        expect(message.to.every((to) => SERVER_IDS.includes(to))).toBe(true);
      }
    }
  });

  it('marks what a step changed and nothing else', () => {
    const changed = (step: string) =>
      frame(step).servers.flatMap((s) =>
        s.decisions.filter((d) => d.changed).map((d) => `${s.id}:${d.subject}`),
      );
    expect(changed('neighbourhood')).toEqual([]);
    expect(changed('one-voice')).toEqual([]);
    expect(changed('c-protected')).toEqual(['a:bot', 'c:bot']);
    // A's safety list holds, but A did receive the flood.
    expect(changed('rogue-floods')).toEqual(['a:payment', 'b:payment', 'c:payment']);
  });

  it('uses the node’s documented defaults, except the threshold the demo announces', () => {
    const yaml = repoFile('documentation/examples/obie.yaml');
    expect(yaml).toMatch(new RegExp(`^  mode: ${NODE_DEFAULTS.mode}$`, 'm'));
    expect(yaml).toMatch(new RegExp(`^  threshold: ${NODE_DEFAULTS.threshold}$`, 'm'));
    expect(yaml).toMatch(new RegExp(`^  quorum: ${NODE_DEFAULTS.quorum}$`, 'm'));
    expect(yaml).toMatch(/^ {2}local_weight: 1\.0$/m);
    expect(NODE_DEFAULTS.localWeight).toBe(1);
    expect(yaml).toMatch(new RegExp(`^  default_weight: ${NODE_DEFAULTS.defaultWeight}$`, 'm'));
    expect(yaml).toMatch(new RegExp(`^  local_autoblock: ${NODE_DEFAULTS.localAutoblock}$`, 'm'));
    expect(repoFile('documentation/guides/fail2ban.md')).toMatch(
      new RegExp(`\\| \`confidence\` +\\| \`${NODE_DEFAULTS.fail2banConfidence}\``),
    );
    // The federation guide's suggestion for three to five nodes.
    expect(repoFile('documentation/operations/federation.md')).toMatch(
      new RegExp(
        `\\| Three to five nodes \\| 0\\.8 each \\| ${DEMO_SETTINGS.threshold} \\| ${DEMO_SETTINGS.quorum} \\|`,
      ),
    );
    for (const server of SCENARIO.servers) {
      const { threshold, ...rest } = server.settings;
      expect(threshold).toBe(1.2);
      expect(rest).toEqual({
        quorum: NODE_DEFAULTS.quorum,
        localWeight: NODE_DEFAULTS.localWeight,
        defaultWeight: NODE_DEFAULTS.defaultWeight,
        localAutoblock: NODE_DEFAULTS.localAutoblock,
      });
      for (const weight of Object.values(server.trust)) {
        expect(weight).toBeGreaterThan(0);
        expect(weight).toBeLessThanOrEqual(1);
      }
      expect(server.trust.rogue).toBeUndefined();
    }
  });

  it('reports within the protocol’s limits and with Fail2Ban’s confidence', () => {
    const reports = SCENARIO.steps.flatMap((step) =>
      step.events.flatMap((event) => ('report' in event ? [event.report] : [])),
    );
    for (const report of reports) {
      expect(report.ttlMinutes).toBeGreaterThanOrEqual(1);
      expect(report.ttlMinutes).toBeLessThanOrEqual(30 * 24 * 60);
      expect(report.reason).toMatch(/^[a-z0-9_]{1,64}$/);
      if (report.publisher !== 'rogue') {
        expect(report.confidence).toBe(NODE_DEFAULTS.fail2banConfidence);
      }
    }
  });
});
