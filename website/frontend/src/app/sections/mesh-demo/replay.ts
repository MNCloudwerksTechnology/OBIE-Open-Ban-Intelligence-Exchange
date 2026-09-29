import { decide } from './decision-rule';
import {
  AttackView,
  Decision,
  DecisionView,
  DemoEvent,
  Frame,
  MessageView,
  PublisherId,
  Report,
  SERVER_IDS,
  SUBJECT_IDS,
  Scenario,
  ServerId,
  ServerSetup,
  ServerView,
  SubjectId,
} from './mesh-demo.model';

/** Each server's reports: per subject, the latest report of each publisher. */
type Stores = Record<ServerId, Map<SubjectId, Map<PublisherId, Report>>>;

/**
 * Plays the scenario and returns what the demo shows after each step. The
 * state at a step depends only on the steps up to it, so jumping to a step
 * or going back always shows the same state as stepping through.
 */
export function replay(scenario: Scenario): readonly Frame[] {
  const stores: Stores = { a: new Map(), b: new Map(), c: new Map() };
  const setups = new Map(scenario.servers.map((server) => [server.id, server]));
  const frames: Frame[] = [];
  scenario.steps.forEach((step, index) => {
    const attacks: AttackView[] = [];
    const messages: MessageView[] = [];
    const decideNow = (server: ServerId, subject: SubjectId) =>
      decide(setup(setups, server), subject, active(stores, server, subject, step.at));
    for (const event of step.events) {
      apply(event, stores, attacks, messages, decideNow);
    }
    const previous = frames.at(-1);
    const servers = SERVER_IDS.map((id): ServerView => ({
      id,
      decisions: SUBJECT_IDS.map((subject) =>
        withChange(
          decideNow(id, subject),
          previous?.servers.find((s) => s.id === id),
        ),
      ),
    }));
    frames.push({ index, servers, attacks, messages });
  });
  return frames;
}

function apply(
  event: DemoEvent,
  stores: Stores,
  attacks: AttackView[],
  messages: MessageView[],
  decideNow: (server: ServerId, subject: SubjectId) => Decision,
): void {
  switch (event.kind) {
    case 'attack':
      attacks.push({
        attacker: event.attacker,
        target: event.target,
        turnedAway: decideNow(event.target, event.attacker).state === 'blocked',
      });
      return;
    case 'detect': {
      const { publisher } = event.report;
      if (publisher === 'rogue') {
        throw new Error('only the three servers detect attacks');
      }
      keep(stores, publisher, event.report);
      return;
    }
    case 'share':
      // Each copy replaces the previous one: a publisher counts once, however often it reports.
      for (let copy = 0; copy < event.copies; copy++) {
        event.to.forEach((server) => keep(stores, server, event.report));
      }
      messages.push(message('report', event.report, event.to, event.copies));
      return;
    case 'revoke': {
      const { publisher } = event.report;
      const holders = publisher === 'rogue' ? event.to : [publisher, ...event.to];
      holders.forEach((server) => drop(stores, server, event.report));
      messages.push(message('revocation', event.report, event.to, 1));
      return;
    }
  }
}

function keep(stores: Stores, server: ServerId, report: Report): void {
  const bySubject = stores[server];
  const byPublisher = bySubject.get(report.subject) ?? new Map<PublisherId, Report>();
  byPublisher.set(report.publisher, report);
  bySubject.set(report.subject, byPublisher);
}

function drop(stores: Stores, server: ServerId, report: Report): void {
  stores[server].get(report.subject)?.delete(report.publisher);
}

/** The server's reports on `subject` that have not expired at minute `now`. */
function active(stores: Stores, server: ServerId, subject: SubjectId, now: number): Report[] {
  const reports = [...(stores[server].get(subject)?.values() ?? [])];
  return reports.filter((report) => now < report.issuedAt + report.ttlMinutes);
}

function message(
  kind: MessageView['kind'],
  report: Report,
  to: readonly ServerId[],
  copies: number,
): MessageView {
  return { kind, from: report.publisher, to, subject: report.subject, copies };
}

function setup(setups: Map<ServerId, ServerSetup>, id: ServerId): ServerSetup {
  const server = setups.get(id);
  if (!server) {
    throw new Error(`the scenario has no server ${id}`);
  }
  return server;
}

function withChange(decision: Decision, previous: ServerView | undefined): DecisionView {
  const before = previous?.decisions.find((d) => d.subject === decision.subject);
  const changed =
    before !== undefined &&
    (before.state !== decision.state ||
      before.cause !== decision.cause ||
      before.reporters !== decision.reporters ||
      before.contributions.length !== decision.contributions.length ||
      Math.abs(before.score - decision.score) > 1e-9);
  return { ...decision, changed, before: before?.state ?? decision.state };
}
