package identity

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// goldenSeed is the seed 0x01..0x20; goldenKeyFile, goldenPeerID are what
// go-libp2p v0.50.0 produces for it (crypto.MarshalPrivateKey and
// peer.IDFromPrivateKey).
var (
	goldenSeed = func() []byte {
		seed := make([]byte, ed25519.SeedSize)
		for i := range seed {
			seed[i] = byte(i + 1)
		}
		return seed
	}()
	goldenKeyFile = "08011240" +
		"0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" +
		"79b5562e8fe654f94078b112e8a98ba7901f853ae695bed7e0e3910bad049664"
	goldenPeerID = "12D3KooWJ1TsijH7H5F74hfAD5XishQz3sxrmAtVY37GtNd9CqYf"
)

// newStateDir returns a new empty directory with mode 0700: t.TempDir applies
// the umask, which may leave it group-writable.
func newStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil { // #nosec G302 -- a directory needs the x bit.
		t.Fatal(err)
	}
	return dir
}

// writeKey writes content as the key file in a new state directory.
func writeKey(t *testing.T, content []byte, perm os.FileMode) string {
	t.Helper()
	dir := newStateDir(t)
	path := Path(dir)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	// Chmod explicitly: WriteFile applies the umask.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// dirEntries returns the names in dir.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestCreateLoadRoundtrip(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "var")
	stateDir := filepath.Join(parent, "state")
	created, err := Create(stateDir, false)
	if err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]os.FileMode{parent: 0o700, stateDir: 0o700, Path(stateDir): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s has mode %04o, want %04o", path, info.Mode().Perm(), want)
		}
	}

	loaded, err := Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PeerID() != created.PeerID() || !loaded.PublicKey().Equal(created.PublicKey()) {
		t.Fatalf("loaded %s, created %s", loaded.PeerID(), created.PeerID())
	}
	if !strings.HasPrefix(loaded.PeerID(), "12D3KooW") {
		t.Errorf("peer ID %q is not an Ed25519 libp2p peer ID", loaded.PeerID())
	}
	pub, err := obieproto.PublicKeyFromPeerID(loaded.PeerID())
	if err != nil || !pub.Equal(loaded.PublicKey()) {
		t.Errorf("peer ID does not embed the public key: %v", err)
	}
	msg := []byte("obie")
	if !ed25519.Verify(created.PublicKey(), msg, loaded.Sign(msg)) {
		t.Error("signature of the loaded key does not verify with the created public key")
	}
}

func TestKeyFileMatchesLibp2p(t *testing.T) {
	key, err := newKey(ed25519.NewKeyFromSeed(goldenSeed))
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(encodeKey(key.k.priv)); got != goldenKeyFile {
		t.Errorf("encoded key = %s, want %s", got, goldenKeyFile)
	}

	stateDir := writeKey(t, mustDecodeHex(t, goldenKeyFile), 0o600)
	loaded, err := Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PeerID() != goldenPeerID {
		t.Errorf("peer ID = %s, want %s", loaded.PeerID(), goldenPeerID)
	}
}

func TestLoadOrCreate(t *testing.T) {
	stateDir := newStateDir(t)
	first, created, err := LoadOrCreate(stateDir)
	if err != nil || !created {
		t.Fatalf("first LoadOrCreate: created=%v, err=%v", created, err)
	}
	second, created, err := LoadOrCreate(stateDir)
	if err != nil || created {
		t.Fatalf("second LoadOrCreate: created=%v, err=%v", created, err)
	}
	if first.PeerID() != second.PeerID() {
		t.Errorf("peer ID changed across restarts: %s, then %s", first.PeerID(), second.PeerID())
	}
}

func TestLoadOrCreateDoesNotReplaceBadKey(t *testing.T) {
	stateDir := writeKey(t, []byte("garbage"), 0o600)
	if _, _, err := LoadOrCreate(stateDir); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if data, _ := os.ReadFile(Path(stateDir)); string(data) != "garbage" {
		t.Errorf("key file was changed to %q", data)
	}
}

func TestCreateReplace(t *testing.T) {
	stateDir := newStateDir(t)
	first, err := Create(stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(stateDir))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Create(stateDir, false); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("Create without replace: err = %v, want ErrKeyExists", err)
	}
	if after, _ := os.ReadFile(Path(stateDir)); !bytes.Equal(before, after) {
		t.Error("Create without replace changed the key file")
	}

	second, err := Create(stateDir, true)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if second.PeerID() == first.PeerID() || loaded.PeerID() != second.PeerID() {
		t.Errorf("replace: first %s, second %s, loaded %s", first.PeerID(), second.PeerID(), loaded.PeerID())
	}
	if names := dirEntries(t, stateDir); len(names) != 1 || names[0] != FileName {
		t.Errorf("state directory holds %v, want only %s", names, FileName)
	}
}

