//go:build !linux

package selfcheck

import "errors"

// KernelClock is only available on Linux.
func KernelClock() (ClockState, error) {
	return ClockState{}, errors.New("reading the clock's synchronization state needs Linux")
}
