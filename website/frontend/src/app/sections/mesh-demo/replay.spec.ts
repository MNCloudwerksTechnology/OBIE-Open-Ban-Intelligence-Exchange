import { Report, Scenario, ScenarioStep } from './mesh-demo.model';
import { replay } from './replay';
import { DEMO_SETTINGS } from './scenario';

// How the replay picks the reports that count, like latestPerPublisher in
// internal/decision: expired reports first drop out, then the newest
// remaining report of each publisher counts.

function report(publisher: Report['publisher'], issuedAt: number, ttlMinutes: number): Report {
  return {
    publisher,
    subject: 'bot',
    action: 'ban',
    confidence: 0.8,
    reason: 'password_bruteforce',
    events: 5,
    logHash: 'sha256:…',
    issuedAt,
    ttlMinutes,
  };
}

function scenario(steps: ScenarioStep[]): Scenario {
  return {
    servers: [
      { id: 'a', settings: DEMO_SETTINGS, trust: { b: 0.8, c: 0.8 }, safetyList: [] },
      { id: 'b', settings: DEMO_SETTINGS, trust: { a: 0.8, c: 0.8 }, safetyList: [] },
      { id: 'c', settings: DEMO_SETTINGS, trust: { a: 0.8, b: 0.8 }, safetyList: [] },
    ],
    addresses: { bot: '203.0.113.7', office: '', scanner: '', payment: '' },
    steps,
  };
}

/** C's decision on the bot after each step. */
function botOnC(steps: ScenarioStep[]) {
  return replay(scenario(steps)).map(
    (frame) => frame.servers[2].decisions.find((d) => d.subject === 'bot') ?? null,
  );
}

describe('Demo replay', () => {
  it('falls back to a publisher’s older report once its newer one has expired', () => {
    const older = report('a', 0, 600);
    const newer = report('a', 10, 5);
    const decisions = botOnC([
      { id: 'older', at: 0, events: [{ kind: 'share', report: older, to: ['c'], copies: 1 }] },
      { id: 'newer', at: 12, events: [{ kind: 'share', report: newer, to: ['c'], copies: 1 }] },
      { id: 'later', at: 20, events: [] },
    ]);
    expect(decisions.map((d) => [d?.state, d?.reporters])).toEqual([
      ['watching', 1],
      ['watching', 1],
      ['watching', 1],
    ]);
  });

  it('keeps the newest report of a publisher, whatever order the reports arrive in', () => {
    const newer = report('a', 10, 600);
    const older = report('a', 0, 5);
    const decisions = botOnC([
      { id: 'newer', at: 10, events: [{ kind: 'share', report: newer, to: ['c'], copies: 1 }] },
      { id: 'older', at: 11, events: [{ kind: 'share', report: older, to: ['c'], copies: 1 }] },
    ]);
    // The older report has already expired; had it replaced the newer one, C would know nothing.
    expect(decisions[1]?.state).toBe('watching');
    expect(decisions[1]?.contributions.length).toBe(1);
  });

  it('counts a publisher once, however often it repeats its report', () => {
    const [decision] = botOnC([
      {
        id: 'flood',
        at: 0,
        events: [{ kind: 'share', report: report('a', 0, 60), to: ['c'], copies: 50 }],
      },
    ]);
    expect([decision?.reporters, decision?.contributions.length]).toEqual([1, 1]);
    expect(decision?.score).toBeCloseTo(0.64, 10);
  });

  it('withdraws a revoked report on the publisher and on every server it reached', () => {
    const own = report('a', 0, 600);
    const frames = replay(
      scenario([
        {
          id: 'report',
          at: 0,
          events: [
            { kind: 'detect', report: own },
            { kind: 'share', report: own, to: ['b', 'c'], copies: 1 },
          ],
        },
        { id: 'revoke', at: 1, events: [{ kind: 'revoke', report: own, to: ['b', 'c'] }] },
      ]),
    );
    const states = (index: number) =>
      frames[index].servers.map((s) => s.decisions.find((d) => d.subject === 'bot')?.state);
    expect(states(0)).toEqual(['blocked', 'watching', 'watching']);
    expect(states(1)).toEqual(['unknown', 'unknown', 'unknown']);
  });

  it('lets only the three servers detect attacks', () => {
    expect(() =>
      replay(
        scenario([{ id: 'x', at: 0, events: [{ kind: 'detect', report: report('rogue', 0, 1) }] }]),
      ),
    ).toThrow('only the three servers detect attacks');
  });
});
