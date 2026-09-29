import {
  Contribution,
  Decision,
  DecisionCause,
  PUBLISHER_IDS,
  PublisherId,
  Report,
  ServerSetup,
  SubjectId,
  SubjectState,
} from './mesh-demo.model';

// The decision rule of the OBIE node (internal/decision, ADR 0011 and 0013),
// rewritten for the demo: it only illustrates, it never decides anything on a
// node. Keep it in step with Evaluate and Decide in internal/decision.

/** Absorbs float rounding, so that e.g. 0.6 + 0.6 + 0.6 reaches 1.8. */
export const SCORE_TOLERANCE = 1e-9;

/** The weight `server` gives reports of `publisher`. */
export function weightOf(server: ServerSetup, publisher: PublisherId): number {
  if (publisher === server.id) {
    return server.settings.localWeight;
  }
  return server.trust[publisher] ?? server.settings.defaultWeight;
}

/**
 * What `server` decides about `subject` from `reports`, the latest active
 * report of each publisher: block when the score reaches the threshold and
 * enough publishers agree, or when the server's own report says ban (local
 * autoblock); the safety list always wins.
 */
export function decide(
  server: ServerSetup,
  subject: SubjectId,
  reports: readonly Report[],
): Decision {
  const contributions = [...reports]
    .sort((x, y) => PUBLISHER_IDS.indexOf(x.publisher) - PUBLISHER_IDS.indexOf(y.publisher))
    .map((report) => contribution(server, report));
  const counting = contributions.filter((c) => c.counts);
  const score = counting.reduce((sum, c) => sum + c.score, 0);
  const reporters = counting.length;
  const { threshold, quorum, localAutoblock } = server.settings;

  const agreement = score >= threshold - SCORE_TOLERANCE && reporters >= quorum;
  const ownBan = counting.some((c) => c.publisher === server.id);
  let cause: DecisionCause = reports.length === 0 ? 'none' : 'below-bar';
  if (agreement) {
    cause = 'agreement';
  } else if (localAutoblock && ownBan) {
    cause = 'own-detection';
  }
  if (server.safetyList.includes(subject)) {
    cause = 'safety-list';
  }
  return { subject, state: stateOf(cause), cause, score, reporters, contributions };
}

function contribution(server: ServerSetup, report: Report): Contribution {
  const weight = weightOf(server, report.publisher);
  const counts = report.action === 'ban' && weight > 0;
  return {
    publisher: report.publisher,
    weight,
    confidence: report.confidence,
    score: counts ? weight * report.confidence : 0,
    counts,
  };
}

function stateOf(cause: DecisionCause): SubjectState {
  switch (cause) {
    case 'none':
      return 'unknown';
    case 'below-bar':
      return 'watching';
    case 'safety-list':
      return 'safe';
    default:
      return 'blocked';
  }
}
