package admin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/httpserver"
)

// SocketMode is the permission of the admin socket: owner and group may
// connect.
const SocketMode fs.FileMode = 0o660

// staleProbeTimeout bounds the probe for a daemon still serving the socket.
const staleProbeTimeout = time.Second

// ListenUnix returns a ListenFunc for the admin socket at path. It removes a
// stale socket file left by a previous run, refusing to start when another
// process may still answer on it or when path is not a socket. The socket gets
// SocketMode and, if the group exists, is owned by group.
//
// The parent directory must exist. Until the chmod, the socket carries the
// permissions of the process umask, so the directory should admit only the
// service user and group (e.g. systemd RuntimeDirectoryMode=0750).
func ListenUnix(path, group string, log *slog.Logger) httpserver.ListenFunc {
	return func(ctx context.Context) (net.Listener, error) {
		if err := removeStaleSocket(ctx, path); err != nil {
			return nil, err
		}
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "unix", path)
		if err != nil {
			return nil, err
		}
		if err := setOwnership(path, group, log); err != nil {
			_ = ln.Close() // Also removes the socket file.
			return nil, err
		}
		return ln, nil
	}
}

func removeStaleSocket(ctx context.Context, path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("%s exists and is not a socket; refusing to replace it", path)
	}
	ctx, cancel := context.WithTimeout(ctx, staleProbeTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("%s is in use; is another obied running?", path)
	}
	// Only a refused connection proves nobody listens; any other failure
	// (permissions, full backlog, timeout) may hide a live daemon.
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("cannot tell whether %s is stale, refusing to replace it: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

func setOwnership(path, group string, log *slog.Logger) error {
	// #nosec G302 -- group members must be able to connect (ADR 0003).
	if err := os.Chmod(path, SocketMode); err != nil {
		return fmt.Errorf("chmod admin socket: %w", err)
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		log.Warn("admin socket group not found; keeping the process group", "group", group, "socket", path, "error", err)
		return nil
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("group %s has non-numeric gid %q", group, g.Gid)
	}
	if socketGID(path) == gid {
		// The socket already belongs to the group when it is the process's
		// group, as under the systemd unit (Group=obie); skipping the chown
		// keeps obied working under a system call filter without @chown.
		return nil
	}
	if err := chown(path, -1, gid); err != nil {
		return fmt.Errorf("chgrp admin socket to %s: %w", group, err)
	}
	return nil
}

// chown changes the group of the socket; tests replace it.
var chown = os.Chown

// socketGID returns the group ID of the file at path, or -1 if it cannot be
// determined.
func socketGID(path string) int {
	fi, err := os.Stat(path)
	if err != nil {
		return -1
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1
	}
	return int(st.Gid)
}