func TestConcurrentCreateKeepsOneKey(t *testing.T) {
	stateDir := newStateDir(t)
	const writers = 8
	keys := make([]*Key, writers)
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys[i], errs[i] = Create(stateDir, false)
		}()
	}
	wg.Wait()

	var winner *Key
	for i, err := range errs {
		switch {
		case err == nil && winner == nil:
			winner = keys[i]
		case err == nil:
			t.Errorf("two writers created a key: %s and %s", winner, keys[i])
		case !errors.Is(err, ErrKeyExists):
			t.Errorf("writer %d: %v", i, err)
		}
	}
	loaded, err := Load(stateDir)
	if err != nil || winner == nil || loaded.PeerID() != winner.PeerID() {
		t.Fatalf("loaded %v (%v), winner %v", loaded, err, winner)
	}
	if names := dirEntries(t, stateDir); len(names) != 1 {
		t.Errorf("state directory holds %v, want only %s", names, FileName)
	}
}

func TestInsecureStateDirectory(t *testing.T) {
	stateDir := writeKey(t, mustDecodeHex(t, goldenKeyFile), 0o600)
	if err := os.Chmod(stateDir, 0o770); err != nil { // #nosec G302 -- the insecure mode under test.
		t.Fatal(err)
	}
	want := "fix with: chmod 700 " + stateDir
	if _, err := Load(stateDir); !errors.Is(err, ErrInsecure) || !strings.Contains(err.Error(), want) {
		t.Errorf("Load: err = %v, want ErrInsecure with %q", err, want)
	}
	if _, err := Create(stateDir, true); !errors.Is(err, ErrInsecure) {
		t.Errorf("Create: err = %v, want ErrInsecure", err)
	}
	if got := hex.EncodeToString(mustReadFile(t, Path(stateDir))); got != goldenKeyFile {
		t.Error("Create replaced the key in an insecure directory")
	}
	for _, perm := range []os.FileMode{0o700, 0o750, 0o755} {
		if err := os.Chmod(stateDir, perm); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(stateDir); err != nil {
			t.Errorf("directory mode %04o: %v", perm, err)
		}
	}
}

func TestLoadStateDirNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("err = %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAtomicWriteFailureLeavesNoTrace(t *testing.T) {
	// A non-empty directory where the key file should be makes the final
	// rename or link fail after the temporary file was written.
	stateDir := newStateDir(t)
	if err := os.MkdirAll(filepath.Join(Path(stateDir), "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, replace := range []bool{false, true} {
		if _, err := Create(stateDir, replace); err == nil {
			t.Fatalf("Create(replace=%v) succeeded over a directory", replace)
		}
		if names := dirEntries(t, stateDir); len(names) != 1 || names[0] != FileName {
			t.Errorf("replace=%v: state directory holds %v, want only %s", replace, names, FileName)
		}
		if _, err := os.Stat(filepath.Join(Path(stateDir), "keep")); err != nil {
			t.Errorf("replace=%v: existing content damaged: %v", replace, err)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(newStateDir(t)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestLoadRefusesInsecureMode(t *testing.T) {
	valid := mustDecodeHex(t, goldenKeyFile)
	for _, perm := range []os.FileMode{0o640, 0o604, 0o660, 0o644, 0o602, 0o610} {
		t.Run(fmt.Sprintf("%04o", perm), func(t *testing.T) {
			stateDir := writeKey(t, valid, perm)
			_, err := Load(stateDir)
			if !errors.Is(err, ErrInsecure) {
				t.Fatalf("err = %v, want ErrInsecure", err)
			}
			if want := "fix with: chmod 600 " + Path(stateDir); !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not contain %q", err, want)
			}
		})
	}
	for _, perm := range []os.FileMode{0o400, 0o600, 0o700} {
		if _, err := Load(writeKey(t, valid, perm)); err != nil {
			t.Errorf("mode %04o: %v", perm, err)
		}
	}
}

func TestCheckFileRefusesForeignOwner(t *testing.T) {
	stateDir := writeKey(t, mustDecodeHex(t, goldenKeyFile), 0o600)
	info, err := os.Stat(Path(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	err = checkFile(Path(stateDir), info, os.Geteuid()+1)
	if !errors.Is(err, ErrInsecure) {
		t.Fatalf("err = %v, want ErrInsecure", err)
	}
	if !strings.Contains(err.Error(), "not by the user running obied") ||
		!strings.Contains(err.Error(), "fix with: chown ") {
		t.Errorf("error %q does not explain the fix", err)
	}
}

func TestLoadRefusesSymlink(t *testing.T) {
	target := writeKey(t, mustDecodeHex(t, goldenKeyFile), 0o600)
	stateDir := newStateDir(t)
	if err := os.Symlink(Path(target), Path(stateDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(stateDir); !errors.Is(err, ErrInsecure) || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("err = %v, want ErrInsecure for a symlink", err)
	}
}

func TestLoadRefusesDirectory(t *testing.T) {
	stateDir := newStateDir(t)
	if err := os.Mkdir(Path(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(stateDir); !errors.Is(err, ErrInsecure) || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("err = %v, want ErrInsecure for a directory", err)
	}
}

func TestLoadCorrupted(t *testing.T) {
	valid := mustDecodeHex(t, goldenKeyFile)
	mismatch := bytes.Clone(valid)
	mismatch[len(mismatch)-1] ^= 1
	rsa := bytes.Clone(valid)
	rsa[1] = 0x00 // KeyType RSA

	tests := map[string][]byte{
		"empty":              {},
		"text":               []byte("not a key\n"),
		"truncated":          valid[:len(valid)-1],
		"trailing byte":      append(bytes.Clone(valid), 0),
		"bare seed":          valid[len(keyFilePrefix) : len(keyFilePrefix)+ed25519.SeedSize],
		"other key type":     rsa,
		"public key differs": mismatch,
		"oversized":          bytes.Repeat([]byte{0x08}, 1<<20),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			stateDir := writeKey(t, content, 0o600)
			_, err := Load(stateDir)
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("err = %v, want ErrCorrupt", err)
			}
			if !strings.Contains(err.Error(), Path(stateDir)) || !strings.Contains(err.Error(), "obied keygen --force") {
				t.Errorf("error %q does not name the file and the fix", err)
			}
		})
	}
}

func TestKeyNeverPrintsPrivateKey(t *testing.T) {
	key, err := newKey(ed25519.NewKeyFromSeed(goldenSeed))
	if err != nil {
		t.Fatal(err)
	}
	var id Identity = key
	var textLog, jsonLog bytes.Buffer
	slog.New(slog.NewTextHandler(&textLog, nil)).Info("key", "ptr", key, "value", *key, "id", id)
	slog.New(slog.NewJSONHandler(&jsonLog, nil)).Info("key", "ptr", key, "value", *key, "id", id)
	outputs := []string{
		textLog.String(), jsonLog.String(), fmt.Sprintf("%v %v", []*Key{key}, map[string]Key{"k": *key}),
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%X", "%d", "%q", "%p", "%T"} {
		outputs = append(outputs, fmt.Sprintf(verb, key), fmt.Sprintf(verb, *key), fmt.Sprintf(verb, id))
	}
	secrets := []string{
		hex.EncodeToString(goldenSeed), base64.StdEncoding.EncodeToString(goldenSeed),
		base64.RawURLEncoding.EncodeToString(goldenSeed), "[1 2 3 4 5 6 7 8",
	}
	for _, out := range outputs {
		for _, secret := range secrets {
			if strings.Contains(out, secret) {
				t.Errorf("output %q reveals the private key", out)
			}
		}
	}
	if key.String() != goldenPeerID {
		t.Errorf("String() = %q, want the peer ID", key.String())
	}
	if !strings.Contains(textLog.String(), "ptr="+goldenPeerID+" value="+goldenPeerID+" id="+goldenPeerID) {
		t.Errorf("slog output %q does not show the peer ID", textLog.String())
	}
}

func TestPublicKeyIsACopy(t *testing.T) {
	key, err := newKey(ed25519.NewKeyFromSeed(goldenSeed))
	if err != nil {
		t.Fatal(err)
	}
	pub := key.PublicKey()
	pub[0] ^= 0xff
	if key.PublicKey().Equal(pub) {
		t.Error("modifying the returned public key changed the key")
	}
}

// The expected value was computed independently with
// xxd -r -p | sha256sum | xxd -r -p | base64.
func TestFingerprint(t *testing.T) {
	pub := ed25519.NewKeyFromSeed(goldenSeed).Public().(ed25519.PublicKey)
	const want = "SHA256:ZbYGc9btiEvwHCwiLYKtoHQPKawzVdapJcgfF/R6J7g"
	if got := Fingerprint(pub); got != want {
		t.Errorf("Fingerprint = %q, want %q", got, want)
	}
}
