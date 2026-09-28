package store

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/dgraph-io/badger/v4/table"
	"github.com/dgraph-io/ristretto/v2/z"
)

// ErrCorrupt is returned by Start when the database on disk is damaged.
var ErrCorrupt = errors.New("event store is corrupt")

// manifestFile is Badger's list of the tables that make up the database.
const manifestFile = "MANIFEST"

// checkFiles looks for damage in the database directory before Badger
// opens it, because Badger does not report all of it as an error: without
// a MANIFEST it silently starts an empty database next to the old tables,
// it does not verify the tables' block checksums when it opens them, a
// damaged table makes it panic in a goroutine of its own, which no caller
// can recover, and after most other damage it fails having started
// goroutines it never stops. So the key registry, the MANIFEST and every
// table it lists are read and verified here first, where a panic can be
// recovered; opts are the options the database is opened with.
func checkFiles(dir string, opts badger.Options) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var logs []string
	data, manifest := false, false
	for _, e := range entries {
		switch ext := filepath.Ext(e.Name()); {
		case e.Name() == manifestFile:
			manifest = true
		case ext == ".vlog" || ext == ".mem":
			logs = append(logs, e.Name())
			data = true
		case ext == ".sst":
			data = true
		}
	}
	if !manifest {
		if data {
			return fmt.Errorf("%w: %s is missing although the directory holds data files", ErrCorrupt, manifestFile)
		}
		return nil // a new database
	}
	kr, err := badger.OpenKeyRegistry(badger.KeyRegistryOptions{Dir: dir, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrCorrupt, badger.KeyRegistryFileName, err)
	}
	if err := kr.Close(); err != nil {
		return err
	}
	for _, name := range logs {
		if err := checkLogHeader(filepath.Join(dir, name), opts); err != nil {
			return err
		}
	}
	m, err := readManifest(filepath.Join(dir, manifestFile), opts)
	if err != nil {
		return err
	}
	ids := slices.Sorted(maps.Keys(m.Tables))
	for _, id := range ids {
		path := table.NewFilename(id, dir)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%w: table %s listed in %s: %w", ErrCorrupt, filepath.Base(path), manifestFile, err)
		}
		if err := checkTableFooter(path); err != nil {
			return err
		}
		if err := verifyTable(path, m.Tables[id].Compression, opts.BlockSize); err != nil {
			return err
		}
	}
	return nil
}

// logHeaderSize is the length of the header of Badger's value-log and
// memtable log files: [encryption key ID: 8][IV: 12].
const logHeaderSize = 20

// checkLogHeader reports the damage to a value-log or memtable log file
// that Badger fails on: an empty value log, and a header naming an
// encryption key although the database is not encrypted.
func checkLogHeader(path string, opts badger.Options) error {
	f, err := os.Open(path) // #nosec G304 -- a log file in the store directory.
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	switch {
	case info.Size() == 0 && filepath.Ext(name) == ".vlog":
		return fmt.Errorf("%w: value log %s is empty", ErrCorrupt, name)
	case info.Size() < logHeaderSize || len(opts.EncryptionKey) > 0:
		return nil // Badger truncates a short log; keys it checks itself.
	}
	var buf [8]byte
	if _, err := f.ReadAt(buf[:], 0); err != nil {
		return err
	}
	if id := binary.BigEndian.Uint64(buf[:]); id != 0 {
		return fmt.Errorf("%w: log %s is damaged: its header names encryption key %d, but the store is not encrypted",
			ErrCorrupt, name, id)
	}
	return nil
}

// readManifest replays the MANIFEST at path.
func readManifest(path string, opts badger.Options) (badger.Manifest, error) {
	f, err := os.Open(path) // #nosec G304 -- the MANIFEST in the store directory.
	if err != nil {
		return badger.Manifest{}, err
	}
	defer func() { _ = f.Close() }()
	m, _, err := badger.ReplayManifestFile(f, opts.ExternalMagicVersion, opts)
	if err != nil {
		return badger.Manifest{}, fmt.Errorf("%w: %s: %w", ErrCorrupt, manifestFile, err)
	}
	return m, nil
}

// verifyTable opens a Badger table read-only and verifies the checksums of
// its index and every block, recovering the panics Badger raises for some
// damage.
func verifyTable(path string, compression options.CompressionType, blockSize int) (err error) {
	name := filepath.Base(path)
	mf, err := z.OpenMmapFile(path, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("%w: table %s: %w", ErrCorrupt, name, err)
	}
	defer func() {
		if r := recover(); r != nil {
			msg, _, _ := strings.Cut(fmt.Sprint(r), "\n")
			err = fmt.Errorf("%w: table %s is damaged: %s", ErrCorrupt, name, msg)
		}
		// Closing twice (OpenTable closes on some errors) only fails.
		_ = mf.Close(-1)
	}()
	_, err = table.OpenTable(mf, table.Options{
		ReadOnly:    true,
		ChkMode:     options.OnTableRead,
		BlockSize:   blockSize,
		Compression: compression,
	})
	if err != nil {
		return fmt.Errorf("%w: table %s: %w", ErrCorrupt, name, err)
	}
	return nil
}

// checkTableFooter checks that the lengths in a Badger table's footer —
// [index][index length: 4][checksum][checksum length: 4], big-endian —
// point inside the file.
func checkTableFooter(path string) error {
	f, err := os.Open(path) // #nosec G304 -- a table file in the store directory.
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	pos := info.Size() - 4
	checksumLen, err := readUint32(f, pos)
	if err != nil {
		return fmt.Errorf("%w: table %s is truncated (%d bytes)", ErrCorrupt, name, info.Size())
	}
	if pos -= int64(checksumLen) + 4; pos < 0 {
		return fmt.Errorf("%w: table %s is truncated or damaged: its checksum does not fit into its %d bytes",
			ErrCorrupt, name, info.Size())
	}
	indexLen, err := readUint32(f, pos)
	if err != nil {
		return err
	}
	if pos -= int64(indexLen); pos < 0 {
		return fmt.Errorf("%w: table %s is truncated or damaged: its index does not fit into its %d bytes",
			ErrCorrupt, name, info.Size())
	}
	return nil
}

// readUint32 reads a big-endian uint32 at off.
func readUint32(r io.ReaderAt, off int64) (uint32, error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], off); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(buf[:]), nil
}

// openError explains why the database in dir did not open.
func openError(dir string, err error) error {
	switch {
	case strings.Contains(err.Error(), "Cannot acquire directory lock"):
		return fmt.Errorf("open database %s: it is in use by another process (is another obied running?): %w", dir, err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("open database %s: %w; it must belong to the user obied runs as", dir, err)
	case errors.Is(err, syscall.ENOSPC):
		return fmt.Errorf("open database %s: %w; free up disk space", dir, err)
	}
	if !errors.Is(err, ErrCorrupt) {
		err = fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return fmt.Errorf("open database %s: %w; restore the directory from a backup, or move it aside (mv %s %s.corrupt) "+
		"to start with an empty store, which loses the stored verdicts and operator overrides", dir, err, dir, dir)
}
