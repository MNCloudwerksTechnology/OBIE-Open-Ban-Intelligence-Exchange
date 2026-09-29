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

// outcomes keeps the outcomes of actions for the pages the browsers
// return to, under random IDs, so a URL carries no words (ADR 0026).
type outcomes struct {
	mu   sync.Mutex
	byID map[string]storedOutcome
	// order holds the IDs, oldest first.
	order []string
}

type storedOutcome struct {
	notice actionNotice
	at     time.Time
}

func newOutcomes() *outcomes { return &outcomes{byID: make(map[string]storedOutcome)} }

// put keeps n and returns its ID, forgetting the oldest outcome over
// maxOutcomes.
func (o *outcomes) put(n actionNotice, now time.Time) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never fails; it crashes the program instead.
	id := base64.RawURLEncoding.EncodeToString(b)
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.order) >= maxOutcomes {
		delete(o.byID, o.order[0])
		o.order = o.order[1:]
	}
	o.byID[id] = storedOutcome{notice: n, at: now}
	o.order = append(o.order, id)
	return id
}

// get returns the outcome with the ID id, unless it is older than
// outcomeLifetime.
func (o *outcomes) get(id string, now time.Time) (actionNotice, bool) {
	if id == "" {
		return actionNotice{}, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.byID[id]
	if !ok || now.Sub(s.at) > outcomeLifetime {
		return actionNotice{}, false
	}
	return s.notice, true
}
