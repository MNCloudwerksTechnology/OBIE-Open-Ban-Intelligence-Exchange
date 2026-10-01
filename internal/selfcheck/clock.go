package selfcheck

import (
	"fmt"
	"time"
)

// plausibleSince is a time before any OBIE release: a clock showing an
// earlier time is wrong, synchronized or not.
var plausibleSince = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// maxClockError is the estimated error beyond which a clock does not count
// as synchronized, as for timedatectl.
const maxClockError = 16 * time.Second

// ClockState is the kernel's view of the system clock.
type ClockState struct {
	// Synced is set while time synchronization (NTP) keeps the clock.
	Synced bool
	// MaxError is the kernel's estimate of the clock's error.
	MaxError time.Duration
}

// nextClock is the next step for a clock that is not kept in sync.
const nextClock = "let time synchronization keep the clock: sudo timedatectl set-ntp true, then check: timedatectl"

// checkClock: the clock shows a plausible time and is synchronized:
// verdicts expire by it, and peers drop verdicts dated more than five
// minutes ahead.
func (r *run) checkClock() Check {
	const id, name = "clock", "Clock"
	now := r.env.Now()
	if now.Before(plausibleSince) {
		return newCheck(id, name, problem(fmt.Sprintf("the clock shows %s, which cannot be right", now.UTC().Format(time.RFC3339)),
			"set the time and "+nextClock))
	}
	state, err := r.env.Clock()
	switch {
	case err != nil:
		return newCheck(id, name, warn(fmt.Sprintf("cannot tell whether the clock is synchronized: %v", err), "check it: timedatectl"))
	case !state.Synced || state.MaxError >= maxClockError:
		return newCheck(id, name, warn("the clock is not synchronized: if it drifts, peers drop this node's verdicts "+
			"and verdicts expire at the wrong time", nextClock))
	}
	return newCheck(id, name, ok(fmt.Sprintf("the clock is synchronized (estimated error %s)", state.MaxError)))
}
