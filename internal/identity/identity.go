// Package identity manages the persistent Ed25519 identity of an OBIE node:
// one key that is both the node's libp2p peer ID and its event-signing key
// (ADR 0005).
//
// The key lives in <state_dir>/node.key in the libp2p marshaled format and
// never leaves this package: other subsystems use it through Identity.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// FileName is the name of the key file in the state directory.
const FileName = "node.key"

// Errors reported by Load, Create and LoadOrCreate; test with errors.Is.
var (
	// ErrKeyExists means Create found a key file and was not told to
	// replace it.
	ErrKeyExists = errors.New("key file already exists")
	// ErrCorrupt means the key file is not a valid libp2p marshaled
	// Ed25519 private key.
	ErrCorrupt = errors.New("corrupted key file")
	// ErrInsecure means the key file could be read or replaced by another
	// user; the error text names the fix.
	ErrInsecure = errors.New("insecure key file")
)

// Identity is the node identity as seen by other subsystems. It never hands
// out the private key.
type Identity interface {
	// PeerID returns the libp2p peer ID in its text form ("12D3KooW...").
	PeerID() string
	// PublicKey returns the Ed25519 public key.
	PublicKey() ed25519.PublicKey
	// Sign returns the Ed25519 signature of msg.
	Sign(msg []byte) []byte
}

// Key is the node's Ed25519 key pair. It implements Identity; formatting it
// with fmt or logging it with slog shows only its peer ID.
type Key struct {
	// k is a pointer so that fmt prints an address rather than the private
	// key for verbs that bypass String, such as %d: fmt cannot call methods
	// of unexported fields.
	k *keyPair
}

type keyPair struct {
	priv   ed25519.PrivateKey
	peerID string
}

var (
	_ Identity       = Key{}
	_ slog.LogValuer = Key{}
)

func newKey(priv ed25519.PrivateKey) (*Key, error) {
	peerID, err := obieproto.PeerIDFromPublicKey(priv.Public().(ed25519.PublicKey))
	if err != nil {
		return nil, err
	}
	return &Key{k: &keyPair{priv: priv, peerID: peerID}}, nil
}

// PeerID implements Identity.
func (k Key) PeerID() string { return k.k.peerID }

// PublicKey implements Identity. The result is a copy.
func (k Key) PublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), k.k.priv.Public().(ed25519.PublicKey)...)
}

// Sign implements Identity.
func (k Key) Sign(msg []byte) []byte { return ed25519.Sign(k.k.priv, msg) }

// String returns the peer ID, so that printing a Key never reveals the
// private key.
func (k Key) String() string { return k.k.peerID }

// GoString is like String, for the %#v verb.
func (k Key) GoString() string { return "identity.Key(" + k.k.peerID + ")" }

// LogValue logs a Key as its peer ID.
func (k Key) LogValue() slog.Value { return slog.StringValue(k.k.peerID) }

// Fingerprint returns the fingerprint of an Ed25519 public key: "SHA256:"
// and the unpadded base64 SHA-256 hash of the raw key, as OpenSSH shows it.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// Path returns the path of the key file in stateDir.
func Path(stateDir string) string { return filepath.Join(stateDir, FileName) }

// Load reads the key file in stateDir. A missing file or directory yields
// an error matching fs.ErrNotExist; a key file that another user could
// access, replace or delete yields ErrInsecure; an unreadable key yields
// ErrCorrupt.
func Load(stateDir string) (*Key, error) {
	return load(stateDir, os.Geteuid())
}

// Verify checks the key file in stateDir as obied running as the user with
// uid would load it, and returns the key: the errors are those of Load,
// with the key file required to belong to uid rather than to the calling
// user. The self-check calls it as root for the service user.
func Verify(stateDir string, uid int) (*Key, error) {
	return load(stateDir, uid)
}

// load is Load with the key file required to belong to uid.
func load(stateDir string, uid int) (*Key, error) {
	if err := checkDir(stateDir); err != nil {
		return nil, err
	}
	path := Path(stateDir)
	data, err := readKeyFile(path, uid)
	if err != nil {
		return nil, err
	}
	priv, err := decodeKey(data)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w; restore it from a backup, or create a new key with "+
			"obied keygen --force (this changes the peer ID)", ErrCorrupt, path, err)
	}
	return newKey(priv)
}

// Create generates a new key and writes it atomically to the key file in
// stateDir, creating stateDir with mode 0700 if it is missing; an existing
// stateDir that group or others can write to yields ErrInsecure. An existing
// key file is replaced only if replace is set; otherwise the result matches
// ErrKeyExists and the file is left untouched.
func Create(stateDir string, replace bool) (*Key, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	key, err := newKey(priv)
	if err != nil {
		return nil, err
	}
	if err := writeKeyFile(Path(stateDir), encodeKey(priv), replace); err != nil {
		return nil, err
	}
	return key, nil
}

// LoadOrCreate loads the key file in stateDir, or creates it if it does not
// exist yet; created reports which happened.
func LoadOrCreate(stateDir string) (key *Key, created bool, err error) {
	key, err = Load(stateDir)
	if !errors.Is(err, fs.ErrNotExist) {
		return key, false, err
	}
	key, err = Create(stateDir, false)
	if errors.Is(err, ErrKeyExists) {
		// Another process created the key in the meantime; use it.
		key, err = Load(stateDir)
		return key, false, err
	}
	return key, err == nil, err
}
