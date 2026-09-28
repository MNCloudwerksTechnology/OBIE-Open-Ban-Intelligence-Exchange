package decision

import "unique"

// heldVerdict is an active verdict of a kept decision: its publisher,
// interned, and whether it counts in the decision.
type heldVerdict struct {
	publisher unique.Handle[string]
	counts    bool
}

// heldOf returns the active verdicts of a decision's contributions.
func heldOf(contributions []Contribution) []heldVerdict {
	if len(contributions) == 0 {
		return nil
	}
	out := make([]heldVerdict, len(contributions))
	for i, c := range contributions {
		out[i] = heldVerdict{publisher: unique.Make(c.PeerID), counts: c.Contributes}
	}
	return out
}

// PublisherCount counts the active verdicts of one publisher in the kept
// decisions.
type PublisherCount struct {
	// Verdicts counts its active verdicts; Counting those that count in
	// the decisions: its ban verdicts, while its trust weight is above 0.
	Verdicts, Counting int
}

// PublisherCounts returns the counts of every publisher with active
// verdicts in the kept decisions, by peer ID; this node's own verdicts
// are counted under its peer ID. Reading them costs O(publishers),
// however many decisions are kept.
func (e *Engine) PublisherCounts() map[string]PublisherCount {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]PublisherCount, len(e.publishers))
	for p, c := range e.publishers {
		out[p.Value()] = c
	}
	return out
}

// count adds the verdicts held, times sign, to the counts by publisher.
// Callers hold e.mu.
func (e *Engine) count(held []heldVerdict, sign int) {
	for _, h := range held {
		c := e.publishers[h.publisher]
		c.Verdicts += sign
		if h.counts {
			c.Counting += sign
		}
		if c.Verdicts == 0 {
			delete(e.publishers, h.publisher)
		} else {
			e.publishers[h.publisher] = c
		}
	}
}
