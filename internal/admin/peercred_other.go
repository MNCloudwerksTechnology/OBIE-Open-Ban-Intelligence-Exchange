//go:build !linux

package admin

import "net"

// readPeerCred reports errPeerCredUnsupported: without SO_PEERCRED only the
// socket's file mode protects the admin API.
func readPeerCred(net.Conn) (peerCred, error) {
	return peerCred{}, errPeerCredUnsupported
}
