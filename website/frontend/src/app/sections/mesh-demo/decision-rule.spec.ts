import { decide, weightOf } from './decision-rule';
import { Report, ServerSetup } from './mesh-demo.model';

// The rule of internal/decision (Evaluate and Decide), case by case.

const server: ServerSetup = {
  id: 'c',
  settings: { threshold: 1.2, quorum: 2, localWeight: 1, defaultWeight: 0, localAutoblock: true },
  trust: { a: 0.8, b: 0.8 },
  safetyList: ['payment'],
};

function report(publisher: Report['publisher'], overrides: Partial<Report> = {}): Report {
  return {
    publisher,
    subject: 'bot',
    action: 'ban',
    confidence: 0.8,
    reason: 'password_bruteforce',
    events: 10,
    logHash: 'sha256:…',
    issuedAt: 0,
    ttlMinutes: 60,
    ...overrides,
  };
}

describe('Demo decision rule', () => {
  it('weights its own reports with the local weight, listed peers as listed, others with the default', () => {
    expect(weightOf(server, 'c')).toBe(1);
    expect(weightOf(server, 'a')).toBe(0.8);
    expect(weightOf(server, 'rogue')).toBe(0);
    expect(
      weightOf({ ...server, settings: { ...server.settings, defaultWeight: 0.3 } }, 'rogue'),
    ).toBe(0.3);
  });

  it('knows nothing about a subject without reports', () => {
    const decision = decide(server, 'bot', []);
    expect(decision).toEqual({
      subject: 'bot',
      state: 'unknown',
      cause: 'none',
      score: 0,
      reporters: 0,
      contributions: [],
    });
  });

  it('only watches with one trusted reporter: below the threshold and the quorum', () => {
    const decision = decide(server, 'bot', [report('a')]);
    expect(decision.state).toBe('watching');
    expect(decision.cause).toBe('below-bar');
    expect(decision.score).toBeCloseTo(0.64, 10);
    expect(decision.reporters).toBe(1);
  });

  it('blocks when two trusted reporters together reach the threshold', () => {
    const decision = decide(server, 'bot', [report('b'), report('a')]);
    expect(decision.state).toBe('blocked');
    expect(decision.cause).toBe('agreement');
    expect(decision.score).toBeCloseTo(1.28, 10);
    expect(decision.reporters).toBe(2);
    expect(decision.contributions.map((c) => c.publisher)).toEqual(['a', 'b']);
  });

  it('does not block below the threshold, even with the quorum met', () => {
    const decision = decide(server, 'bot', [report('a', { confidence: 0.5 }), report('b')]);
    expect(decision.score).toBeCloseTo(1.04, 10);
    expect(decision.reporters).toBe(2);
    expect(decision.state).toBe('watching');
  });

  it('does not block without the quorum, however high the score', () => {
    const strict = { ...server, settings: { ...server.settings, threshold: 0.5, quorum: 2 } };
    expect(decide(strict, 'bot', [report('a')]).state).toBe('watching');
  });

  it('reaches the threshold despite float rounding (tolerance 1e-9)', () => {
    const three = {
      ...server,
      settings: { ...server.settings, threshold: 1.8, quorum: 3, localAutoblock: false },
      trust: { a: 1, b: 1 },
    };
    const reports = [
      report('a', { confidence: 0.6 }),
      report('b', { confidence: 0.6 }),
      report('c', { confidence: 0.6 }),
    ];
    expect(0.6 + 0.6 + 0.6).toBeLessThan(1.8);
    expect(decide(three, 'bot', reports).state).toBe('blocked');
  });

  it('gives reports of publishers it does not trust no weight and does not count them', () => {
    const decision = decide(server, 'bot', [report('rogue', { confidence: 1 })]);
    expect(decision.state).toBe('watching');
    expect(decision.score).toBe(0);
    expect(decision.reporters).toBe(0);
    expect(decision.contributions).toEqual([
      { publisher: 'rogue', weight: 0, confidence: 1, score: 0, counts: false },
    ]);
  });

  it('never counts watch reports', () => {
    const decision = decide(server, 'bot', [
      report('a', { action: 'watch' }),
      report('b', { action: 'watch' }),
    ]);
    expect(decision.state).toBe('watching');
    expect(decision.reporters).toBe(0);
  });

  it('blocks on its own report at once (local autoblock)', () => {
    const decision = decide(server, 'bot', [report('c')]);
    expect(decision.state).toBe('blocked');
    expect(decision.cause).toBe('own-detection');
    expect(decision.score).toBeCloseTo(0.8, 10);
    expect(decision.reporters).toBe(1);
  });

  it('names agreement as the cause when its own report and a peer reach the bar together', () => {
    const decision = decide(server, 'bot', [report('c'), report('a')]);
    expect(decision.cause).toBe('agreement');
    expect(decision.score).toBeCloseTo(1.44, 10);
  });

  it('does not autoblock when autoblock is off or its own weight is 0', () => {
    const off = { ...server, settings: { ...server.settings, localAutoblock: false } };
    expect(decide(off, 'bot', [report('c')]).state).toBe('watching');
    const distrustsItself = { ...server, settings: { ...server.settings, localWeight: 0 } };
    expect(decide(distrustsItself, 'bot', [report('c')]).state).toBe('watching');
  });

  it('never blocks an address on its safety list, whatever anyone reports', () => {
    const decision = decide(server, 'payment', [
      report('a', { subject: 'payment' }),
      report('b', { subject: 'payment' }),
      report('c', { subject: 'payment' }),
    ]);
    expect(decision.score).toBeCloseTo(2.08, 10);
    expect(decision.reporters).toBe(3);
    expect(decision.state).toBe('safe');
    expect(decision.cause).toBe('safety-list');
    expect(decide(server, 'payment', []).state).toBe('safe');
  });
});
