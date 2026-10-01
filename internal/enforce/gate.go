// Package enforce carries the decision engine's blocks to the enforcement
// backend. The Gate is the only path there and applies node.mode: in
// observe mode decisions are logged but never reach the enforcer. The
// Reconciler makes the backend's entries match the Gate's blocks. See
// ADR 0013 and ADR 0014.
package enforce

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
)

// Gate tracks the current blocks and the mode. In enforce mode it notifies
// the reconciler of every block change; a mode switch always notifies it,
// so switching to enforce applies the blocks and switching to observe
// withdraws them.
type Gate struct {
	log    *slog.Logger
	notify func()

	mu     sync.Mutex
	mode   config.Mode
	blocks map[string]decision.Decision
}

// NewGate returns a gate in mode that calls notify, which must be fast and
// must not call back into the Gate, when the reconciler has work; a nil
// notify means no backend is configured.
func NewGate(mode config.Mode, notify func(), log *slog.Logger) *Gate {
	setModeMetric(mode)
	return &Gate{log: log, notify: notify, mode: mode, blocks: map[string]decision.Decision{}}
}

// Mode returns the current mode.
func (g *Gate) Mode() config.Mode {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.mode
}

// Blocks returns the current block decisions, expired ones included,
// ordered by indicator key.
func (g *Gate) Blocks() []decision.Decision {
	g.mu.Lock()
	out := make([]decision.Decision, 0, len(g.blocks))
	for _, d := range g.blocks {
		out = append(out, d)
	}
	g.mu.Unlock()
	slices.SortFunc(out, func(a, b decision.Decision) int { return strings.Compare(a.Indicator.Key(), b.Indicator.Key()) })
	return out
}

// Handle is the decision engine subscription: it records c and, in
// enforce mode, notifies the reconciler.
func (g *Gate) Handle(c decision.Change) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if c.Type == decision.ChangeRemoved {
		delete(g.blocks, c.Key)
	} else {
		g.blocks[c.Key] = c.Decision
	}
	attrs := []any{"indicator", c.Key, "change", c.Type, "cause", c.Cause, "reason", c.Decision.Reason}
	if c.Type != decision.ChangeRemoved {
		attrs = append(attrs, "expires_at", c.Decision.ExpiresAt.UTC())
	}
	level := slog.LevelInfo
	if c.Type == decision.ChangeUpdated {
		level = slog.LevelDebug // e.g. hourly refreshes of capped blocks
	}
	switch {
	case g.mode != config.ModeEnforce:
		g.log.Log(context.Background(), level, "observe mode: block decision not enforced", attrs...)
	case g.notify == nil:
		g.log.Warn("enforce mode: no enforcement backend, block decision not applied", attrs...)
	default:
		g.log.Log(context.Background(), level, "block decision sent to the enforcer", attrs...)
		g.notify()
	}
}

// SetMode switches the mode and notifies the reconciler, which applies
// every current block when switching to enforce and withdraws them when
// switching to observe.
func (g *Gate) SetMode(mode config.Mode) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if mode == g.mode {
		return
	}
	g.log.Warn("node mode changed", "from", g.mode, "to", mode, "blocks", len(g.blocks))
	g.mode = mode
	setModeMetric(mode)
	if g.notify != nil {
		g.notify()
	}
}
