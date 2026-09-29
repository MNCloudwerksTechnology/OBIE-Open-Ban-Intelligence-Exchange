//go:build unix

package cli

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// socketGroup returns the name of the group that owns the file at path, or
// "" if it cannot be told.
func socketGroup(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	gid := strconv.FormatUint(uint64(st.Gid), 10)
	if g, err := user.LookupGroupId(gid); err == nil {
		return g.Name
	}
	return gid
}
