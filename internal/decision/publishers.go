package decision

import (
	"unique"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
)

// heldVerdict is an active verdict of a kept decision: its publisher and
// its category, interned, and whether it counts in the decision.
type heldVerdict struct {
	publisher unique.Handle[string]
	category  unique.Handle[string]
	counts    bool
}

// heldOf returns the active verdicts of a decision's contributions.
func heldOf(contributions []Contribution) []heldVerdict {
	if len(contributions) == 0 {
		return nil
	}
	out := make([]heldVerdict, len(contributions))
	for i, c := range contributions {
		out[i] = heldVerdict{publisher: unique.Make(c.PeerID), category: unique.Make(Category(c.Reason, c.Protocol)),
			counts: c.Contributes}
	}
	return out
}

// Category names what a verdict is about: its evidence reason and the
// attacked protocol, e.g. "password_bruteforce/ssh" (ADR 0022), as the
// store names the categories of the verdicts it keeps (ADR 0023).
func Category(reason, protocol string) string {
	return store.Category(reason, protocol)
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

// Categories returns how many kept decisions hold an active verdict of
// each category (see Category). Reading them costs O(categories).
func (e *Engine) Categories() map[string]int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]int, len(e.categories))
	for c, n := range e.categories {
		out[c.Value()] = n
	}
	return out
}

// count adds the verdicts held by one decision, times sign, to the counts
// by publisher and by category. Callers hold e.mu.
func (e *Engine) count(held []heldVerdict, sign int) {
	for i, h := range held {
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
		if firstOfCategory(held, i) {
			if n := e.categories[h.category] + sign; n == 0 {
				delete(e.categories, h.category)
			} else {
				e.categories[h.category] = n
			}
		}
	}
}

// firstOfCategory reports whether held[i] is the first verdict of its
// category in held, so a decision counts once per category.
func firstOfCategory(held []heldVerdict, i int) bool {
	for _, h := range held[:i] {
		if h.category == held[i].category {
			return false
		}
	}
	return true
}
