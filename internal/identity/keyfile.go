package identity

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// keyFilePrefix starts every libp2p marshaled Ed25519 private key: the
// protobuf message PrivateKey {Type: Ed25519 (1), Data: <64 bytes>}, where
// Data is the seed followed by the public key — exactly what go-libp2p's
// crypto.MarshalPrivateKey writes.
var keyFilePrefix = []byte{
	0x08, 0x01, // protobuf field 1 (Type): Ed25519
	0x12, 0x40, // protobuf field 2 (Data): 64 bytes
}

// keyFileSize is the exact size of a key file.
var keyFileSize = len(keyFilePrefix) + ed25519.PrivateKeySize

func encodeKey(priv ed25519.PrivateKey) []byte {
	return append(bytes.Clone(keyFilePrefix), priv...)
}

// decodeKey parses a key file strictly: any other size or key type, and a
// public half that does not belong to the seed, is an error.
func decodeKey(data []byte) (ed25519.PrivateKey, error) {
	if len(data) != keyFileSize || !bytes.HasPrefix(data, keyFilePrefix) {
		return nil, errors.New("not a libp2p marshaled Ed25519 private key")
	}
	raw := data[len(keyFilePrefix):]
	priv := ed25519.NewKeyFromSeed(raw[:ed25519.SeedSize])
	if !bytes.Equal(priv, raw) {
		return nil, errors.New("public key does not match the private key")
	}
	return priv, nil
}

// readKeyFile opens the key file without following symlinks, checks its
// type, permissions and owner on the open file, and reads it.
func readKeyFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) // #nosec G304 -- path is <state_dir>/node.key.
	switch {
	case errors.Is(err, syscall.ELOOP):
		return nil, fmt.Errorf("%w: %s is a symbolic link; replace it with the key file itself", ErrInsecure, path)
	case errors.Is(err, fs.ErrPermission):
		return nil, fmt.Errorf("read key file: %w; the key file and the state directory must belong to the user running obied (%s)",
			err, currentUser())
	case err != nil:
		return nil, fmt.Errorf("read key file: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	if err := checkFile(path, info, os.Geteuid()); err != nil {
		return nil, err
	}
	// Read one byte more than a key file has, so that decodeKey rejects
	// oversized files without reading them whole.
	data, err := io.ReadAll(io.LimitReader(f, int64(keyFileSize)+1))
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	return data, nil
}

// checkFile refuses a key file that is not a regular file, that grants any
// permission to group or others, or that is not owned by euid.
func checkFile(path string, info fs.FileInfo, euid int) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrInsecure, path)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%w: %s has mode %04o and is accessible by group or others; fix with: chmod 600 %s",
			ErrInsecure, path, perm, path)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: cannot determine the owner of %s", ErrInsecure, path)
	}
	if int64(st.Uid) != int64(euid) {
		me := userName(strconv.Itoa(euid))
		return fmt.Errorf("%w: %s is owned by %s, not by the user running obied (%s); fix with: chown %s %s",
			ErrInsecure, path, userName(strconv.FormatUint(uint64(st.Uid), 10)), me, me, path)
	}
	return nil
}

// checkDir refuses a state directory that group or others can write to:
// they could delete the key file, and obied would then silently generate a
// new identity. A missing directory yields an error matching fs.ErrNotExist.
func checkDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("state directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("state directory %s is not a directory", dir)
	}
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		return fmt.Errorf("%w: state directory %s has mode %04o and is writable by group or others; fix with: chmod 700 %s",
			ErrInsecure, dir, perm, dir)
	}
	return nil
}

func currentUser() string { return userName(strconv.Itoa(os.Geteuid())) }

// userName returns the name of the user with the given uid, or the uid if
// it has no name.
func userName(uid string) string {
	if u, err := user.LookupId(uid); err == nil {
		return u.Username
	}
	return uid
}

// writeKeyFile writes data atomically to path with mode 0600, creating its
// directory with mode 0700 if needed. The data goes to a temporary file in
// the same directory first, which is then renamed over path (replace) or
// hard-linked to it, so that an existing file is never overwritten unless
// replace is set — not even by a concurrent writer.
func writeKeyFile(path string, data []byte, replace bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	if err := checkDir(dir); err != nil {
		return err
	}
	// CreateTemp creates the file with mode 0600.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	// After a rename the temporary name is gone; after a hard link or a
	// failure it must go. Failing to remove it does not undo a key that is
	// already in place, so the error is ignored.
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write key file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write key file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	if replace {
		err = os.Rename(tmp.Name(), path)
	} else {
		err = os.Link(tmp.Name(), path)
	}
	switch {
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%w: %s", ErrKeyExists, path)
	case !replace && (errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP)):
		return fmt.Errorf("write key file: %w (the file system of the state directory must support hard links)", err)
	case err != nil:
		return fmt.Errorf("write key file: %w", err)
	}
	return syncDir(dir)
}

// syncDir makes a rename or link in dir durable.
func syncDir(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- the state directory.
	if err != nil {
		return fmt.Errorf("sync state directory: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync state directory: %w", err)
	}
	return nil
}
