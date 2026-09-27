// Package enforce carries the decision engine's block changes to the
// enforcement backend. The Gate is the only path there and applies
// node.mode: in observe mode decisions are logged but never reach the
// enforcer. See ADR 0013.
package enforce

import (
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
)

// CauseMode is the cause of the changes the Gate sends when the mode
// switches.
const CauseMode = "mode"

// Enforcer applies block changes, e.g. to nftables.
type Enforcer interface {
	// Apply receives one block change. It is called one change at a time,
	// must be fast (queue the work) and must not call back into the Gate.
	Apply(decision.Change)
}

// Gate forwards block changes to the enforcer in enforce mode only. It
// tracks the current blocks in either mode, so that switching to enforce
// applies them and switching to observe withdraws them.
type Gate struct {
	log *slog.Logger
	now func() time.Time

	// mu serializes changes and mode switches, so the enforcer sees them
	// in order.
	mu       sync.Mutex
	mode     config.Mode
	enforcer Enforcer
	blocks   map[string]decision.Change
}

// NewGate returns a gate in mode that forwards to enforcer; a nil enforcer
// receives nothing (no backend configured).
func NewGate(mode config.Mode, enforcer Enforcer, log *slog.Logger) *Gate {
	return &Gate{log: log, now: time.Now, mode: mode, enforcer: enforcer, blocks: map[string]decision.Change{}}
}

// Mode returns the current mode.
func (g *Gate) Mode() config.Mode {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.mode
}

// Handle is the decision engine subscription: it records c and forwards it
// in enforce mode.
func (g *Gate) Handle(c decision.Change) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if c.Type == decision.ChangeRemoved {
		delete(g.blocks, c.Key)
	} else {
		g.blocks[c.Key] = c
	}
	attrs := []any{"indicator", c.Key, "change", c.Type, "cause", c.Cause, "reason", c.Decision.Reason}
	if c.Type != decision.ChangeRemoved {
		attrs = append(attrs, "expires_at", c.Decision.ExpiresAt.UTC())
	}
	if g.mode != config.ModeEnforce {
		g.log.Info("observe mode: block decision not enforced", attrs...)
		return
	}
	g.forward(c, attrs)
}

// forward hands c to the enforcer. Callers hold mu.
func (g *Gate) forward(c decision.Change, attrs []any) {
	if g.enforcer == nil {
		g.log.Warn("enforce mode: no enforcement backend, block decision not applied", attrs...)
		return
	}
	g.log.Info("block decision sent to the enforcer", attrs...)
	g.enforcer.Apply(c)
}

// SetMode switches the mode. Switching to enforce sends every current,
// unexpired block to the enforcer as added; switching to observe withdraws
// every block as removed. Both carry CauseMode.
func (g *Gate) SetMode(mode config.Mode) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if mode == g.mode {
		return
	}
	g.log.Warn("node mode changed", "from", g.mode, "to", mode, "blocks", len(g.blocks))
	g.mode = mode
	now := g.now()
	for _, key := range g.keys() {
		d := g.blocks[key].Decision
		if mode == config.ModeEnforce {
			if now.Before(d.ExpiresAt) { // an expired block is removed by the engine's next refresh
				c := decision.Change{Type: decision.ChangeAdded, Key: key, Decision: d, Cause: CauseMode}
				g.forward(c, []any{"indicator", key, "change", c.Type, "cause", c.Cause, "reason", d.Reason})
			}
			continue
		}
		if g.enforcer != nil {
			d.State, d.ExpiresAt, d.Reason = decision.StateNone, time.Time{}, "node switched to observe mode"
			g.enforcer.Apply(decision.Change{Type: decision.ChangeRemoved, Key: key, Decision: d, Cause: CauseMode})
		}
	}
}

// keys returns the keys of the current blocks in order. Callers hold mu.
func (g *Gate) keys() []string {
	keys := make([]string, 0, len(g.blocks))
	for key := range g.blocks {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
