package console

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
)

// actionNotice is an action's outcome on the page the browser returns to.
type actionNotice struct {
	// Level is "ok" or "warning".
	Level string
	Title string
	Lines []string
	// Links lead to the views the action affected.
	Links []link
}

// outcomeNotice says what the action kind did.
func outcomeNotice(kind string, out *ActionOutcome) actionNotice {
	addr := rangeText(out.Range)
	n := actionNotice{Level: "ok"}
	switch kind {
	case ActionAllow, ActionBlock:
		verb := "allowed"
		if kind == ActionBlock {
			verb = "blocked"
		}
		until := "until you remove the override"
		if o := out.Override; o != nil && !o.ExpiresAt.IsZero() {
			until = "until " + stamp(o.ExpiresAt).Text
		}
		n.Title = fmt.Sprintf("%s is always %s on this node, %s.", addr, verb, until)
		if out.Warning != "" {
			n.Level = "warning"
			n.Lines = append(n.Lines, "Warning: "+out.Warning+".")
		}
	case ActionUnoverride:
		n.Title = "The override on " + addr + " was removed."
	case ActionReport:
		switch {
		case out.Coalesced:
			n.Title = "Your report on " + addr + " was added to the next refresh of your verdict."
		case out.Held:
			n.Level = "warning"
			n.Title = "Your verdict on " + addr + " is stored and counts on this node."
			n.Lines = append(n.Lines, "No peer is reachable now: it will be sent as soon as one is (unless obied restarts before).")
		default:
			n.Title = fmt.Sprintf("Your verdict on %s was published to %s.", addr, plural(out.Peers, "peer", "peers"))
		}
	case ActionRevoke:
		if out.Held {
			n.Level = "warning"
			n.Title = "Your verdict on " + addr + " is revoked on this node."
			n.Lines = append(n.Lines, "No peer is reachable now: the revocation will be sent as soon as one is (unless obied restarts before).")
		} else {
			n.Title = fmt.Sprintf("Your verdict on %s was revoked; the revocation was published to %s.", addr,
				plural(out.Peers, "peer", "peers"))
		}
	}
	n.Lines = append(n.Lines, "Decision now: "+strings.ToLower(stateLabel(out.Decision.State))+" — "+out.Decision.Reason+".")
	n.Links = []link{{Text: "Its decision", Href: decisionHref(out.Range)},
		{Text: "In the activity timeline", Href: activityQuery{address: addr}.href("/activity")}}
	return n
}

// memo keeps what the browser returns to after a redirect — the outcome
// of an action, an action waiting for its operator to sign in — on the
// node under random IDs, so that a URL carries neither words nor details
// (ADR 0026). It keeps the last max values, each for life.
type memo[T any] struct {
	max  int
	life time.Duration

	mu   sync.Mutex
	byID map[string]memoEntry[T]
	// order holds the IDs, oldest first.
	order []string
}

type memoEntry[T any] struct {
	value T
	at    time.Time
}

func newMemo[T any](maxValues int, life time.Duration) *memo[T] {
	return &memo[T]{max: maxValues, life: life, byID: make(map[string]memoEntry[T])}
}

// put keeps v and returns its ID, forgetting the oldest value over max.
func (m *memo[T]) put(v T, now time.Time) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never fails; it crashes the program instead.
	id := base64.RawURLEncoding.EncodeToString(b)
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.order) >= m.max {
		delete(m.byID, m.order[0])
		m.order = m.order[1:]
	}
	m.byID[id] = memoEntry[T]{value: v, at: now}
	m.order = append(m.order, id)
	return id
}

// get returns the value with the ID id, unless it is older than life.
func (m *memo[T]) get(id string, now time.Time) (T, bool) {
	var zero T
	if id == "" {
		return zero, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.byID[id]
	if !ok || now.Sub(e.at) > m.life {
		return zero, false
	}
	return e.value, true
}
