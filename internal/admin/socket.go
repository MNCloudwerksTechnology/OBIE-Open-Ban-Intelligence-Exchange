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
// process still answers on it or when path is not a socket. The socket gets
// SocketMode and, if the group exists, is owned by group.
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
	if conn, err := d.DialContext(ctx, "unix", path); err == nil {
		_ = conn.Close()
		return fmt.Errorf("%s is in use; is another obied running?", path)
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
	if err := os.Chown(path, -1, gid); err != nil {
		return fmt.Errorf("chgrp admin socket to %s: %w", group, err)
	}
	return nil
}
