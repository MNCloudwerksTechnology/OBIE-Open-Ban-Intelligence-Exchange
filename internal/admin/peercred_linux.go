//go:build linux

package admin

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// readPeerCred returns the SO_PEERCRED credentials of a Unix socket
// connection.
func readPeerCred(c net.Conn) (peerCred, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return peerCred{}, fmt.Errorf("not a Unix socket connection: %T", c)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return peerCred{}, err
	}
	var ucred *syscall.Ucred
	var credErr error
	err = raw.Control(func(fd uintptr) {
		ucred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED) // #nosec G115 -- a file descriptor fits in an int.
	})
	if err = errors.Join(err, credErr); err != nil {
		return peerCred{}, fmt.Errorf("read SO_PEERCRED: %w", err)
	}
	return peerCred{PID: ucred.Pid, UID: ucred.Uid, GID: ucred.Gid}, nil
}
