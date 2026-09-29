//go:build linux

package peercred

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
)

// The kernel's TCP socket tables of this process's network namespace. They
// are read through /proc/self: the systemd unit's ProcSubset=pid hides
// /proc/net, but not /proc/<pid>/net.
const (
	tcp4Table = "/proc/self/net/tcp"
	tcp6Table = "/proc/self/net/tcp6"
)

// socketOwner returns the UID owning the TCP socket with local address
// client and remote address server. An IPv4 client may also use an IPv6
// socket (with an IPv4-mapped address), which only tcp6 lists.
func socketOwner(client, server netip.AddrPort) (uint32, error) {
	tables := []string{tcp6Table}
	if client.Addr().Is4() {
		tables = []string{tcp4Table, tcp6Table}
	}
	var err error
	for _, path := range tables {
		var uid uint32
		if uid, err = ownerIn(path, client, server); !errors.Is(err, errNotInTable) {
			return uid, err
		}
	}
	return 0, err
}

func ownerIn(path string, client, server netip.AddrPort) (uint32, error) {
	f, err := os.Open(path) // #nosec G304 -- a fixed procfs path.
	if err != nil {
		return 0, fmt.Errorf("read the socket table: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseSocketTable(f, client, server)
}
