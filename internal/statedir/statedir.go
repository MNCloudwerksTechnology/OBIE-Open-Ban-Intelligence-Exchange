// Package statedir versions the layout of the node state directory
// (node.state_dir), so that an upgraded obied can migrate it and an older
// obied refuses to open one it does not understand (ADR 0017).
//
// The format version is a decimal integer in <state_dir>/FORMAT. A
// directory without the file is either new or was written before the file
// existed; both have the layout of format 1 and are stamped with it.
package statedir

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName is the name of the format file in the state directory.
const FileName = "FORMAT"

// Version is the state directory format this build reads and writes:
// node.key (the identity key) and db/ (the BadgerDB event store).
const Version = 1

// maxFileSize bounds how much of the format file is read.
const maxFileSize = 64

// ErrNewerFormat means the state directory was written by a newer obied
// whose format this build does not understand.
var ErrNewerFormat = errors.New("state directory has a newer format")

// Path returns the path of the format file in dir.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// Prepare makes dir usable by this build: it creates dir (mode 0700) if
// needed, stamps a directory without a format file with Version, and
// refuses a directory of a newer format with an error wrapping
// ErrNewerFormat that tells the operator what to do. It returns the format
// the directory had before (0 for a directory without a format file).
func Prepare(dir, buildVersion string) (previous int, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, fmt.Errorf("create state directory: %w", err)
	}
	found, err := read(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return 0, write(dir, Version)
	case err != nil:
		return 0, err
	case found > Version:
		return found, fmt.Errorf("%w: %s has format %d, but obied %s only understands format %d or older; "+
			"it was last used by a newer obied. Run that newer obied again, or restore a backup of the state "+
			"directory taken before the upgrade", ErrNewerFormat, dir, found, buildVersion, Version)
	}
	// found == Version: there is no older format to migrate from yet.
	return found, nil
}

// read returns the format version recorded in dir.
func read(dir string) (int, error) {
	path := Path(dir)
	f, err := os.Open(path) // #nosec G304 -- path is <state_dir>/FORMAT.
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	// Read one byte more than allowed, so that an oversized file is refused
	// without reading it whole.
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	text := strings.TrimSpace(string(data))
	v, err := strconv.Atoi(text)
	if len(data) > maxFileSize || err != nil || v < 1 {
		return 0, fmt.Errorf("%s does not hold a state directory format version (a positive integer): %.20q", path, text)
	}
	return v, nil
}

// write records version in dir atomically: through a temporary file that is
// renamed over the format file.
func write(dir string, version int) error {
	tmp, err := os.CreateTemp(dir, "."+FileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", Path(dir), err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // fails harmlessly after the rename
	_, err = fmt.Fprintf(tmp, "%d\n", version)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), Path(dir))
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", Path(dir), err)
	}
	return nil
}
