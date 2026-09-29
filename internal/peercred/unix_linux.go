//go:build linux

package peercred

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// Unix returns the SO_PEERCRED credentials of a Unix socket connection.
func Unix(c net.Conn) (Cred, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return Cred{}, fmt.Errorf("not a Unix socket connection: %T", c)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return Cred{}, err
	}
	var ucred *syscall.Ucred
	var credErr error
	err = raw.Control(func(fd uintptr) {
		ucred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED) // #nosec G115 -- a file descriptor fits in an int.
	})
	if err = errors.Join(err, credErr); err != nil {
		return Cred{}, fmt.Errorf("read SO_PEERCRED: %w", err)
	}
	return Cred{PID: ucred.Pid, UID: ucred.Uid, GID: ucred.Gid}, nil
}
