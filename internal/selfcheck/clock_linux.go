//go:build linux

package selfcheck

import (
	"time"

	"golang.org/x/sys/unix"
)

// KernelClock reads the clock's synchronization state with adjtimex(2),
// changing nothing: what timedatectl shows as "System clock synchronized".
func KernelClock() (ClockState, error) {
	var tx unix.Timex
	state, err := unix.Adjtimex(&tx)
	if err != nil {
		return ClockState{}, err
	}
	return ClockState{
		Synced:   state != unix.TIME_ERROR && tx.Status&unix.STA_UNSYNC == 0,
		MaxError: time.Duration(tx.Maxerror) * time.Microsecond,
	}, nil
}
