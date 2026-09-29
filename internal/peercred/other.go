//go:build !linux

package peercred

import (
	"net"
	"net/netip"
)

// Unix reports ErrUnsupported: without SO_PEERCRED only the socket's file
// mode protects the admin API.
func Unix(net.Conn) (Cred, error) {
	return Cred{}, ErrUnsupported
}

// socketOwner reports ErrUnsupported: without the kernel's socket table
// only the token protects the console.
func socketOwner(_, _ netip.AddrPort) (uint32, error) {
	return 0, ErrUnsupported
}
