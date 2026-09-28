//go:build linux

package peercred

import (
	"fmt"
	"net/netip"
	"os"
)

// socketTables are the kernel's TCP socket tables of this process's
// network namespace. They are read through /proc/self: the systemd unit's
// ProcSubset=pid hides /proc/net, but not /proc/<pid>/net.
var socketTables = map[bool]string{false: "/proc/self/net/tcp", true: "/proc/self/net/tcp6"}

// socketOwner returns the UID owning the TCP socket with local address
// client and remote address server.
func socketOwner(client, server netip.AddrPort) (uint32, error) {
	path := socketTables[client.Addr().Is6()]
	f, err := os.Open(path) // #nosec G304 -- a fixed procfs path.
	if err != nil {
		return 0, fmt.Errorf("read the socket table: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseSocketTable(f, client, server)
}
