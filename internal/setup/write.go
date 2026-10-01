package setup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// FileMode is the mode of the configuration file: readable by root and the
// group obied runs in, as install.sh installs it; it may name internal
// networks.
const FileMode fs.FileMode = 0o640

// maxBackups bounds the numbered backups Write tries.
const maxBackups = 100

// ErrExists means the configuration file exists and Write was not allowed
// to replace it.
var ErrExists = errors.New("configuration file already exists")

// WriteOptions says how Write may write the file.
type WriteOptions struct {
	// Replace allows replacing an existing file: the operator agreed.
	Replace bool
	// Group owns the file if it exists, so that obied, running in it, can
	// read the file; empty leaves the group alone.
	Group string
}

// Write writes data to path with FileMode, creating its directory if
// needed. An existing file is replaced only with opts.Replace, and then
// kept as the returned backup (path.bak, or path.bak.1, ... if that is
// taken). The data goes to a temporary file first, which is renamed over
// path, so that path always holds either the old or the new file.
func Write(path string, data []byte, opts WriteOptions) (backup string, err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- /etc/obie is world-readable, as install.sh creates it.
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	info, err := os.Lstat(path)
	exists := err == nil
	switch {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return "", err
	case exists && !opts.Replace:
		return "", fmt.Errorf("%w: %s", ErrExists, path)
	case exists && !info.Mode().IsRegular():
		return "", notRegular(path)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	// After the rename the temporary name is gone; after a failure it must go.
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := fill(tmp, data, opts.Group); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	if exists {
		if backup, err = keepBackup(path); err != nil {
			return "", err
		}
		err = os.Rename(tmp.Name(), path)
	} else {
		// A link fails if another process created path in the meantime.
		err = os.Link(tmp.Name(), path)
	}
	if err != nil {
		return backup, fmt.Errorf("write %s: %w", path, err)
	}
	if err := syncDir(dir); err != nil {
		return backup, fmt.Errorf("wrote %s, but it may not survive a crash: %w", path, err)
	}
	return backup, nil
}

// notRegular is the refusal to replace path, which is not a regular file.
func notRegular(path string) error {
	return fmt.Errorf("%s is not a regular file (a symbolic link?), which obied setup never replaces; "+
		"give the file itself with --config", path)
}

// CheckPath reports whether Write may write the configuration to path, as
// far as the path tells: it names no file Write refuses to replace, and no
// character ends the comments that name it. The assistant checks it
// before it asks anything.
func CheckPath(path string) error {
	if err := checkPathText(path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return notRegular(path)
	}
	return nil
}

// fill writes data to f, gives it FileMode and group, and closes it.
func fill(f *os.File, data []byte, group string) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Chmod(FileMode)
	}
	if err == nil && group != "" {
		err = chgrp(f, group)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// chgrp gives f the group if it exists.
func chgrp(f *os.File, group string) error {
	g, err := user.LookupGroup(group)
	if err != nil {
		return nil // No such group (install.sh not run yet): nothing to give.
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("group %s has the non-numeric ID %q", group, g.Gid)
	}
	if err := f.Chown(-1, gid); err != nil {
		if errors.Is(err, fs.ErrPermission) && os.Geteuid() != 0 {
			// Not root and not in the group: a file of the user's own, as
			// for a node run by hand; the group cannot be given.
			return nil
		}
		return fmt.Errorf("give the file to group %s: %w", group, err)
	}
	return nil
}

// keepBackup hard-links path to the first free backup name and returns it.
func keepBackup(path string) (string, error) {
	for n := 0; n < maxBackups; n++ {
		name := path + ".bak"
		if n > 0 {
			name += "." + strconv.Itoa(n)
		}
		err := os.Link(path, name)
		if err == nil {
			return name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("keep a backup of %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("keep a backup of %s: %s.bak to %s.bak.%d are all taken; remove old backups", path, path, path, maxBackups-1)
}

// syncDir makes a rename or link in dir durable.
func syncDir(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- the configuration's directory.
	if err != nil {
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	return nil
}

// CheckWritable reports whether this process may create or replace path:
// its directory, or the nearest parent that exists, must be writable. The
// assistant checks this before it asks anything.
func CheckWritable(path string) error {
	dir := filepath.Dir(path)
	for {
		err := unix.Access(dir, unix.W_OK|unix.X_OK)
		if errors.Is(err, unix.ENOENT) && filepath.Dir(dir) != dir {
			dir = filepath.Dir(dir)
			continue
		}
		if err != nil {
			return &fs.PathError{Op: "write", Path: dir, Err: err}
		}
		return nil
	}
}
